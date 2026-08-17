package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPromotionWithdrawalPayoutReferenceAlreadyUsed = errors.New("payout reference was already used by another withdrawal")
	ErrPromotionWithdrawalPayoutReferenceConflict    = errors.New("withdrawal already has a different payout reference")
	ErrPromotionWithdrawalPayoutReferenceImmutable   = errors.New("payout reference claims are immutable")
)

// PromotionWithdrawalPayoutReference permanently claims one normalized
// external payout reference. Keeping claims in a separate table allows the
// unique key to exclude historical withdrawals whose trade_no is empty, and
// avoids database-specific NULL/empty-string uniqueness behavior.
type PromotionWithdrawalPayoutReference struct {
	Id                int    `json:"id"`
	ReferenceKey      string `json:"reference_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	ExternalReference string `json:"external_reference" gorm:"type:varchar(255);not null"`
	PayoutMethod      string `json:"payout_method" gorm:"type:varchar(32);not null"`
	WithdrawalId      int    `json:"withdrawal_id" gorm:"not null;uniqueIndex"`
	CreatedAt         int64  `json:"created_at" gorm:"not null;index"`
}

func (claim *PromotionWithdrawalPayoutReference) BeforeCreate(_ *gorm.DB) error {
	if claim.CreatedAt == 0 {
		claim.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func (*PromotionWithdrawalPayoutReference) BeforeUpdate(_ *gorm.DB) error {
	return ErrPromotionWithdrawalPayoutReferenceImmutable
}

func (*PromotionWithdrawalPayoutReference) BeforeDelete(_ *gorm.DB) error {
	return ErrPromotionWithdrawalPayoutReferenceImmutable
}

func ClaimPromotionWithdrawalPayoutReferenceTx(tx *gorm.DB, withdrawal *PromotionWithdrawal, externalReference string) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if withdrawal == nil || withdrawal.Id <= 0 {
		return errors.New("invalid withdrawal")
	}
	externalReference = strings.TrimSpace(externalReference)
	if externalReference == "" {
		return ErrPromotionWithdrawalPayoutReferenceRequired
	}
	if utf8.RuneCountInString(externalReference) > promotionFundReferenceMaxRunes {
		return fmt.Errorf("payout reference cannot exceed %d characters", promotionFundReferenceMaxRunes)
	}
	canonicalReference := strings.ToUpper(externalReference)
	referenceKey := fmt.Sprintf("%x", common.Sha256Raw([]byte(canonicalReference)))
	claim := &PromotionWithdrawalPayoutReference{
		ReferenceKey:      referenceKey,
		ExternalReference: externalReference,
		PayoutMethod:      strings.ToLower(strings.TrimSpace(withdrawal.PayoutMethod)),
		WithdrawalId:      withdrawal.Id,
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(claim)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	var referenceClaim PromotionWithdrawalPayoutReference
	err := tx.Where("reference_key = ?", referenceKey).First(&referenceClaim).Error
	if err == nil {
		if referenceClaim.WithdrawalId == withdrawal.Id &&
			strings.EqualFold(referenceClaim.ExternalReference, externalReference) {
			return nil
		}
		return ErrPromotionWithdrawalPayoutReferenceAlreadyUsed
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var withdrawalClaim PromotionWithdrawalPayoutReference
	err = tx.Where("withdrawal_id = ?", withdrawal.Id).First(&withdrawalClaim).Error
	if err == nil {
		return ErrPromotionWithdrawalPayoutReferenceConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return ErrPromotionWithdrawalPayoutReferenceConflict
}

// BackfillPromotionWithdrawalPayoutReferences reserves every non-empty payout
// reference that already reached processing, including failed payouts. A
// failed external reference remains permanently unavailable for reuse.
func BackfillPromotionWithdrawalPayoutReferences(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	cursor := 0
	for {
		var withdrawals []PromotionWithdrawal
		if err := db.Where(`id > ? AND trade_no <> ?
			AND (status IN ? OR (status = ? AND payout_initiated_at > ?))
			AND NOT EXISTS (
				SELECT 1 FROM promotion_withdrawal_payout_references
				WHERE promotion_withdrawal_payout_references.withdrawal_id = promotion_withdrawals.id
			)`,
			cursor, "", []string{PromotionWithdrawalStatusProcessing, PromotionWithdrawalStatusPaid},
			PromotionWithdrawalStatusFailed, 0).
			Order("id ASC").Limit(200).Find(&withdrawals).Error; err != nil {
			return err
		}
		if len(withdrawals) == 0 {
			return nil
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			for i := range withdrawals {
				withdrawal := &withdrawals[i]
				if strings.TrimSpace(withdrawal.TradeNo) == "" {
					continue
				}
				if err := ClaimPromotionWithdrawalPayoutReferenceTx(tx, withdrawal, withdrawal.TradeNo); err != nil {
					if errors.Is(err, ErrPromotionWithdrawalPayoutReferenceAlreadyUsed) ||
						errors.Is(err, ErrPromotionWithdrawalPayoutReferenceConflict) {
						common.SysError(fmt.Sprintf("historical payout reference conflict: withdrawal_id=%d", withdrawal.Id))
						continue
					}
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
