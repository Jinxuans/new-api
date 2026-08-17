package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	AdminQuotaAdjustmentModeAdd      = "add"
	AdminQuotaAdjustmentModeSubtract = "subtract"
	AdminQuotaAdjustmentModeOverride = "override"

	PromotionFundKindAdminQuotaCredited   = "api_balance_admin_credited"
	PromotionFundKindAdminQuotaDebited    = "api_balance_admin_debited"
	PromotionFundKindAdminQuotaOverridden = "api_balance_admin_overridden"

	adminQuotaAdjustmentRemarkMaxRunes = 1000
)

var (
	ErrAdminQuotaAdjustmentInvalid        = errors.New("invalid administrator quota adjustment")
	ErrAdminQuotaAdjustmentOutOfRange     = errors.New("administrator quota adjustment is outside the wallet range")
	ErrAdminQuotaAdjustmentReasonRequired = errors.New("administrator quota adjustment reason is required")
	ErrAdminQuotaAdjustmentNoChange       = errors.New("administrator quota adjustment does not change the balance")
)

type AdminQuotaAdjustmentInput struct {
	UserId         int
	Mode           string
	Value          int
	ActorId        int
	ActorRef       string
	Remark         string
	IdempotencyKey string
}

type AdminQuotaAdjustmentResult struct {
	PreviousQuota     int  `json:"previous_quota"`
	CurrentQuota      int  `json:"current_quota"`
	FundTransactionId int  `json:"fund_transaction_id"`
	Replayed          bool `json:"replayed"`
}

// AdjustUserQuotaByAdmin commits the wallet mutation and its immutable fund
// record together. Reusing an idempotency key with the same request returns the
// original result without applying the balance change again.
func AdjustUserQuotaByAdmin(input AdminQuotaAdjustmentInput) (*AdminQuotaAdjustmentResult, error) {
	input.Mode = strings.TrimSpace(input.Mode)
	input.ActorRef = strings.TrimSpace(input.ActorRef)
	input.Remark = strings.TrimSpace(input.Remark)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.UserId <= 0 || input.ActorId <= 0 || len(input.ActorRef) > 191 ||
		utf8.RuneCountInString(input.Remark) > adminQuotaAdjustmentRemarkMaxRunes {
		return nil, ErrAdminQuotaAdjustmentInvalid
	}
	if input.Remark == "" {
		return nil, ErrAdminQuotaAdjustmentReasonRequired
	}
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 96 {
		return nil, ErrAdminQuotaAdjustmentInvalid
	}

	kind := ""
	switch input.Mode {
	case AdminQuotaAdjustmentModeAdd:
		kind = PromotionFundKindAdminQuotaCredited
	case AdminQuotaAdjustmentModeSubtract:
		kind = PromotionFundKindAdminQuotaDebited
	case AdminQuotaAdjustmentModeOverride:
		kind = PromotionFundKindAdminQuotaOverridden
	default:
		return nil, ErrAdminQuotaAdjustmentInvalid
	}
	if input.Mode != AdminQuotaAdjustmentModeOverride && input.Value <= 0 {
		return nil, ErrAdminQuotaAdjustmentInvalid
	}

	transactionKey := "admin_quota:" + input.IdempotencyKey
	sourceKey := fmt.Sprintf("users:%d:%s:%d", input.UserId, input.Mode, input.Value)
	var adjustmentResult *AdminQuotaAdjustmentResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		lockedUsers, err := lockActiveUsersForFinancialWriteTx(tx, input.ActorId, input.UserId)
		if err != nil {
			return err
		}
		user := lockedUsers[input.UserId]
		actor := lockedUsers[input.ActorId]
		if actor.Role != common.RoleAdminUser && actor.Role != common.RoleRootUser {
			return ErrAdminQuotaAdjustmentInvalid
		}

		var existing PromotionFundTransaction
		err = tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
			return legTx.Order("id ASC")
		}).Where("transaction_key = ?", transactionKey).First(&existing).Error
		if err == nil {
			if existing.UserId != input.UserId || existing.Kind != kind ||
				existing.SourceKey != sourceKey || existing.ActorId != input.ActorId ||
				existing.ActorRef != input.ActorRef || existing.Remark != input.Remark || len(existing.Legs) != 1 {
				return ErrPromotionFundTransactionConflict
			}
			leg := existing.Legs[0]
			if leg.Account != PromotionFundAccountAPIBalance || leg.Asset != PromotionFundAssetQuota || leg.BalanceAfter == nil {
				return ErrPromotionFundTransactionConflict
			}
			if (input.Mode == AdminQuotaAdjustmentModeAdd && leg.Amount != int64(input.Value)) ||
				(input.Mode == AdminQuotaAdjustmentModeSubtract && leg.Amount != -int64(input.Value)) ||
				(input.Mode == AdminQuotaAdjustmentModeOverride && *leg.BalanceAfter != int64(input.Value)) {
				return ErrPromotionFundTransactionConflict
			}
			previousQuota := *leg.BalanceAfter - leg.Amount
			adjustmentResult = &AdminQuotaAdjustmentResult{
				PreviousQuota:     int(previousQuota),
				CurrentQuota:      int(*leg.BalanceAfter),
				FundTransactionId: existing.Id,
				Replayed:          true,
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		previousQuota := int64(user.Quota)
		currentQuota := previousQuota
		switch input.Mode {
		case AdminQuotaAdjustmentModeAdd:
			currentQuota += int64(input.Value)
		case AdminQuotaAdjustmentModeSubtract:
			currentQuota -= int64(input.Value)
		case AdminQuotaAdjustmentModeOverride:
			currentQuota = int64(input.Value)
		}
		if currentQuota < int64(common.MinQuota) || currentQuota >= int64(common.MaxQuota) {
			return ErrAdminQuotaAdjustmentOutOfRange
		}
		if currentQuota == previousQuota {
			return ErrAdminQuotaAdjustmentNoChange
		}
		if currentQuota < previousQuota {
			if err := EnsurePromotionFundOutflowAllowedTx(tx, input.UserId); err != nil {
				return err
			}
		}

		result := tx.Model(&User{}).Where("id = ?", input.UserId).Update("quota", int(currentQuota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}

		balanceAfter := currentQuota
		transaction := &PromotionFundTransaction{
			TransactionKey: transactionKey,
			Kind:           kind,
			UserId:         input.UserId,
			SourceType:     "admin_quota_adjustments",
			SourceKey:      sourceKey,
			ActorType:      "admin",
			ActorId:        input.ActorId,
			ActorRef:       input.ActorRef,
			Remark:         input.Remark,
		}
		if err := CreatePromotionFundTransactionTx(tx, transaction, []PromotionFundTransactionLeg{{
			Account:      PromotionFundAccountAPIBalance,
			Asset:        PromotionFundAssetQuota,
			Amount:       currentQuota - previousQuota,
			SourceType:   "admin_quota_adjustments",
			BalanceAfter: &balanceAfter,
		}}); err != nil {
			return err
		}
		adjustmentResult = &AdminQuotaAdjustmentResult{
			PreviousQuota:     int(previousQuota),
			CurrentQuota:      int(currentQuota),
			FundTransactionId: transaction.Id,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	invalidateUserQuotaCacheAfterDBWrite(input.UserId, "administrator quota adjustment")
	return adjustmentResult, nil
}
