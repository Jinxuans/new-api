package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PromotionFundKindSubscriptionBalanceDebited = "api_balance_subscription_debited"
	PromotionFundSourceSubscriptionOrders       = "subscription_orders"
)

var (
	ErrSubscriptionBalanceFundTransactionInvalid = errors.New("subscription balance fund transaction is invalid")
	ErrSubscriptionBalanceFundTransactionMissing = errors.New("completed balance subscription order is missing its fund transaction")
)

type subscriptionBalancePurchaseResult struct {
	Subscription *UserSubscription
	ChargedQuota int
	Replayed     bool
}

func subscriptionBalanceProviderPayload(chargedQuota int) string {
	return fmt.Sprintf("charged_quota=%d", chargedQuota)
}

func parseSubscriptionBalanceChargedQuota(providerPayload string) (int, bool) {
	const prefix = "charged_quota="
	if !strings.HasPrefix(providerPayload, prefix) {
		return 0, false
	}
	rawQuota := strings.TrimPrefix(providerPayload, prefix)
	if rawQuota == "" {
		return 0, false
	}
	for i := 0; i < len(rawQuota); i++ {
		if rawQuota[i] < '0' || rawQuota[i] > '9' {
			return 0, false
		}
	}
	chargedQuota, err := strconv.ParseInt(rawQuota, 10, 32)
	if err != nil || chargedQuota < 0 || chargedQuota >= int64(common.MaxQuota) {
		return 0, false
	}
	return int(chargedQuota), true
}

// subscriptionBalanceChargedQuotaForFundRecord accepts only a completed
// balance order with an exact positive debit stored at purchase time. Price
// and quota conversion settings are intentionally not consulted because they
// can change after settlement.
func subscriptionBalanceChargedQuotaForFundRecord(order *SubscriptionOrder) (int, bool) {
	if order == nil || order.Id <= 0 || order.UserId <= 0 ||
		order.PaymentProvider != PaymentProviderBalance ||
		order.Status != common.TopUpStatusSuccess {
		return 0, false
	}
	chargedQuota, ok := parseSubscriptionBalanceChargedQuota(order.ProviderPayload)
	if !ok || chargedQuota <= 0 {
		return 0, false
	}
	return chargedQuota, true
}

// recordSubscriptionBalanceFundTransactionTx records the debit represented by
// one completed balance order. Live settlement always supplies the exact
// BalanceAfter. Historical reconstruction leaves it unknown instead of using
// the user's current balance as if it were historical evidence.
func recordSubscriptionBalanceFundTransactionTx(tx *gorm.DB, order *SubscriptionOrder, balanceAfter *int64, historical bool) (*PromotionFundTransaction, error) {
	if tx == nil {
		return nil, errors.New("database transaction is required")
	}
	chargedQuota, ok := subscriptionBalanceChargedQuotaForFundRecord(order)
	if !ok || (!historical && balanceAfter == nil) {
		return nil, ErrSubscriptionBalanceFundTransactionInvalid
	}

	canonicalKey := fmt.Sprintf("subscription_order:%d:balance_debited", order.Id)
	transaction := &PromotionFundTransaction{
		TransactionKey: canonicalKey,
		Kind:           PromotionFundKindSubscriptionBalanceDebited,
		UserId:         order.UserId,
		SourceType:     PromotionFundSourceSubscriptionOrders,
		SourceId:       order.Id,
		SourceKey:      order.TradeNo,
		ActorType:      "user",
		ActorId:        order.UserId,
		OccurredAt:     promotionFundBackfillOccurredAt(order.CompleteTime, order.CreateTime),
	}
	legs := []PromotionFundTransactionLeg{{
		Account:      PromotionFundAccountAPIBalance,
		Asset:        PromotionFundAssetQuota,
		Amount:       -int64(chargedQuota),
		SourceType:   PromotionFundSourceSubscriptionOrders,
		SourceId:     order.Id,
		BalanceAfter: balanceAfter,
	}}
	if !historical {
		if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
			return nil, err
		}
		return transaction, nil
	}

	transaction.TransactionKey = fmt.Sprintf("pfb:%s:%d:balance_debited", PromotionFundSourceSubscriptionOrders, order.Id)
	if err := createPromotionFundBackfillTransitionTx(tx, transaction, legs, canonicalKey); err != nil {
		return nil, err
	}
	return transaction, nil
}

// completeSubscriptionBalanceOrderTx is the order-level idempotency boundary.
// The caller owns the database transaction and holds the user's financial row
// lock. A pending order is charged once; replaying the same completed order
// verifies its entitlement and immutable journal without changing the wallet.
func completeSubscriptionBalanceOrderTx(tx *gorm.DB, order *SubscriptionOrder, plan *SubscriptionPlan, lockedUser *User) (*subscriptionBalancePurchaseResult, error) {
	if tx == nil || order == nil || order.Id <= 0 || plan == nil || plan.Id <= 0 ||
		lockedUser == nil || lockedUser.Id <= 0 || order.UserId != lockedUser.Id ||
		order.PlanId != plan.Id || order.PaymentMethod != PaymentMethodBalance ||
		order.PaymentProvider != PaymentProviderBalance {
		return nil, ErrSubscriptionBalanceFundTransactionInvalid
	}

	chargedQuota, ok := parseSubscriptionBalanceChargedQuota(order.ProviderPayload)
	if !ok {
		return nil, ErrSubscriptionBalanceFundTransactionInvalid
	}

	if order.Status == common.TopUpStatusSuccess {
		if order.UserSubscriptionId == nil || *order.UserSubscriptionId <= 0 {
			return nil, ErrSubscriptionBalanceFundTransactionInvalid
		}
		var subscription UserSubscription
		if err := tx.Where("id = ? AND user_id = ? AND plan_id = ?", *order.UserSubscriptionId, order.UserId, order.PlanId).
			First(&subscription).Error; err != nil {
			return nil, err
		}
		if chargedQuota > 0 {
			var transaction PromotionFundTransaction
			transactionKey := fmt.Sprintf("subscription_order:%d:balance_debited", order.Id)
			if err := lockForUpdate(tx).Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
				return legTx.Order("id ASC")
			}).Where("transaction_key = ?", transactionKey).First(&transaction).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrSubscriptionBalanceFundTransactionMissing
				}
				return nil, err
			}
			if len(transaction.Legs) != 1 || transaction.Legs[0].BalanceAfter == nil {
				return nil, ErrSubscriptionBalanceFundTransactionInvalid
			}
			if _, err := recordSubscriptionBalanceFundTransactionTx(tx, order, transaction.Legs[0].BalanceAfter, false); err != nil {
				return nil, err
			}
		}
		return &subscriptionBalancePurchaseResult{
			Subscription: &subscription,
			ChargedQuota: chargedQuota,
			Replayed:     true,
		}, nil
	}
	if order.Status != common.TopUpStatusPending || order.UserSubscriptionId != nil {
		return nil, ErrSubscriptionOrderStatusInvalid
	}
	expectedQuota, err := calcSubscriptionBalanceQuota(plan.PriceAmount)
	if err != nil {
		return nil, err
	}
	if chargedQuota != expectedQuota {
		return nil, ErrSubscriptionBalanceFundTransactionInvalid
	}
	if chargedQuota > 0 && lockedUser.Quota < chargedQuota {
		return nil, errors.New("余额不足")
	}

	subscription, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, PaymentMethodBalance)
	if err != nil {
		return nil, err
	}
	order.UserSubscriptionId = &subscription.Id
	order.Status = common.TopUpStatusSuccess
	order.CompleteTime = common.GetTimestamp()
	if err := tx.Save(order).Error; err != nil {
		return nil, err
	}

	if chargedQuota > 0 {
		result := tx.Model(&User{}).
			Where("id = ? AND quota >= ?", order.UserId, chargedQuota).
			Update("quota", gorm.Expr("quota - ?", chargedQuota))
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, errors.New("余额不足")
		}
		lockedUser.Quota -= chargedQuota
		balanceAfter := int64(lockedUser.Quota)
		if _, err := recordSubscriptionBalanceFundTransactionTx(tx, order, &balanceAfter, false); err != nil {
			return nil, err
		}
	}

	return &subscriptionBalancePurchaseResult{
		Subscription: subscription,
		ChargedQuota: chargedQuota,
	}, nil
}
