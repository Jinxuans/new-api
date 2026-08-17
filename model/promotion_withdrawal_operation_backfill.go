package model

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BackfillPromotionWithdrawalOperations reconstructs only the state changes
// that can be proven from a legacy withdrawal row. It deliberately does not
// invent missing intermediate approvals or payout steps.
func BackfillPromotionWithdrawalOperations(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	cursor := 0
	for {
		var withdrawals []PromotionWithdrawal
		missingOperation := `
			NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_operations
				WHERE promotion_withdrawal_operations.withdrawal_id = promotion_withdrawals.id
				AND promotion_withdrawal_operations.action = ?
			)
			OR (status = ? AND NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_operations
				WHERE promotion_withdrawal_operations.withdrawal_id = promotion_withdrawals.id
				AND promotion_withdrawal_operations.action = ?
			))
			OR (status = ? AND NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_operations
				WHERE promotion_withdrawal_operations.withdrawal_id = promotion_withdrawals.id
				AND promotion_withdrawal_operations.action = ?
			))
			OR (status = ? AND NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_operations
				WHERE promotion_withdrawal_operations.withdrawal_id = promotion_withdrawals.id
				AND promotion_withdrawal_operations.action = ?
			))
			OR (status = ? AND NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_operations
				WHERE promotion_withdrawal_operations.withdrawal_id = promotion_withdrawals.id
				AND promotion_withdrawal_operations.action = ?
			))
			OR (status = ? AND NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_operations
				WHERE promotion_withdrawal_operations.withdrawal_id = promotion_withdrawals.id
				AND promotion_withdrawal_operations.action IN (?, ?)
			))`
		if err := db.Where("id > ? AND ("+missingOperation+")",
			cursor,
			PromotionWithdrawalActionSubmitted,
			PromotionWithdrawalStatusApproved, PromotionWithdrawalActionApproved,
			PromotionWithdrawalStatusProcessing, PromotionWithdrawalActionPayoutInitiated,
			PromotionWithdrawalStatusPaid, PromotionWithdrawalActionPaid,
			PromotionWithdrawalStatusRejected, PromotionWithdrawalActionRejected,
			PromotionWithdrawalStatusFailed, PromotionWithdrawalActionPayoutFailed, PromotionWithdrawalActionCancelledByRefund,
		).
			Order("id ASC").Limit(200).Find(&withdrawals).Error; err != nil {
			return err
		}
		if len(withdrawals) == 0 {
			return nil
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			withdrawalIds := make([]int, len(withdrawals))
			for i := range withdrawals {
				withdrawalIds[i] = withdrawals[i].Id
			}
			var lockedWithdrawals []PromotionWithdrawal
			if err := lockForUpdate(tx).Where("id IN ?", withdrawalIds).
				Order("id ASC").Find(&lockedWithdrawals).Error; err != nil {
				return err
			}
			var existingOperations []PromotionWithdrawalOperation
			if err := tx.Where("withdrawal_id IN ?", withdrawalIds).Find(&existingOperations).Error; err != nil {
				return err
			}
			actionsByWithdrawal := make(map[int]map[string]struct{}, len(lockedWithdrawals))
			for i := range existingOperations {
				operation := &existingOperations[i]
				actions := actionsByWithdrawal[operation.WithdrawalId]
				if actions == nil {
					actions = make(map[string]struct{})
					actionsByWithdrawal[operation.WithdrawalId] = actions
				}
				actions[operation.Action] = struct{}{}
			}

			for i := range lockedWithdrawals {
				withdrawal := &lockedWithdrawals[i]
				if withdrawal.Id <= 0 || withdrawal.UserId <= 0 {
					return fmt.Errorf("invalid legacy withdrawal %d", withdrawal.Id)
				}
				existingActions := actionsByWithdrawal[withdrawal.Id]
				submittedAt := withdrawal.AppliedAt
				if submittedAt == 0 {
					submittedAt = withdrawal.CreatedAt
				}
				operations := make([]PromotionWithdrawalOperation, 0, 2)
				_, hasSubmitted := existingActions[PromotionWithdrawalActionSubmitted]
				if !hasSubmitted && submittedAt > 0 {
					operations = append(operations, PromotionWithdrawalOperation{
						WithdrawalId:  withdrawal.Id,
						Action:        PromotionWithdrawalActionSubmitted,
						ActorType:     PromotionWithdrawalActorUser,
						ActorId:       withdrawal.UserId,
						Reconstructed: true,
						CreatedAt:     submittedAt,
					})
				}

				action := ""
				externalReference := ""
				occurredAt := int64(0)
				switch withdrawal.Status {
				case PromotionWithdrawalStatusPendingReview:
				case PromotionWithdrawalStatusApproved:
					action = PromotionWithdrawalActionApproved
					occurredAt = withdrawal.ReviewedAt
				case PromotionWithdrawalStatusProcessing:
					action = PromotionWithdrawalActionPayoutInitiated
					externalReference = strings.TrimSpace(withdrawal.TradeNo)
					occurredAt = withdrawal.PayoutInitiatedAt
				case PromotionWithdrawalStatusPaid:
					action = PromotionWithdrawalActionPaid
					externalReference = strings.TrimSpace(withdrawal.TradeNo)
					occurredAt = withdrawal.PaidAt
				case PromotionWithdrawalStatusRejected:
					action = PromotionWithdrawalActionRejected
					occurredAt = withdrawal.ReviewedAt
				case PromotionWithdrawalStatusFailed:
					action = PromotionWithdrawalActionPayoutFailed
					externalReference = strings.TrimSpace(withdrawal.TradeNo)
					occurredAt = withdrawal.ReviewedAt
				default:
					return fmt.Errorf("unknown promotion withdrawal status %q for row %d", withdrawal.Status, withdrawal.Id)
				}
				requiresExternalReference := action == PromotionWithdrawalActionPayoutInitiated ||
					action == PromotionWithdrawalActionPaid || action == PromotionWithdrawalActionPayoutFailed
				_, hasCurrentAction := existingActions[action]
				if action == PromotionWithdrawalActionPayoutFailed {
					_, wasCancelledByRefund := existingActions[PromotionWithdrawalActionCancelledByRefund]
					hasCurrentAction = hasCurrentAction || wasCancelledByRefund
				}
				if action != "" && !hasCurrentAction && occurredAt > 0 && (!requiresExternalReference || externalReference != "") {
					actorType := PromotionWithdrawalActorAdmin
					if withdrawal.ReviewerId <= 0 {
						actorType = PromotionWithdrawalActorLegacy
					}
					operations = append(operations, PromotionWithdrawalOperation{
						WithdrawalId:      withdrawal.Id,
						Action:            action,
						ActorType:         actorType,
						ActorId:           withdrawal.ReviewerId,
						Note:              strings.TrimSpace(withdrawal.ReviewNote),
						ExternalReference: externalReference,
						Reconstructed:     true,
						CreatedAt:         occurredAt,
					})
				}
				if len(operations) == 0 {
					continue
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&operations).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		cursor = withdrawals[len(withdrawals)-1].Id
	}
}
