package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const PromotionRefundActionRevokeSubscription = "revoke_subscription_entitlement"

// bindPromotionRefundSubscriptionEntitlementTx resolves the durable
// payment-to-entitlement relationship. New orders carry the link directly;
// legacy orders are rebound automatically only when one time-correlated
// entitlement is provable. A Root-selected id may be supplied by the explicit
// revocation action when historical data is otherwise ambiguous.
func bindPromotionRefundSubscriptionEntitlementTx(tx *gorm.DB, topUp *TopUp, requestedSubscriptionId int) (*SubscriptionOrder, *UserSubscription, bool, error) {
	if tx == nil || topUp == nil || topUp.Id <= 0 {
		return nil, nil, false, errors.New("top-up and transaction are required")
	}
	if topUp.Purpose != TopUpPurposeSubscription {
		if requestedSubscriptionId != 0 {
			return nil, nil, false, errors.New("refund top-up is not a subscription payment")
		}
		return nil, nil, false, nil
	}
	if topUp.TradeNo == "" || topUp.PaymentProvider == "" || topUp.UserId <= 0 {
		return nil, nil, false, errors.New("subscription refund top-up identity is incomplete")
	}

	var orders []SubscriptionOrder
	if err := lockForUpdate(tx).
		Where("trade_no = ? AND payment_provider = ?", topUp.TradeNo, topUp.PaymentProvider).
		Order("id ASC").Limit(2).Find(&orders).Error; err != nil {
		return nil, nil, false, err
	}
	if len(orders) != 1 {
		return nil, nil, false, errors.New("subscription refund has no unique matching order")
	}
	order := &orders[0]
	if order.UserId != topUp.UserId || order.TradeNo != topUp.TradeNo || order.PaymentProvider != topUp.PaymentProvider {
		return nil, nil, false, errors.New("subscription order does not match the refunded top-up")
	}
	if order.Status != common.TopUpStatusSuccess {
		if requestedSubscriptionId != 0 {
			return nil, nil, false, errors.New("subscription payment is not completed")
		}
		return order, nil, false, nil
	}

	linkedSubscriptionId := 0
	if order.UserSubscriptionId != nil {
		linkedSubscriptionId = *order.UserSubscriptionId
		if linkedSubscriptionId <= 0 {
			return nil, nil, false, errors.New("subscription order has an invalid entitlement link")
		}
	}
	if linkedSubscriptionId > 0 && requestedSubscriptionId > 0 && linkedSubscriptionId != requestedSubscriptionId {
		return nil, nil, false, errors.New("selected subscription does not match the payment entitlement")
	}

	linkChanged := false
	if linkedSubscriptionId == 0 {
		candidateId := requestedSubscriptionId
		if candidateId == 0 && order.CompleteTime > 0 {
			windowStart := order.CompleteTime - 5
			if order.CreateTime > windowStart {
				windowStart = order.CreateTime
			}
			var candidates []UserSubscription
			if err := lockForUpdate(tx).
				Where("user_id = ? AND plan_id = ? AND source IN ? AND created_at BETWEEN ? AND ?",
					order.UserId, order.PlanId, []string{"order", order.PaymentMethod}, windowStart, order.CompleteTime+5).
				Order("id ASC").Limit(2).Find(&candidates).Error; err != nil {
				return nil, nil, false, err
			}
			if len(candidates) == 1 {
				candidateId = candidates[0].Id
			}
		}
		if candidateId == 0 {
			return order, nil, false, nil
		}

		var candidate UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", candidateId).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, false, errors.New("selected subscription entitlement was not found")
			}
			return nil, nil, false, err
		}
		if err := validateSubscriptionOrderEntitlement(order, &candidate); err != nil {
			return nil, nil, false, err
		}

		candidateIdCopy := candidate.Id
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND user_subscription_id IS NULL", order.Id).
			Update("user_subscription_id", &candidateIdCopy)
		if result.Error != nil {
			return nil, nil, false, result.Error
		}
		if result.RowsAffected != 1 {
			var current SubscriptionOrder
			if err := lockForUpdate(tx).Where("id = ?", order.Id).First(&current).Error; err != nil {
				return nil, nil, false, err
			}
			if current.UserSubscriptionId == nil || *current.UserSubscriptionId != candidate.Id {
				return nil, nil, false, errors.New("subscription entitlement link changed concurrently")
			}
			order = &current
		} else {
			order.UserSubscriptionId = &candidateIdCopy
			linkChanged = true
		}
		linkedSubscriptionId = candidate.Id
	}

	var subscription UserSubscription
	if err := lockForUpdate(tx).Where("id = ?", linkedSubscriptionId).First(&subscription).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, false, errors.New("linked subscription entitlement is missing")
		}
		return nil, nil, false, err
	}
	if err := validateSubscriptionOrderEntitlement(order, &subscription); err != nil {
		return nil, nil, false, err
	}
	return order, &subscription, linkChanged, nil
}

func validateSubscriptionOrderEntitlement(order *SubscriptionOrder, subscription *UserSubscription) error {
	if order == nil || subscription == nil || order.Id <= 0 || subscription.Id <= 0 {
		return errors.New("subscription order and entitlement are required")
	}
	if subscription.UserId != order.UserId || subscription.PlanId != order.PlanId {
		return errors.New("subscription entitlement does not match the payment order")
	}
	source := strings.TrimSpace(subscription.Source)
	if source != "order" && source != strings.TrimSpace(order.PaymentMethod) {
		return errors.New("subscription entitlement source does not match the payment order")
	}
	switch subscription.Status {
	case "active", "expired", "cancelled":
		return nil
	default:
		return errors.New("subscription entitlement has an invalid status")
	}
}

func promotionRefundSubscriptionResponsibilityPart(order *SubscriptionOrder, subscription *UserSubscription) string {
	if order == nil {
		return "subscription_order:missing"
	}
	linkedId := 0
	if order.UserSubscriptionId != nil {
		linkedId = *order.UserSubscriptionId
	}
	part := fmt.Sprintf("subscription_order:%d:user:%d:plan:%d:status:%s:provider:%s:trade:%s:entitlement:%d",
		order.Id, order.UserId, order.PlanId, order.Status, order.PaymentProvider, order.TradeNo, linkedId)
	if subscription == nil {
		return part + ":unbound"
	}
	return part + fmt.Sprintf(":subscription:%d:user:%d:plan:%d:status:%s:start:%d:end:%d:total:%d:used:%d:source:%s",
		subscription.Id, subscription.UserId, subscription.PlanId, subscription.Status,
		subscription.StartTime, subscription.EndTime, subscription.AmountTotal, subscription.AmountUsed, subscription.Source)
}

// revokePromotionRefundSubscriptionEntitlementTx terminates or records the
// already-ended entitlement atomically with the immutable refund action.
func revokePromotionRefundSubscriptionEntitlementTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp, requestedSubscriptionId int) (*UserSubscription, int64, bool, error) {
	if tx == nil || refundCase == nil || topUp == nil || requestedSubscriptionId <= 0 {
		return nil, 0, false, errors.New("subscription refund entitlement is required")
	}
	_, subscription, _, err := bindPromotionRefundSubscriptionEntitlementTx(tx, topUp, requestedSubscriptionId)
	if err != nil {
		return nil, 0, false, err
	}
	if subscription == nil {
		return nil, 0, false, errors.New("subscription refund has no linked entitlement")
	}

	endTimeBefore := subscription.EndTime
	groupChanged := false
	if subscription.Status == "active" {
		now := common.GetTimestamp()
		result := tx.Model(&UserSubscription{}).
			Where("id = ? AND status = ?", subscription.Id, "active").
			Updates(map[string]interface{}{
				"status": "cancelled", "end_time": now, "updated_at": now,
			})
		if result.Error != nil {
			return nil, 0, false, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, 0, false, errors.New("subscription entitlement status changed concurrently")
		}
		targetGroup, err := downgradeUserGroupForSubscriptionTx(tx, subscription, now)
		if err != nil {
			return nil, 0, false, err
		}
		groupChanged = targetGroup != ""
		subscription.Status = "cancelled"
		subscription.EndTime = now
		subscription.UpdatedAt = now
	}
	return subscription, endTimeBefore, groupChanged, nil
}

func validatePromotionRefundSubscriptionDispositionTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp) error {
	if tx == nil || refundCase == nil || topUp == nil {
		return errors.New("refund subscription state is required")
	}
	if topUp.Purpose != TopUpPurposeSubscription {
		return nil
	}
	order, subscription, _, err := bindPromotionRefundSubscriptionEntitlementTx(tx, topUp, 0)
	if err != nil {
		return err
	}
	if order == nil || order.Status != common.TopUpStatusSuccess || subscription == nil {
		return errors.New("subscription refund entitlement must be linked before review completion")
	}
	var actionCount int64
	if err := tx.Model(&PromotionRefundAction{}).
		Where("refund_case_id = ? AND action = ? AND user_subscription_id = ?",
			refundCase.Id, PromotionRefundActionRevokeSubscription, subscription.Id).
		Count(&actionCount).Error; err != nil {
		return err
	}
	if subscription.Status != "active" {
		if actionCount != 1 {
			return errors.New("subscription refund entitlement disposition is not audited")
		}
		return nil
	}
	if actionCount != 0 {
		return errors.New("revoked subscription entitlement became active again")
	}

	// Keeping an active entitlement is allowed only when Root quantified the
	// principal as cash and every cent of that assessed obligation was actually
	// recovered. The obligation is the authoritative assessment because legacy
	// partial-refund rows do not persist whether provider amounts were cumulative.
	var obligations []PromotionRefundObligation
	if err := lockForUpdate(tx).
		Where("refund_case_id = ? AND source_type = ? AND source_id = ? AND asset = ?",
			refundCase.Id, "top_ups", topUp.Id, PromotionFundAssetCash).
		Order("id ASC").Find(&obligations).Error; err != nil {
		return err
	}
	if len(obligations) == 0 {
		return errors.New("active subscription requires a fully recovered cash obligation")
	}
	for i := range obligations {
		obligation := &obligations[i]
		if obligation.Amount <= 0 || obligation.Currency == "" ||
			obligation.RecoveredAmount != obligation.Amount || obligation.WaivedAmount != 0 ||
			obligation.Status != PromotionRefundObligationStatusRecovered {
			return errors.New("active subscription requires a fully recovered cash obligation")
		}
	}
	return nil
}

// LoadPromotionRefundSubscriptionEntitlements attaches read-only entitlement
// details to admin case responses. It never discovers or persists a missing
// relationship from a GET request.
func LoadPromotionRefundSubscriptionEntitlements(db *gorm.DB, refundCases []*PromotionRefundCase) error {
	if db == nil {
		return errors.New("database is required")
	}
	topUpIds := make([]int, 0, len(refundCases))
	for _, refundCase := range refundCases {
		if refundCase == nil {
			continue
		}
		refundCase.SubscriptionOrderId = 0
		refundCase.UserSubscriptionId = 0
		refundCase.SubscriptionPlanId = 0
		refundCase.SubscriptionStatus = ""
		refundCase.SubscriptionStartTime = 0
		refundCase.SubscriptionEndTime = 0
		refundCase.SubscriptionAmountTotal = 0
		refundCase.SubscriptionAmountUsed = 0
		if refundCase.TopUpId > 0 {
			topUpIds = append(topUpIds, refundCase.TopUpId)
		}
	}
	if len(topUpIds) == 0 {
		return nil
	}

	var topUps []TopUp
	if err := db.Select("id", "user_id", "purpose", "trade_no", "payment_provider").
		Where("id IN ?", topUpIds).
		Find(&topUps).Error; err != nil {
		return err
	}
	topUpById := make(map[int]TopUp, len(topUps))
	tradeNos := make([]string, 0, len(topUps))
	for i := range topUps {
		topUpById[topUps[i].Id] = topUps[i]
		if topUps[i].Purpose == TopUpPurposeSubscription && topUps[i].TradeNo != "" {
			tradeNos = append(tradeNos, topUps[i].TradeNo)
		}
	}
	var orders []SubscriptionOrder
	if len(tradeNos) > 0 {
		if err := db.Where("trade_no IN ?", tradeNos).Order("id ASC").Find(&orders).Error; err != nil {
			return err
		}
	}
	type subscriptionOrderKey struct {
		tradeNo  string
		provider string
	}
	ordersByPayment := make(map[subscriptionOrderKey][]SubscriptionOrder, len(orders))
	subscriptionIds := make([]int, 0, len(orders))
	for i := range orders {
		key := subscriptionOrderKey{tradeNo: orders[i].TradeNo, provider: orders[i].PaymentProvider}
		ordersByPayment[key] = append(ordersByPayment[key], orders[i])
		if orders[i].UserSubscriptionId != nil && *orders[i].UserSubscriptionId > 0 {
			subscriptionIds = append(subscriptionIds, *orders[i].UserSubscriptionId)
		}
	}
	var subscriptions []UserSubscription
	if len(subscriptionIds) > 0 {
		if err := db.Where("id IN ?", subscriptionIds).Find(&subscriptions).Error; err != nil {
			return err
		}
	}
	subscriptionById := make(map[int]UserSubscription, len(subscriptions))
	for i := range subscriptions {
		subscriptionById[subscriptions[i].Id] = subscriptions[i]
	}

	for _, refundCase := range refundCases {
		if refundCase == nil {
			continue
		}
		topUp, exists := topUpById[refundCase.TopUpId]
		if !exists || topUp.Purpose != TopUpPurposeSubscription {
			continue
		}
		if topUp.TradeNo == "" || topUp.TradeNo != refundCase.TradeNo || topUp.PaymentProvider == "" ||
			topUp.PaymentProvider != refundCase.Provider || topUp.UserId <= 0 ||
			(refundCase.UserId > 0 && refundCase.UserId != topUp.UserId) {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The subscription payment top-up does not match the refund case.")
			continue
		}
		matchingOrders := ordersByPayment[subscriptionOrderKey{tradeNo: topUp.TradeNo, provider: topUp.PaymentProvider}]
		if len(matchingOrders) != 1 {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The subscription refund has no unique matching payment order.")
			continue
		}
		order := matchingOrders[0]
		refundCase.SubscriptionOrderId = order.Id
		refundCase.SubscriptionPlanId = order.PlanId
		if order.UserId != topUp.UserId || order.TradeNo != topUp.TradeNo ||
			order.PaymentProvider != topUp.PaymentProvider {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The subscription payment order does not match the refunded top-up.")
			continue
		}
		if order.Status != common.TopUpStatusSuccess {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The subscription payment order is not completed.")
			continue
		}
		if order.UserSubscriptionId == nil || *order.UserSubscriptionId <= 0 {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The subscription payment order has no linked entitlement.")
			continue
		}
		subscription, exists := subscriptionById[*order.UserSubscriptionId]
		if !exists {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The linked subscription entitlement no longer exists.")
			continue
		}
		if err := validateSubscriptionOrderEntitlement(&order, &subscription); err != nil {
			markPromotionRefundResponsibilityIntegrityError(refundCase,
				"The linked subscription entitlement does not match the payment order.")
			continue
		}
		refundCase.UserSubscriptionId = subscription.Id
		refundCase.SubscriptionPlanId = subscription.PlanId
		refundCase.SubscriptionStatus = subscription.Status
		refundCase.SubscriptionStartTime = subscription.StartTime
		refundCase.SubscriptionEndTime = subscription.EndTime
		refundCase.SubscriptionAmountTotal = subscription.AmountTotal
		refundCase.SubscriptionAmountUsed = subscription.AmountUsed
	}
	return nil
}
