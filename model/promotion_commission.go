package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PromotionCommissionSourceTopUpRebate = "topup_rebate"

	PromotionCommissionStatusPending     = "pending"
	PromotionCommissionStatusSettled     = "settled"
	PromotionCommissionStatusWithdrawing = "withdrawing"
	PromotionCommissionStatusWithdrawn   = "withdrawn"
	PromotionCommissionStatusTransferred = "transferred"
	PromotionCommissionStatusReversed    = "reversed"

	PromotionWithdrawalStatusPendingReview = "pending_review"
	PromotionWithdrawalStatusApproved      = "approved"
	PromotionWithdrawalStatusPaid          = "paid"
	PromotionWithdrawalStatusRejected      = "rejected"
	PromotionWithdrawalStatusFailed        = "failed"
)

var ErrPromotionWithdrawalLedgerNotPayable = errors.New("withdrawal contains a commission that is no longer eligible for payout")

type PromotionCommissionLedger struct {
	Id                  int    `json:"id"`
	UserId              int    `json:"user_id" gorm:"index"`
	InviteeId           int    `json:"invitee_id" gorm:"index"`
	SourceType          string `json:"source_type" gorm:"type:varchar(32);uniqueIndex:idx_promotion_commission_source"`
	SourceId            int    `json:"source_id" gorm:"uniqueIndex:idx_promotion_commission_source"`
	SourceTradeNo       string `json:"source_trade_no" gorm:"type:varchar(255);index"`
	Cashable            bool   `json:"cashable" gorm:"index"`
	Currency            string `json:"currency" gorm:"type:varchar(16);index"`
	GrossAmountCents    int64  `json:"gross_amount_cents"`
	FeeAmountCents      int64  `json:"fee_amount_cents"`
	TaxAmountCents      int64  `json:"tax_amount_cents"`
	NetAmountCents      int64  `json:"net_amount_cents"`
	QuotaEquivalent     int    `json:"quota_equivalent"`
	Status              string `json:"status" gorm:"type:varchar(32);index"`
	AvailableAt         int64  `json:"available_at" gorm:"index"`
	SettledAt           int64  `json:"settled_at" gorm:"index"`
	WithdrawnAt         int64  `json:"withdrawn_at" gorm:"index"`
	TransferredAt       int64  `json:"transferred_at" gorm:"index"`
	RefundTradeNo       string `json:"refund_trade_no" gorm:"type:varchar(255);index"`
	ReversalAmountCents int64  `json:"reversal_amount_cents"`
	ReversalQuota       int    `json:"reversal_quota"`
	ReversedAt          int64  `json:"reversed_at" gorm:"index"`
	RuleSnapshot        string `json:"rule_snapshot" gorm:"type:text"`
	PaymentSnapshot     string `json:"payment_snapshot" gorm:"type:text"`
	Remark              string `json:"remark" gorm:"type:text"`
	CreatedAt           int64  `json:"created_at" gorm:"index"`
}

type PromotionWithdrawal struct {
	Id                    int    `json:"id"`
	UserId                int    `json:"user_id" gorm:"index"`
	Currency              string `json:"currency" gorm:"type:varchar(16);index"`
	GrossAmountCents      int64  `json:"gross_amount_cents"`
	FeeAmountCents        int64  `json:"fee_amount_cents"`
	TaxAmountCents        int64  `json:"tax_amount_cents"`
	NetAmountCents        int64  `json:"net_amount_cents"`
	Status                string `json:"status" gorm:"type:varchar(32);index"`
	PayoutMethod          string `json:"payout_method" gorm:"type:varchar(32);index"`
	PayoutAccountSnapshot string `json:"payout_account_snapshot" gorm:"type:text"`
	TradeNo               string `json:"trade_no" gorm:"type:varchar(255);index"`
	ReviewerId            int    `json:"reviewer_id" gorm:"index"`
	ReviewNote            string `json:"review_note" gorm:"type:text"`
	AppliedAt             int64  `json:"applied_at" gorm:"index"`
	ReviewedAt            int64  `json:"reviewed_at" gorm:"index"`
	PaidAt                int64  `json:"paid_at" gorm:"index"`
	CreatedAt             int64  `json:"created_at" gorm:"index"`
}

type PromotionWithdrawalItem struct {
	Id           int   `json:"id"`
	WithdrawalId int   `json:"withdrawal_id" gorm:"index"`
	LedgerId     int   `json:"ledger_id" gorm:"index"`
	AmountCents  int64 `json:"amount_cents"`
	CreatedAt    int64 `json:"created_at" gorm:"index"`
}

func (ledger *PromotionCommissionLedger) BeforeCreate(_ *gorm.DB) error {
	if ledger.CreatedAt == 0 {
		ledger.CreatedAt = common.GetTimestamp()
	}
	if ledger.Currency == "" {
		ledger.Currency = "CNY"
	}
	if ledger.Status == "" {
		ledger.Status = PromotionCommissionStatusPending
	}
	if ledger.NetAmountCents == 0 {
		ledger.NetAmountCents = ledger.GrossAmountCents - ledger.FeeAmountCents - ledger.TaxAmountCents
	}
	return nil
}

func (withdrawal *PromotionWithdrawal) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if withdrawal.CreatedAt == 0 {
		withdrawal.CreatedAt = now
	}
	if withdrawal.AppliedAt == 0 {
		withdrawal.AppliedAt = now
	}
	if withdrawal.Currency == "" {
		withdrawal.Currency = "CNY"
	}
	if withdrawal.Status == "" {
		withdrawal.Status = PromotionWithdrawalStatusPendingReview
	}
	if withdrawal.NetAmountCents == 0 {
		withdrawal.NetAmountCents = withdrawal.GrossAmountCents - withdrawal.FeeAmountCents - withdrawal.TaxAmountCents
	}
	return nil
}

func (item *PromotionWithdrawalItem) BeforeCreate(_ *gorm.DB) error {
	if item.CreatedAt == 0 {
		item.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func CreatePromotionCommissionLedgerTx(tx *gorm.DB, ledger *PromotionCommissionLedger) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if ledger == nil || ledger.UserId <= 0 || ledger.SourceType == "" || ledger.SourceId <= 0 {
		return nil
	}
	if ledger.GrossAmountCents <= 0 || ledger.NetAmountCents < 0 {
		return nil
	}

	var existing PromotionCommissionLedger
	err := tx.Where("source_type = ? AND source_id = ?", ledger.SourceType, ledger.SourceId).First(&existing).Error
	if err == nil {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := tx.Create(ledger).Error; err != nil {
		return err
	}
	eventType := PromotionEventTypeCommissionPending
	if ledger.Status == PromotionCommissionStatusSettled {
		eventType = PromotionEventTypeCommissionSettled
	}
	return CreatePromotionCommissionEventTx(tx, ledger, eventType)
}

// FreezeUnverifiedTopUpPromotionCommissions prevents legacy estimated rebates
// from remaining cashable after payment verification was introduced. It is
// safe to run after every migration and intentionally also freezes orphaned
// top-up rebate ledgers whose source row cannot prove a verified payment.
func FreezeUnverifiedTopUpPromotionCommissions() error {
	verifiedRebateIds := DB.Model(&InvitationRebate{}).
		Select("id").
		Where("paid_amount_verified = ?", true)
	return DB.Model(&PromotionCommissionLedger{}).
		Where("source_type = ? AND cashable = ?", PromotionCommissionSourceTopUpRebate, true).
		Where("source_id NOT IN (?)", verifiedRebateIds).
		Update("cashable", false).Error
}

func SettlePromotionCommissionLedgerTx(tx *gorm.DB, sourceType string, sourceId int, settledAt int64) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if sourceType == "" || sourceId <= 0 {
		return nil
	}
	if settledAt <= 0 {
		settledAt = common.GetTimestamp()
	}
	res := tx.Model(&PromotionCommissionLedger{}).
		Where("source_type = ? AND source_id = ? AND status = ?", sourceType, sourceId, PromotionCommissionStatusPending).
		Updates(map[string]interface{}{
			"status":     PromotionCommissionStatusSettled,
			"settled_at": settledAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}
	var ledger PromotionCommissionLedger
	if err := tx.Where("source_type = ? AND source_id = ?", sourceType, sourceId).First(&ledger).Error; err != nil {
		return err
	}
	return CreatePromotionCommissionEventTx(tx, &ledger, PromotionEventTypeCommissionSettled)
}

// LockSettledPromotionCommissionLedgersTx returns the CNY commission rows
// eligible for a transfer or withdrawal while holding real row locks on
// MySQL/PostgreSQL. Callers must still use a status predicate on their UPDATE
// so SQLite and concurrent state transitions remain CAS-safe.
func LockSettledPromotionCommissionLedgersTx(tx *gorm.DB, userId int) ([]*PromotionCommissionLedger, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	verifiedRebateIds := tx.Model(&InvitationRebate{}).
		Select("id").
		Where("paid_amount_verified = ?", true)
	var ledgers []*PromotionCommissionLedger
	err := lockForUpdate(tx).
		Where("user_id = ? AND status = ? AND cashable = ? AND currency = ?", userId, PromotionCommissionStatusSettled, true, "CNY").
		Where("source_type <> ? OR source_id IN (?)", PromotionCommissionSourceTopUpRebate, verifiedRebateIds).
		Order("id ASC").
		Find(&ledgers).Error
	return ledgers, err
}

// ValidatePromotionWithdrawalLedgersPayableTx rechecks every ledger attached
// to a withdrawal immediately before approval or payout. This closes the
// upgrade window for withdrawal rows created before legacy unverified rebates
// were frozen.
func ValidatePromotionWithdrawalLedgersPayableTx(tx *gorm.DB, withdrawalId int) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if withdrawalId <= 0 {
		return ErrPromotionWithdrawalLedgerNotPayable
	}

	var items []PromotionWithdrawalItem
	if err := tx.Where("withdrawal_id = ?", withdrawalId).Find(&items).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return ErrPromotionWithdrawalLedgerNotPayable
	}

	ledgerIds := make([]int, 0, len(items))
	seenLedgerIds := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item.LedgerId <= 0 {
			return ErrPromotionWithdrawalLedgerNotPayable
		}
		if _, exists := seenLedgerIds[item.LedgerId]; exists {
			return ErrPromotionWithdrawalLedgerNotPayable
		}
		seenLedgerIds[item.LedgerId] = struct{}{}
		ledgerIds = append(ledgerIds, item.LedgerId)
	}

	var ledgers []PromotionCommissionLedger
	if err := lockForUpdate(tx).Where("id IN ?", ledgerIds).Find(&ledgers).Error; err != nil {
		return err
	}
	if len(ledgers) != len(ledgerIds) {
		return ErrPromotionWithdrawalLedgerNotPayable
	}

	verifiedRebateIds := make(map[int]struct{})
	var sourceIds []int
	for _, ledger := range ledgers {
		if ledger.SourceType == PromotionCommissionSourceTopUpRebate {
			sourceIds = append(sourceIds, ledger.SourceId)
		}
	}
	if len(sourceIds) > 0 {
		var ids []int
		if err := tx.Model(&InvitationRebate{}).
			Where("id IN ? AND paid_amount_verified = ?", sourceIds, true).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			verifiedRebateIds[id] = struct{}{}
		}
	}

	for _, ledger := range ledgers {
		if ledger.Status != PromotionCommissionStatusWithdrawing || !ledger.Cashable || ledger.Currency != "CNY" {
			return ErrPromotionWithdrawalLedgerNotPayable
		}
		if ledger.SourceType == PromotionCommissionSourceTopUpRebate {
			if _, verified := verifiedRebateIds[ledger.SourceId]; !verified {
				return ErrPromotionWithdrawalLedgerNotPayable
			}
		}
	}
	return nil
}

func ReversePromotionCommissionLedgerTx(tx *gorm.DB, sourceType string, sourceId int, refundTradeNo string, remark string) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if sourceType == "" || sourceId <= 0 {
		return nil
	}
	var ledger PromotionCommissionLedger
	err := lockForUpdate(tx).
		Where("source_type = ? AND source_id = ?", sourceType, sourceId).
		First(&ledger).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if ledger.Status == PromotionCommissionStatusReversed {
		return nil
	}
	if ledger.Status == PromotionCommissionStatusWithdrawing || ledger.Status == PromotionCommissionStatusWithdrawn {
		return fmt.Errorf("commission ledger is %s; manual reversal required", ledger.Status)
	}
	if ledger.Status == PromotionCommissionStatusTransferred && ledger.QuotaEquivalent > 0 {
		minimumCurrentQuota := common.MinQuota + ledger.QuotaEquivalent
		result := tx.Model(&User{}).
			Where("id = ? AND quota >= ?", ledger.UserId, minimumCurrentQuota).
			Update("quota", gorm.Expr("quota - ?", ledger.QuotaEquivalent))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("commission reversal would exceed wallet quota range")
		}
	}
	result := tx.Model(&PromotionCommissionLedger{}).
		Where("id = ? AND status = ?", ledger.Id, ledger.Status).
		Updates(map[string]interface{}{
			"status":                PromotionCommissionStatusReversed,
			"refund_trade_no":       refundTradeNo,
			"reversal_amount_cents": ledger.NetAmountCents,
			"reversal_quota":        ledger.QuotaEquivalent,
			"reversed_at":           common.GetTimestamp(),
			"remark":                remark,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("commission status changed, please retry")
	}
	if err := tx.Where("id = ?", ledger.Id).First(&ledger).Error; err != nil {
		return err
	}
	return CreatePromotionCommissionEventTx(tx, &ledger, PromotionEventTypeCommissionReversed)
}

func ListPromotionCommissionLedgers(userId int, pageInfo *common.PageInfo) ([]*PromotionCommissionLedger, int64, error) {
	var total int64
	if err := DB.Model(&PromotionCommissionLedger{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ledgers []*PromotionCommissionLedger
	err := DB.Where("user_id = ?", userId).
		Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&ledgers).Error
	return ledgers, total, err
}

func ListPromotionWithdrawals(userId int, pageInfo *common.PageInfo) ([]*PromotionWithdrawal, int64, error) {
	var total int64
	if err := DB.Model(&PromotionWithdrawal{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var withdrawals []*PromotionWithdrawal
	err := DB.Where("user_id = ?", userId).
		Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&withdrawals).Error
	return withdrawals, total, err
}
