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

const (
	promotionFundReferenceMaxRunes = 255

	PromotionFundAccountAPIBalance          = "api_balance"
	PromotionFundAccountReferralCredit      = "referral_credit"
	PromotionFundAccountCommissionPending   = "commission_pending"
	PromotionFundAccountCommissionAvailable = "commission_available"
	PromotionFundAccountCommissionReserved  = "commission_reserved"
	PromotionFundAccountRefundDebt          = "refund_debt"

	PromotionFundAssetQuota = "quota"
	PromotionFundAssetCash  = "cash"
)

var (
	ErrPromotionFundTransactionRequired           = errors.New("promotion fund transaction is required")
	ErrPromotionFundTransactionKeyInvalid         = errors.New("promotion fund transaction key is invalid")
	ErrPromotionFundTransactionKindInvalid        = errors.New("promotion fund transaction kind is invalid")
	ErrPromotionFundTransactionUserInvalid        = errors.New("promotion fund transaction user is invalid")
	ErrPromotionFundTransactionSourceKeyInvalid   = errors.New("promotion fund transaction source key is invalid")
	ErrPromotionFundTransactionExternalRefInvalid = errors.New("promotion fund transaction external reference is invalid")
	ErrPromotionFundTransactionLegRequired        = errors.New("promotion fund transaction requires at least one leg")
	ErrPromotionFundTransactionLegInvalid         = errors.New("promotion fund transaction leg is invalid")
	ErrPromotionFundReversalNotFound              = errors.New("reversed promotion fund transaction not found")
	ErrPromotionFundReversalUserMismatch          = errors.New("reversed promotion fund transaction belongs to another user")
	ErrPromotionFundTransactionImmutable          = errors.New("promotion fund transaction history is immutable")
	ErrPromotionFundTransactionConflict           = errors.New("promotion fund transaction key was already used for a different payload")
)

type PromotionFundTransaction struct {
	Id                    int                           `json:"id"`
	TransactionKey        string                        `json:"transaction_key" gorm:"type:varchar(128);not null;uniqueIndex"`
	Kind                  string                        `json:"kind" gorm:"type:varchar(64);not null;index"`
	UserId                int                           `json:"user_id" gorm:"not null;index:idx_promotion_fund_user_time,priority:1"`
	SourceType            string                        `json:"source_type" gorm:"type:varchar(64);index:idx_promotion_fund_source,priority:1"`
	SourceId              int                           `json:"source_id" gorm:"index:idx_promotion_fund_source,priority:2"`
	SourceKey             string                        `json:"source_key" gorm:"type:varchar(255);index"`
	ReversesTransactionId int                           `json:"reverses_transaction_id" gorm:"index"`
	ActorType             string                        `json:"actor_type" gorm:"type:varchar(32);index"`
	ActorId               int                           `json:"actor_id" gorm:"index"`
	ActorRef              string                        `json:"actor_ref" gorm:"type:varchar(191)"`
	ExternalRef           string                        `json:"external_ref" gorm:"type:varchar(255);index"`
	Remark                string                        `json:"remark" gorm:"type:text"`
	OccurredAt            int64                         `json:"occurred_at" gorm:"not null;index:idx_promotion_fund_user_time,priority:2"`
	CreatedAt             int64                         `json:"created_at" gorm:"not null;index"`
	Legs                  []PromotionFundTransactionLeg `json:"legs" gorm:"foreignKey:TransactionId"`
}

type PromotionFundTransactionLeg struct {
	Id            int    `json:"id"`
	TransactionId int    `json:"transaction_id" gorm:"not null;index"`
	Account       string `json:"account" gorm:"type:varchar(64);not null;index"`
	Asset         string `json:"asset" gorm:"type:varchar(16);not null;index"`
	Currency      string `json:"currency" gorm:"type:varchar(3);index"`
	Amount        int64  `json:"amount" gorm:"type:bigint;not null"`
	SourceType    string `json:"source_type" gorm:"type:varchar(64);index:idx_promotion_fund_leg_source,priority:1"`
	SourceId      int    `json:"source_id" gorm:"index:idx_promotion_fund_leg_source,priority:2"`
	BalanceAfter  *int64 `json:"balance_after" gorm:"type:bigint"`
}

func (PromotionFundTransaction) TableName() string {
	return "promotion_fund_transactions"
}

func (PromotionFundTransactionLeg) TableName() string {
	return "promotion_fund_legs"
}

func (transaction *PromotionFundTransaction) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if transaction.OccurredAt == 0 {
		transaction.OccurredAt = now
	}
	if transaction.CreatedAt == 0 {
		transaction.CreatedAt = now
	}
	return nil
}

func (*PromotionFundTransaction) BeforeUpdate(_ *gorm.DB) error {
	return ErrPromotionFundTransactionImmutable
}

func (*PromotionFundTransaction) BeforeDelete(_ *gorm.DB) error {
	return ErrPromotionFundTransactionImmutable
}

func (*PromotionFundTransactionLeg) BeforeUpdate(_ *gorm.DB) error {
	return ErrPromotionFundTransactionImmutable
}

func (*PromotionFundTransactionLeg) BeforeDelete(_ *gorm.DB) error {
	return ErrPromotionFundTransactionImmutable
}

// CreatePromotionFundTransactionTx records an immutable transaction header and
// all of its legs through the caller's transaction. Reusing TransactionKey is
// idempotent: transaction is replaced with the previously persisted header and
// its legs.
func CreatePromotionFundTransactionTx(tx *gorm.DB, transaction *PromotionFundTransaction, legs []PromotionFundTransactionLeg) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	if transaction == nil {
		return ErrPromotionFundTransactionRequired
	}

	transaction.TransactionKey = strings.TrimSpace(transaction.TransactionKey)
	if transaction.TransactionKey == "" || len(transaction.TransactionKey) > 128 {
		return ErrPromotionFundTransactionKeyInvalid
	}
	transaction.Kind = strings.TrimSpace(transaction.Kind)
	if transaction.Kind == "" || len(transaction.Kind) > 64 {
		return ErrPromotionFundTransactionKindInvalid
	}
	if transaction.UserId <= 0 {
		return ErrPromotionFundTransactionUserInvalid
	}
	if utf8.RuneCountInString(transaction.SourceKey) > promotionFundReferenceMaxRunes {
		return ErrPromotionFundTransactionSourceKeyInvalid
	}
	if utf8.RuneCountInString(transaction.ExternalRef) > promotionFundReferenceMaxRunes {
		return ErrPromotionFundTransactionExternalRefInvalid
	}
	if len(legs) == 0 {
		return ErrPromotionFundTransactionLegRequired
	}

	for i := range legs {
		leg := &legs[i]
		leg.Account = strings.TrimSpace(leg.Account)
		leg.Asset = strings.TrimSpace(leg.Asset)
		leg.Currency = strings.TrimSpace(leg.Currency)

		switch leg.Account {
		case PromotionFundAccountAPIBalance, PromotionFundAccountReferralCredit:
			if leg.Asset != PromotionFundAssetQuota {
				return fmt.Errorf("%w: leg %d account %q requires quota asset", ErrPromotionFundTransactionLegInvalid, i, leg.Account)
			}
		case PromotionFundAccountCommissionPending, PromotionFundAccountCommissionAvailable, PromotionFundAccountCommissionReserved:
			if leg.Asset != PromotionFundAssetCash {
				return fmt.Errorf("%w: leg %d account %q requires cash asset", ErrPromotionFundTransactionLegInvalid, i, leg.Account)
			}
		case PromotionFundAccountRefundDebt:
			if leg.Asset != PromotionFundAssetQuota && leg.Asset != PromotionFundAssetCash {
				return fmt.Errorf("%w: leg %d has unsupported asset %q", ErrPromotionFundTransactionLegInvalid, i, leg.Asset)
			}
		default:
			return fmt.Errorf("%w: leg %d has unsupported account %q", ErrPromotionFundTransactionLegInvalid, i, leg.Account)
		}

		switch leg.Asset {
		case PromotionFundAssetQuota:
			if leg.Currency != "" {
				return fmt.Errorf("%w: leg %d quota asset must not have a currency", ErrPromotionFundTransactionLegInvalid, i)
			}
		case PromotionFundAssetCash:
			validCurrency := len(leg.Currency) == 3
			for j := 0; validCurrency && j < len(leg.Currency); j++ {
				validCurrency = leg.Currency[j] >= 'A' && leg.Currency[j] <= 'Z'
			}
			if !validCurrency {
				return fmt.Errorf("%w: leg %d cash currency must be three uppercase letters", ErrPromotionFundTransactionLegInvalid, i)
			}
		default:
			return fmt.Errorf("%w: leg %d has unsupported asset %q", ErrPromotionFundTransactionLegInvalid, i, leg.Asset)
		}
		if leg.Amount == 0 {
			return fmt.Errorf("%w: leg %d amount must not be zero", ErrPromotionFundTransactionLegInvalid, i)
		}
		if leg.SourceId < 0 {
			return fmt.Errorf("%w: leg %d source ID must not be negative", ErrPromotionFundTransactionLegInvalid, i)
		}
	}

	if transaction.ReversesTransactionId < 0 {
		return fmt.Errorf("%w: transaction ID must not be negative", ErrPromotionFundReversalNotFound)
	}
	if transaction.ReversesTransactionId > 0 {
		if transaction.Id > 0 && transaction.ReversesTransactionId == transaction.Id {
			return fmt.Errorf("%w: a transaction cannot reverse itself", ErrPromotionFundReversalNotFound)
		}
		var reversed PromotionFundTransaction
		err := lockForUpdate(tx).
			Select("id", "user_id").
			Where("id = ?", transaction.ReversesTransactionId).
			First(&reversed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %d", ErrPromotionFundReversalNotFound, transaction.ReversesTransactionId)
		}
		if err != nil {
			return err
		}
		if reversed.UserId != transaction.UserId {
			return ErrPromotionFundReversalUserMismatch
		}
	}

	requestedOccurredAt := transaction.OccurredAt
	header := *transaction
	header.Id = 0
	header.Legs = nil
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "transaction_key"}},
		DoNothing: true,
	}).Omit("Legs").Create(&header)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 || header.Id == 0 {
		var existing PromotionFundTransaction
		err := lockForUpdate(tx).
			Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
				return legTx.Order("id ASC")
			}).
			Where("transaction_key = ?", transaction.TransactionKey).
			First(&existing).Error
		if err != nil {
			return err
		}
		if !samePromotionFundTransactionPayload(&existing, &header, legs, requestedOccurredAt != 0) {
			return ErrPromotionFundTransactionConflict
		}
		*transaction = existing
		return nil
	}

	persistedLegs := make([]PromotionFundTransactionLeg, len(legs))
	copy(persistedLegs, legs)
	for i := range persistedLegs {
		persistedLegs[i].Id = 0
		persistedLegs[i].TransactionId = header.Id
	}
	if err := tx.Create(&persistedLegs).Error; err != nil {
		return err
	}
	header.Legs = persistedLegs
	*transaction = header
	return nil
}

func samePromotionFundTransactionPayload(existing *PromotionFundTransaction, requested *PromotionFundTransaction, legs []PromotionFundTransactionLeg, compareOccurredAt bool) bool {
	if existing == nil || requested == nil ||
		existing.TransactionKey != requested.TransactionKey ||
		existing.Kind != requested.Kind ||
		existing.UserId != requested.UserId ||
		existing.SourceType != requested.SourceType ||
		existing.SourceId != requested.SourceId ||
		existing.SourceKey != requested.SourceKey ||
		existing.ReversesTransactionId != requested.ReversesTransactionId ||
		existing.ActorType != requested.ActorType ||
		existing.ActorId != requested.ActorId ||
		existing.ActorRef != requested.ActorRef ||
		existing.ExternalRef != requested.ExternalRef ||
		existing.Remark != requested.Remark ||
		(compareOccurredAt && existing.OccurredAt != requested.OccurredAt) ||
		len(existing.Legs) != len(legs) {
		return false
	}
	for i := range legs {
		existingLeg := existing.Legs[i]
		requestedLeg := legs[i]
		if existingLeg.Account != requestedLeg.Account ||
			existingLeg.Asset != requestedLeg.Asset ||
			existingLeg.Currency != requestedLeg.Currency ||
			existingLeg.Amount != requestedLeg.Amount ||
			existingLeg.SourceType != requestedLeg.SourceType ||
			existingLeg.SourceId != requestedLeg.SourceId ||
			(existingLeg.BalanceAfter == nil) != (requestedLeg.BalanceAfter == nil) {
			return false
		}
		if existingLeg.BalanceAfter != nil && *existingLeg.BalanceAfter != *requestedLeg.BalanceAfter {
			return false
		}
	}
	return true
}
