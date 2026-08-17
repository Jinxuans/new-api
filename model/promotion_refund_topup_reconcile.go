package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	ErrPromotionRefundTopUpAmbiguous        = errors.New("multiple top-ups match the refund case")
	ErrPromotionRefundTopUpMismatch         = errors.New("refund case does not match the top-up")
	ErrPromotionRefundResponsibilityChanged = errors.New("refund responsibility changed; reload the case before continuing")
)

// preparePromotionRefundTopUpAccounting publishes a pre-commit fence for the
// principal and the currently verifiable inviter. The transaction repeats all
// relationship checks and persists the durable holds; this preflight only
// closes the cache and delayed-batch window before that commit.
func preparePromotionRefundTopUpAccounting(topUp *TopUp, fencedUserIds refundHoldFenceScope) error {
	return preparePromotionRefundTopUpAccountingDB(DB, topUp, fencedUserIds)
}

func preparePromotionRefundTopUpAccountingDB(db *gorm.DB, topUp *TopUp, fencedUserIds refundHoldFenceScope) error {
	if db == nil {
		return errors.New("database is required")
	}
	if topUp == nil || strings.TrimSpace(topUp.TradeNo) == "" {
		return nil
	}
	var refundCases []PromotionRefundCase
	refundQuery := db.Select("id", "provider").Where("trade_no = ?", topUp.TradeNo)
	if topUp.PaymentProvider != "" {
		refundQuery = refundQuery.Where("provider = ?", topUp.PaymentProvider)
	}
	if err := refundQuery.Order("id ASC").Find(&refundCases).Error; err != nil {
		return err
	}
	if len(refundCases) == 0 {
		return nil
	}
	if topUp.PaymentProvider == "" {
		return fmt.Errorf("%w: payment provider is missing", ErrPromotionRefundTopUpMismatch)
	}
	if topUp.UserId <= 0 {
		return fmt.Errorf("%w: top-up user is missing", ErrPromotionRefundTopUpMismatch)
	}

	userIds := []int{topUp.UserId}
	var invitee User
	if err := db.Unscoped().Select("id", "inviter_id").Where("id = ?", topUp.UserId).First(&invitee).Error; err != nil {
		return err
	}
	if invitee.InviterId > 0 && invitee.InviterId != topUp.UserId {
		userIds = append(userIds, invitee.InviterId)
	}
	sort.Ints(userIds)
	for _, userId := range userIds {
		if err := fencedUserIds.Ensure(userId); err != nil {
			return err
		}
	}
	return nil
}

func preparePromotionRefundTopUpAccountingByTrade(tradeNo string, expectedProvider string, fencedUserIds refundHoldFenceScope) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return nil
	}
	var topUp TopUp
	if err := DB.Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTopUpNotFound
		}
		return err
	}
	if expectedProvider != "" && topUp.PaymentProvider != expectedProvider {
		return ErrPaymentMethodMismatch
	}
	return preparePromotionRefundTopUpAccounting(&topUp, fencedUserIds)
}

// bindPromotionRefundCaseToTopUpTx makes responsibility durable. It is safe to
// call once when an order is created and again after payment settlement has
// created a rebate, commission ledger, or first-top-up invitation reward.
func bindPromotionRefundCaseToTopUpTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp,
	fencedUserIds refundHoldFenceScope, reopenResolved bool, additionalUserIds ...int,
) (bool, error) {
	if tx == nil || refundCase == nil || topUp == nil || refundCase.Id <= 0 || topUp.Id <= 0 || topUp.UserId <= 0 {
		return false, errors.New("refund case and top-up are required")
	}
	if refundCase.TradeNo == "" || refundCase.TradeNo != topUp.TradeNo ||
		refundCase.Provider == "" || topUp.PaymentProvider == "" || refundCase.Provider != topUp.PaymentProvider ||
		(refundCase.TopUpId > 0 && refundCase.TopUpId != topUp.Id) ||
		(refundCase.UserId > 0 && refundCase.UserId != topUp.UserId) {
		return false, ErrPromotionRefundTopUpMismatch
	}

	responsibleUserIds := map[int]struct{}{topUp.UserId: {}}
	previousTopUpId := refundCase.TopUpId
	previousUserId := refundCase.UserId
	previousRebateId := refundCase.InvitationRebateId
	previousLedgerId := refundCase.CommissionLedgerId
	hadResponsibilityFingerprint := refundCase.ResponsibilityFingerprint != ""
	subscriptionOrder, subscription, subscriptionLinkChanged, err := bindPromotionRefundSubscriptionEntitlementTx(tx, topUp, 0)
	if err != nil {
		return false, err
	}
	responsibilityParts := []string{
		fmt.Sprintf("case:%d:kind:%s:paid:%d:refunded:%d:currency:%s",
			refundCase.Id, refundCase.Kind, refundCase.PaidAmountMinor, refundCase.RefundedAmountMinor, refundCase.Currency),
		fmt.Sprintf("topup:%d:user:%d:purpose:%s:status:%s:provider:%s:trade:%s:verified:%t:paid:%d:currency:%s:quota:%d:refund_status:%s:refunded_minor:%d:refunded_quota:%d",
			topUp.Id, topUp.UserId, topUp.Purpose, topUp.Status, topUp.PaymentProvider, topUp.TradeNo,
			topUp.PaidAmountVerified, topUp.PaidAmountMinor, topUp.PaidCurrency, topUp.CreditedQuota,
			topUp.RefundStatus, topUp.RefundedAmountMinor, topUp.RefundedQuota),
	}
	if topUp.Purpose == TopUpPurposeSubscription {
		responsibilityParts = append(responsibilityParts,
			promotionRefundSubscriptionResponsibilityPart(subscriptionOrder, subscription))
	}
	rebate, ledger, err := loadPromotionRefundLinkedCommissionTx(tx, refundCase, topUp)
	if err != nil {
		return false, err
	}
	if rebate != nil {
		refundCase.InvitationRebateId = rebate.Id
		responsibilityParts = append(responsibilityParts,
			fmt.Sprintf("rebate:%d:user:%d:invitee:%d:verified:%t:paid:%d:paid_currency:%s:amount:%d:quota:%d:currency:%s:status:%s:reversal_quota:%d",
				rebate.Id, rebate.InviterId, rebate.InviteeId, rebate.PaidAmountVerified, rebate.PaidAmountMinor,
				rebate.PaidCurrency, rebate.RebateAmountMinor, rebate.RebateQuota, rebate.RebateCurrency,
				rebate.Status, rebate.ReversalQuota))
		if rebate.InviterId > 0 {
			responsibleUserIds[rebate.InviterId] = struct{}{}
		}
	}
	if ledger != nil {
		refundCase.CommissionLedgerId = ledger.Id
		responsibilityParts = append(responsibilityParts,
			fmt.Sprintf("ledger:%d:user:%d:invitee:%d:source:%s:%d:status:%s:net:%d:quota:%d:currency:%s:reversal_amount:%d:reversal_quota:%d",
				ledger.Id, ledger.UserId, ledger.InviteeId, ledger.SourceType, ledger.SourceId, ledger.Status,
				ledger.NetAmountCents, ledger.QuotaEquivalent, ledger.Currency,
				ledger.ReversalAmountCents, ledger.ReversalQuota))
		if ledger.UserId > 0 {
			responsibleUserIds[ledger.UserId] = struct{}{}
		}
	}

	var invitationRewards []InvitationReward
	if err := lockForUpdate(tx).
		Select("id", "inviter_id", "invitee_id", "reward_quota", "transferred_quota", "status").
		Where("trigger_top_up_id = ? AND reward_type = ? AND status = ?",
			topUp.Id, InvitationRewardTypeFirstTopUp, InvitationRewardStatusSettled).
		Find(&invitationRewards).Error; err != nil {
		return false, err
	}
	for i := range invitationRewards {
		if invitationRewards[i].InviteeId != topUp.UserId || invitationRewards[i].InviterId <= 0 {
			return false, errors.New("linked invitation reward is inconsistent with the refunded top-up")
		}
		responsibilityParts = append(responsibilityParts,
			fmt.Sprintf("invitation_reward:%d:user:%d:quota:%d:transferred:%d:status:%s",
				invitationRewards[i].Id, invitationRewards[i].InviterId, invitationRewards[i].RewardQuota,
				invitationRewards[i].TransferredQuota, invitationRewards[i].Status))
		if invitationRewards[i].InviterId > 0 {
			responsibleUserIds[invitationRewards[i].InviterId] = struct{}{}
		}
	}
	if topUp.Purpose == TopUpPurposeAPIBalance {
		var remainingSuccessfulTopUps int64
		if err := tx.Model(&TopUp{}).
			Where("user_id = ? AND id <> ? AND purpose = ? AND status = ?", topUp.UserId, topUp.Id, TopUpPurposeAPIBalance, common.TopUpStatusSuccess).
			Where("refund_status IS NULL OR refund_status NOT IN ?", []string{TopUpRefundStatusFull, TopUpRefundStatusDisputed}).
			Count(&remainingSuccessfulTopUps).Error; err != nil {
			return false, err
		}
		if remainingSuccessfulTopUps == 0 {
			var growthRewards []GrowthReward
			if err := lockForUpdate(tx).
				Select("id", "user_id", "reward_quota", "status").
				Where("user_id = ? AND item_code = ? AND status IN ?", topUp.UserId, GrowthRewardItemFirstTopUp,
					[]string{GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
				Find(&growthRewards).Error; err != nil {
				return false, err
			}
			for i := range growthRewards {
				responsibilityParts = append(responsibilityParts,
					fmt.Sprintf("growth_reward:%d:user:%d:quota:%d:status:%s",
						growthRewards[i].Id, growthRewards[i].UserId, growthRewards[i].RewardQuota, growthRewards[i].Status))
			}
		}
	}
	sort.Strings(responsibilityParts)
	responsibilityFingerprint := common.Sha1([]byte(strings.Join(responsibilityParts, "|")))
	responsibilityChanged := refundCase.ResponsibilityFingerprint != responsibilityFingerprint ||
		previousTopUpId != topUp.Id || previousUserId != topUp.UserId ||
		(rebate != nil && previousRebateId != rebate.Id) ||
		(ledger != nil && previousLedgerId != ledger.Id) || subscriptionLinkChanged

	shouldHoldResponsibleUsers := refundCase.Status == PromotionRefundCaseStatusPendingReview ||
		(reopenResolved && responsibilityChanged && refundCase.Status == PromotionRefundCaseStatusResolved)
	if shouldHoldResponsibleUsers {
		orderedUserIds := make([]int, 0, len(responsibleUserIds))
		for userId := range responsibleUserIds {
			orderedUserIds = append(orderedUserIds, userId)
		}
		sort.Ints(orderedUserIds)
		if err := recordPromotionRefundCaseUsersTx(tx, refundCase.Id, orderedUserIds...); err != nil {
			return false, err
		}
		userIdsToLock := make([]int, 0, len(orderedUserIds)+len(additionalUserIds))
		userIdsToLock = append(userIdsToLock, orderedUserIds...)
		userIdsToLock = append(userIdsToLock, additionalUserIds...)
		if _, err := lockUsersForFinancialWriteTx(tx, userIdsToLock...); err != nil {
			return false, err
		}
		for _, userId := range orderedUserIds {
			if err := fencedUserIds.Ensure(userId); err != nil {
				return false, err
			}
			if err := tx.Unscoped().Model(&User{}).Where("id = ?", userId).Update("refund_hold", true).Error; err != nil {
				return false, err
			}
		}
	}

	refundCase.TopUpId = topUp.Id
	refundCase.UserId = topUp.UserId
	refundCase.ResponsibilityFingerprint = responsibilityFingerprint
	updates := map[string]interface{}{
		"top_up_id":                  topUp.Id,
		"user_id":                    topUp.UserId,
		"invitation_rebate_id":       refundCase.InvitationRebateId,
		"commission_ledger_id":       refundCase.CommissionLedgerId,
		"responsibility_fingerprint": responsibilityFingerprint,
	}
	requiresReviewForChange := hadResponsibilityFingerprint || previousTopUpId != topUp.Id || previousUserId != topUp.UserId ||
		(rebate != nil && previousRebateId != rebate.Id) || (ledger != nil && previousLedgerId != ledger.Id) ||
		subscriptionLinkChanged
	if responsibilityChanged && requiresReviewForChange {
		refundCase.RequiresRootReview = true
		updates["requires_root_review"] = true
	}
	if reopenResolved && responsibilityChanged && refundCase.Status == PromotionRefundCaseStatusResolved {
		refundCase.Status = PromotionRefundCaseStatusPendingReview
		refundCase.ResolvedAt = 0
		refundCase.ReviewerId = 0
		refundCase.ReviewNote = ""
		updates["status"] = refundCase.Status
		updates["resolved_at"] = int64(0)
		updates["reviewer_id"] = 0
		updates["review_note"] = ""
	}
	if err := tx.Model(&PromotionRefundCase{}).Where("id = ?", refundCase.Id).Updates(updates).Error; err != nil {
		return false, err
	}
	return responsibilityChanged, nil
}

func reconcilePromotionRefundForTopUpTx(tx *gorm.DB, topUp *TopUp, fencedUserIds refundHoldFenceScope, reopenResolved bool) error {
	if tx == nil || topUp == nil || topUp.Id <= 0 || topUp.TradeNo == "" {
		return nil
	}
	var refundCases []PromotionRefundCase
	if err := lockForUpdate(tx).
		Where("trade_no = ? AND provider = ?", topUp.TradeNo, topUp.PaymentProvider).
		Order("id ASC").Find(&refundCases).Error; err != nil {
		return err
	}
	if len(refundCases) == 0 {
		return nil
	}
	for i := range refundCases {
		if _, err := bindPromotionRefundCaseToTopUpTx(tx, &refundCases[i], topUp, fencedUserIds, reopenResolved); err != nil {
			return err
		}
	}
	return nil
}

// bindPromotionRefundResponsibilityTx is used by Root review and release. It
// does not trust fields inferred by the list endpoint: the top-up and every
// downstream responsibility link are locked and checked again here.
func bindPromotionRefundResponsibilityTx(tx *gorm.DB, refundCase *PromotionRefundCase, fencedUserIds refundHoldFenceScope,
	additionalUserIds ...int,
) (*TopUp, bool, error) {
	if tx == nil || refundCase == nil || refundCase.Id <= 0 || refundCase.TradeNo == "" {
		return nil, false, errors.New("refund case has no verifiable top-up")
	}
	var topUps []TopUp
	query := lockForUpdate(tx).Where("trade_no = ? AND payment_provider = ?", refundCase.TradeNo, refundCase.Provider)
	if refundCase.TopUpId > 0 {
		query = query.Where("id = ?", refundCase.TopUpId)
	}
	if err := query.Order("id ASC").Limit(2).Find(&topUps).Error; err != nil {
		return nil, false, err
	}
	if len(topUps) != 1 {
		return nil, false, errors.New("refund case has no unique matching top-up")
	}
	changed, err := bindPromotionRefundCaseToTopUpTx(tx, refundCase, &topUps[0], fencedUserIds, false, additionalUserIds...)
	if err != nil {
		return nil, false, err
	}
	return &topUps[0], changed, nil
}

// ReconcilePromotionRefundCaseResponsibility commits newly discovered
// responsibility before an administrator acts on a potentially stale view.
// The caller must make the operator reload when changed is true.
func ReconcilePromotionRefundCaseResponsibility(refundCaseId int) (changed bool, err error) {
	if refundCaseId <= 0 {
		return false, errors.New("invalid refund case")
	}
	fencedUserIds := newRefundHoldFenceScope()
	var preflightCase PromotionRefundCase
	if err := DB.Select("id", "trade_no", "provider").Where("id = ?", refundCaseId).First(&preflightCase).Error; err != nil {
		return false, err
	}
	var preflightTopUp TopUp
	if err := DB.Where("trade_no = ? AND payment_provider = ?", preflightCase.TradeNo, preflightCase.Provider).
		First(&preflightTopUp).Error; err == nil {
		if err := preparePromotionRefundTopUpAccounting(&preflightTopUp, fencedUserIds); err != nil {
			return false, errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	} else {
		return false, errors.New("refund case has no verifiable top-up")
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		var lockedTopUp TopUp
		if err := lockForUpdate(tx).Where("id = ?", preflightTopUp.Id).First(&lockedTopUp).Error; err != nil {
			return err
		}
		var refundCase PromotionRefundCase
		if err := lockForUpdate(tx).Where("id = ?", refundCaseId).First(&refundCase).Error; err != nil {
			return err
		}
		changedNow, err := bindPromotionRefundCaseToTopUpTx(tx, &refundCase, &lockedTopUp, fencedUserIds, false)
		changed = changedNow
		return err
	})
	reconcileErr := reconcilePromotionRefundHoldFences(fencedUserIds)
	if err != nil || reconcileErr != nil {
		return false, errors.Join(err, reconcileErr)
	}
	return changed, nil
}

// ReconcilePendingPromotionRefundTopUps repairs rows written out of order or
// by an older instance during rolling deployment. Pending cases are always
// rebound. A previously resolved zero-accounting case is reopened only when
// its matching top-up later became successful and has no applied refund.
func ReconcilePendingPromotionRefundTopUps(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	const batchSize = 100
	lastId := 0
	for {
		var refundCases []PromotionRefundCase
		if err := db.Select(
			"id", "trade_no", "provider", "status", "top_up_id", "user_id",
			"quota_amount", "wallet_debited_quota", "debt_created_quota", "cash_debt_created_minor",
		).
			Where("id > ? AND (status = ? OR top_up_id = 0 OR user_id = 0 OR (status = ? AND quota_amount = 0 AND wallet_debited_quota = 0 AND debt_created_quota = 0 AND cash_debt_created_minor = 0))",
				lastId, PromotionRefundCaseStatusPendingReview, PromotionRefundCaseStatusResolved).
			Order("id ASC").Limit(batchSize).Find(&refundCases).Error; err != nil {
			return err
		}
		if len(refundCases) == 0 {
			return nil
		}
		for i := range refundCases {
			candidate := refundCases[i]
			lastId = candidate.Id
			if candidate.TradeNo == "" {
				continue
			}
			var topUps []TopUp
			if err := db.Where("trade_no = ? AND payment_provider = ?", candidate.TradeNo, candidate.Provider).Order("id ASC").Limit(2).Find(&topUps).Error; err != nil {
				return err
			}
			if len(topUps) == 0 {
				continue
			}
			if len(topUps) != 1 {
				return ErrPromotionRefundTopUpAmbiguous
			}
			topUp := &topUps[0]
			if candidate.Status == PromotionRefundCaseStatusResolved &&
				(topUp.Status != common.TopUpStatusSuccess || topUp.RefundStatus != "") {
				continue
			}
			fencedUserIds := newRefundHoldFenceScope()
			if err := preparePromotionRefundTopUpAccountingDB(db, topUp, fencedUserIds); err != nil {
				return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
			}
			err := db.Transaction(func(tx *gorm.DB) error {
				var lockedTopUp TopUp
				if err := lockForUpdate(tx).Where("id = ?", topUp.Id).First(&lockedTopUp).Error; err != nil {
					return err
				}
				var lockedCase PromotionRefundCase
				if err := lockForUpdate(tx).Where("id = ?", candidate.Id).First(&lockedCase).Error; err != nil {
					return err
				}
				reopen := lockedCase.Status == PromotionRefundCaseStatusResolved &&
					lockedTopUp.Status == common.TopUpStatusSuccess && lockedTopUp.RefundStatus == ""
				_, err := bindPromotionRefundCaseToTopUpTx(tx, &lockedCase, &lockedTopUp, fencedUserIds, reopen)
				return err
			})
			reconcileErr := reconcilePromotionRefundHoldFences(fencedUserIds)
			if err != nil || reconcileErr != nil {
				return errors.Join(err, reconcileErr)
			}
		}
		if len(refundCases) < batchSize {
			return nil
		}
	}
}
