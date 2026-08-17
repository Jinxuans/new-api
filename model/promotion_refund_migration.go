package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	promotionRefundAccountingMigrationVersion   = 1
	promotionRefundAccountingMigrationBatchSize = 100
)

// MigrateLegacyPromotionRefundAccounting moves refund cases written before
// principal recovery accounting into an explicit Root-review state. It never
// debits a wallet or creates a debt: old deployments did not preserve enough
// evidence to distinguish an already recovered amount from an unrecovered
// one. Root must assess or waive the case through the normal recovery flow.
func MigrateLegacyPromotionRefundAccounting() error {
	for {
		var caseIds []int
		if err := DB.Model(&PromotionRefundCase{}).
			Where("(payload_hash = ? OR payload_hash IS NULL) AND (accounting_migration_version IS NULL OR accounting_migration_version < ?)", "", promotionRefundAccountingMigrationVersion).
			Order("id ASC").
			Limit(promotionRefundAccountingMigrationBatchSize).
			Pluck("id", &caseIds).Error; err != nil {
			return err
		}
		if len(caseIds) == 0 {
			return nil
		}

		fencedUserIds := newRefundHoldFenceScope()
		for _, caseId := range caseIds {
			if err := migrateLegacyPromotionRefundCase(caseId, fencedUserIds); err != nil {
				return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
			}
		}
		if err := reconcilePromotionRefundHoldFences(fencedUserIds); err != nil {
			return err
		}
	}
}

func migrateLegacyPromotionRefundCase(caseId int, fencedUserIds refundHoldFenceScope) error {
	if caseId <= 0 {
		return errors.New("invalid legacy refund case id")
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var refundCase PromotionRefundCase
		if err := lockForUpdate(tx).Where("id = ?", caseId).First(&refundCase).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if refundCase.PayloadHash != "" || refundCase.AccountingMigrationVersion >= promotionRefundAccountingMigrationVersion {
			return nil
		}

		// Structured recovery rows and non-zero accounting totals are proof that
		// a newer Root workflow already handled this case. Mark it as inspected,
		// but never reopen or reinterpret that decision.
		var obligationCount int64
		if err := tx.Model(&PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error; err != nil {
			return err
		}
		var actionCount int64
		if err := tx.Model(&PromotionRefundAction{}).Where("refund_case_id = ?", refundCase.Id).Count(&actionCount).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		if obligationCount > 0 || actionCount > 0 || refundCase.WalletDebitedQuota != 0 ||
			refundCase.DebtCreatedQuota != 0 || refundCase.CashDebtCreatedMinor != 0 {
			return tx.Model(&PromotionRefundCase{}).
				Where("id = ? AND (accounting_migration_version IS NULL OR accounting_migration_version < ?)", refundCase.Id, promotionRefundAccountingMigrationVersion).
				Updates(map[string]interface{}{
					"accounting_migration_version": promotionRefundAccountingMigrationVersion,
					"accounting_migrated_at":       now,
				}).Error
		}

		var topUp TopUp
		topUpFound := false
		if refundCase.TopUpId > 0 {
			err := tx.Where("id = ?", refundCase.TopUpId).First(&topUp).Error
			if err == nil &&
				(refundCase.TradeNo == "" || topUp.TradeNo == refundCase.TradeNo) &&
				(refundCase.Provider == "" || topUp.PaymentProvider == refundCase.Provider) {
				topUpFound = true
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if !topUpFound && strings.TrimSpace(refundCase.TradeNo) != "" {
			var topUps []TopUp
			query := tx.Where("trade_no = ?", refundCase.TradeNo)
			if refundCase.Provider != "" {
				query = query.Where("payment_provider = ?", refundCase.Provider)
			}
			err := query.Order("id ASC").Limit(2).Find(&topUps).Error
			if err == nil && len(topUps) == 1 {
				topUp = topUps[0]
				topUpFound = true
			} else if err != nil {
				return err
			}
		}

		userId := refundCase.UserId
		if topUpFound && topUp.UserId > 0 {
			userId = topUp.UserId
		}
		if userId > 0 {
			var user User
			err := tx.Unscoped().Select("id").Where("id = ?", userId).First(&user).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				userId = 0
			} else if err != nil {
				return err
			}
		}

		var linkedRebate *InvitationRebate
		var linkedLedger *PromotionCommissionLedger
		if topUpFound {
			var err error
			linkedRebate, linkedLedger, err = loadPromotionRefundLinkedCommissionTx(tx, &refundCase, &topUp)
			if err != nil {
				return err
			}
		}

		responsibleUserIds := make(map[int]struct{}, 2)
		if userId > 0 {
			responsibleUserIds[userId] = struct{}{}
		}
		if linkedLedger != nil && linkedLedger.UserId > 0 {
			var commissionOwner User
			err := tx.Unscoped().Select("id").Where("id = ?", linkedLedger.UserId).First(&commissionOwner).Error
			if err == nil {
				responsibleUserIds[linkedLedger.UserId] = struct{}{}
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		orderedUserIds := make([]int, 0, len(responsibleUserIds))
		for responsibleUserId := range responsibleUserIds {
			orderedUserIds = append(orderedUserIds, responsibleUserId)
		}
		sort.Ints(orderedUserIds)
		if len(orderedUserIds) > 0 {
			if err := recordPromotionRefundCaseUsersTx(tx, refundCase.Id, orderedUserIds...); err != nil {
				return err
			}
		}
		for _, responsibleUserId := range orderedUserIds {
			if err := fencedUserIds.Ensure(responsibleUserId); err != nil {
				return err
			}
			if err := tx.Unscoped().Model(&User{}).Where("id = ?", responsibleUserId).Update("refund_hold", true).Error; err != nil {
				return err
			}
		}

		quotaAmount := 0
		if topUpFound {
			quotaAmount = verifiableLegacyRefundQuota(&refundCase, &topUp)
		}
		reason := strings.TrimSpace(refundCase.Reason)
		migrationReason := "legacy refund principal recovery was not recorded; Root review is required before releasing the account"
		if userId == 0 {
			migrationReason += "; refund user could not be verified"
		}
		if quotaAmount == 0 {
			migrationReason += "; principal quota could not be verified from the payment snapshot"
		}
		if linkedLedger != nil && linkedLedger.UserId != userId {
			migrationReason += "; the linked commission owner is also held pending Root review"
		}
		if reason == "" {
			reason = migrationReason
		} else {
			reason += "; " + migrationReason
		}

		updates := map[string]interface{}{
			"status":                       PromotionRefundCaseStatusPendingReview,
			"requires_root_review":         true,
			"user_id":                      userId,
			"reason":                       reason,
			"accounting_migration_version": promotionRefundAccountingMigrationVersion,
			"accounting_migrated_at":       now,
		}
		if topUpFound {
			updates["top_up_id"] = topUp.Id
		}
		if linkedRebate != nil {
			updates["invitation_rebate_id"] = linkedRebate.Id
		}
		if linkedLedger != nil {
			updates["commission_ledger_id"] = linkedLedger.Id
		}
		if quotaAmount > 0 {
			updates["quota_amount"] = quotaAmount
		}
		result := tx.Model(&PromotionRefundCase{}).
			Where("id = ? AND (payload_hash = ? OR payload_hash IS NULL) AND (accounting_migration_version IS NULL OR accounting_migration_version < ?)",
				refundCase.Id, "", promotionRefundAccountingMigrationVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("legacy refund case %d changed during accounting migration", refundCase.Id)
		}
		return nil
	})
}

// verifiableLegacyRefundQuota returns only an amount supported by the stored,
// provider-verified payment snapshot. Zero means that Root must determine the
// amount manually; it never means that recovery is complete.
func verifiableLegacyRefundQuota(refundCase *PromotionRefundCase, topUp *TopUp) int {
	if refundCase == nil || topUp == nil || topUp.Purpose != TopUpPurposeAPIBalance ||
		!topUp.PaidAmountVerified || topUp.PaidAmountMinor <= 0 || topUp.CreditedQuota <= 0 ||
		topUp.CreditedQuota >= common.MaxQuota || !isISOCurrencyCode(topUp.PaidCurrency) {
		return 0
	}
	if refundCase.TradeNo == "" || refundCase.TradeNo != topUp.TradeNo ||
		(refundCase.Provider != "" && topUp.PaymentProvider != refundCase.Provider) ||
		(refundCase.PaidAmountMinor > 0 && refundCase.PaidAmountMinor != topUp.PaidAmountMinor) ||
		(refundCase.Currency != "" && refundCase.Currency != topUp.PaidCurrency) {
		return 0
	}

	switch refundCase.Kind {
	case PromotionRefundKindFull:
		if topUp.RefundStatus != TopUpRefundStatusFull || topUp.RefundedAmountMinor != topUp.PaidAmountMinor {
			return 0
		}
		return topUp.CreditedQuota
	case PromotionRefundKindDispute:
		if topUp.RefundStatus != TopUpRefundStatusDisputed || topUp.RefundedAmountMinor != topUp.PaidAmountMinor {
			return 0
		}
		return topUp.CreditedQuota
	case PromotionRefundKindPartial:
		if topUp.RefundStatus != TopUpRefundStatusPartial || refundCase.RefundedAmountMinor <= 0 ||
			refundCase.RefundedAmountMinor > topUp.PaidAmountMinor || topUp.RefundedAmountMinor < refundCase.RefundedAmountMinor {
			return 0
		}
		quota, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(topUp.CreditedQuota)).
			Mul(decimal.NewFromInt(refundCase.RefundedAmountMinor)).
			Div(decimal.NewFromInt(topUp.PaidAmountMinor)).Floor())
		if err != nil || quota <= 0 {
			return 0
		}
		return quota
	default:
		return 0
	}
}
