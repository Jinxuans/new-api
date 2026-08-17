package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

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
	PromotionWithdrawalStatusProcessing    = "processing"
	PromotionWithdrawalStatusPaid          = "paid"
	PromotionWithdrawalStatusRejected      = "rejected"
	PromotionWithdrawalStatusFailed        = "failed"

	PromotionWithdrawalActionSubmitted         = "submitted"
	PromotionWithdrawalActionApproved          = "approved"
	PromotionWithdrawalActionPayoutInitiated   = "payout_initiated"
	PromotionWithdrawalActionPayoutFailed      = "payout_failed"
	PromotionWithdrawalActionRejected          = "rejected"
	PromotionWithdrawalActionPaid              = "paid"
	PromotionWithdrawalActionCancelledByRefund = "cancelled_by_refund"

	PromotionWithdrawalActorUser   = "user"
	PromotionWithdrawalActorAdmin  = "admin"
	PromotionWithdrawalActorSystem = "system"
	PromotionWithdrawalActorLegacy = "legacy"
)

func isKnownPromotionCommissionStatus(status string) bool {
	switch status {
	case PromotionCommissionStatusPending,
		PromotionCommissionStatusSettled,
		PromotionCommissionStatusWithdrawing,
		PromotionCommissionStatusWithdrawn,
		PromotionCommissionStatusTransferred,
		PromotionCommissionStatusReversed:
		return true
	default:
		return false
	}
}

var (
	ErrPromotionWithdrawalLedgerNotPayable        = errors.New("withdrawal contains a commission that is no longer eligible for payout")
	ErrPromotionWithdrawalPayoutReferenceRequired = errors.New("payout trade number is required")
	ErrPromotionWithdrawalFailureReasonRequired   = errors.New("payout failure reason is required")
	ErrPromotionWithdrawalFailureConflict         = errors.New("withdrawal payout failure was already recorded with a different payload")
	ErrPromotionWithdrawalPaidConflict            = errors.New("withdrawal payout confirmation was already recorded with a different payload")
	ErrPromotionWithdrawalOperationImmutable      = errors.New("withdrawal operation history is immutable")
)

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
	Id                    int                             `json:"id"`
	UserId                int                             `json:"user_id" gorm:"index"`
	Currency              string                          `json:"currency" gorm:"type:varchar(16);index"`
	GrossAmountCents      int64                           `json:"gross_amount_cents"`
	FeeAmountCents        int64                           `json:"fee_amount_cents"`
	TaxAmountCents        int64                           `json:"tax_amount_cents"`
	NetAmountCents        int64                           `json:"net_amount_cents"`
	Status                string                          `json:"status" gorm:"type:varchar(32);index"`
	PayoutMethod          string                          `json:"payout_method" gorm:"type:varchar(32);index"`
	PayoutAccountSnapshot string                          `json:"payout_account_snapshot" gorm:"type:text"`
	TradeNo               string                          `json:"trade_no" gorm:"type:varchar(255);index"`
	ReviewerId            int                             `json:"reviewer_id" gorm:"index"`
	ReviewNote            string                          `json:"review_note" gorm:"type:text"`
	AppliedAt             int64                           `json:"applied_at" gorm:"index"`
	ReviewedAt            int64                           `json:"reviewed_at" gorm:"index"`
	PayoutInitiatedAt     int64                           `json:"payout_initiated_at" gorm:"index"`
	PaidAt                int64                           `json:"paid_at" gorm:"index"`
	CreatedAt             int64                           `json:"created_at" gorm:"index"`
	Operations            []*PromotionWithdrawalOperation `json:"operations" gorm:"foreignKey:WithdrawalId"`
}

type PromotionWithdrawalItem struct {
	Id           int   `json:"id"`
	WithdrawalId int   `json:"withdrawal_id" gorm:"index"`
	LedgerId     int   `json:"ledger_id" gorm:"index"`
	AmountCents  int64 `json:"amount_cents"`
	CreatedAt    int64 `json:"created_at" gorm:"index"`
}

// PromotionWithdrawalOperation is an append-only audit entry for one
// successful withdrawal state transition. The unique action constraint also
// protects the current one-way lifecycle from duplicate audit entries.
type PromotionWithdrawalOperation struct {
	Id                int    `json:"id"`
	WithdrawalId      int    `json:"withdrawal_id" gorm:"uniqueIndex:idx_promotion_withdrawal_operation,priority:1"`
	Action            string `json:"action" gorm:"type:varchar(32);uniqueIndex:idx_promotion_withdrawal_operation,priority:2"`
	ActorType         string `json:"actor_type" gorm:"type:varchar(16);index"`
	ActorId           int    `json:"actor_id" gorm:"index"`
	Note              string `json:"note" gorm:"type:text"`
	ExternalReference string `json:"external_reference" gorm:"type:varchar(255);index"`
	Reconstructed     bool   `json:"reconstructed"`
	CreatedAt         int64  `json:"created_at" gorm:"index"`
}

func (ledger *PromotionCommissionLedger) BeforeCreate(_ *gorm.DB) error {
	if ledger.CreatedAt == 0 {
		ledger.CreatedAt = common.GetTimestamp()
	}
	ledger.Currency = strings.ToUpper(strings.TrimSpace(ledger.Currency))
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

func (operation *PromotionWithdrawalOperation) BeforeCreate(_ *gorm.DB) error {
	if operation.CreatedAt == 0 && !operation.Reconstructed {
		operation.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func (operation *PromotionWithdrawalOperation) BeforeUpdate(_ *gorm.DB) error {
	return ErrPromotionWithdrawalOperationImmutable
}

func (operation *PromotionWithdrawalOperation) BeforeDelete(_ *gorm.DB) error {
	return ErrPromotionWithdrawalOperationImmutable
}

func CreatePromotionWithdrawalOperationTx(tx *gorm.DB, operation *PromotionWithdrawalOperation) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if operation == nil || operation.WithdrawalId <= 0 {
		return errors.New("invalid withdrawal operation")
	}

	expectedActorType := PromotionWithdrawalActorAdmin
	switch operation.Action {
	case PromotionWithdrawalActionSubmitted:
		expectedActorType = PromotionWithdrawalActorUser
	case PromotionWithdrawalActionApproved, PromotionWithdrawalActionPayoutInitiated, PromotionWithdrawalActionPayoutFailed, PromotionWithdrawalActionRejected, PromotionWithdrawalActionPaid:
	case PromotionWithdrawalActionCancelledByRefund:
		expectedActorType = PromotionWithdrawalActorSystem
	default:
		return errors.New("invalid withdrawal operation")
	}
	if operation.ActorType != expectedActorType {
		return errors.New("invalid withdrawal operation actor")
	}
	if operation.ActorType == PromotionWithdrawalActorSystem {
		if operation.ActorId != 0 {
			return errors.New("invalid withdrawal operation actor")
		}
	} else if operation.ActorId <= 0 {
		return errors.New("invalid withdrawal operation actor")
	}
	if operation.ActorId > 0 {
		var withdrawal PromotionWithdrawal
		if err := tx.Select("user_id").Where("id = ?", operation.WithdrawalId).First(&withdrawal).Error; err != nil {
			return err
		}
		lockedUsers, err := lockUsersForFinancialWriteTx(tx, operation.ActorId, withdrawal.UserId)
		if err != nil {
			return err
		}
		if lockedUsers[operation.ActorId].DeletedAt.Valid {
			return gorm.ErrRecordNotFound
		}
		if operation.ActorType == PromotionWithdrawalActorUser && operation.ActorId != withdrawal.UserId {
			return errors.New("invalid withdrawal operation actor")
		}
		if operation.ActorType == PromotionWithdrawalActorAdmin &&
			lockedUsers[operation.ActorId].Role != common.RoleAdminUser &&
			lockedUsers[operation.ActorId].Role != common.RoleRootUser {
			return errors.New("invalid withdrawal operation actor")
		}
	}
	operation.Note = strings.TrimSpace(operation.Note)
	operation.ExternalReference = strings.TrimSpace(operation.ExternalReference)
	if (operation.Action == PromotionWithdrawalActionPayoutInitiated ||
		operation.Action == PromotionWithdrawalActionPayoutFailed ||
		operation.Action == PromotionWithdrawalActionPaid ||
		operation.Action == PromotionWithdrawalActionCancelledByRefund) && operation.ExternalReference == "" {
		return ErrPromotionWithdrawalPayoutReferenceRequired
	}
	return tx.Create(operation).Error
}

func LockPromotionWithdrawalTx(tx *gorm.DB, id int) (*PromotionWithdrawal, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if id <= 0 {
		return nil, errors.New("invalid withdrawal")
	}
	var withdrawal PromotionWithdrawal
	if err := lockForUpdate(tx).Where("id = ?", id).First(&withdrawal).Error; err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

// EnsurePromotionFundOutflowAllowedTx is the shared durable barrier for every
// referral-credit or cash-commission outflow. The user row lock serializes the
// check with refund recovery updates; the obligation query also protects
// legacy/inconsistent rows where refund_hold was not persisted correctly.
func EnsurePromotionFundOutflowAllowedTx(tx *gorm.DB, userId int) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if userId <= 0 {
		return errors.New("invalid user")
	}
	held, err := isPromotionFundOutflowFenced(userId)
	if err != nil {
		return err
	}
	if held {
		return ErrUserRefundHeld
	}

	var user User
	if err := lockForUpdate(tx).Select("id", "refund_hold").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.RefundHold {
		return ErrUserRefundHeld
	}
	var openObligations int64
	if err := tx.Model(&PromotionRefundObligation{}).
		Where("user_id = ? AND status = ?", userId, PromotionRefundObligationStatusOpen).
		Count(&openObligations).Error; err != nil {
		return err
	}
	if openObligations > 0 {
		return ErrUserRefundHeld
	}
	held, err = isPromotionFundOutflowFenced(userId)
	if err != nil {
		return err
	}
	if held {
		return ErrUserRefundHeld
	}
	return nil
}

func isPromotionFundOutflowFenced(userId int) (bool, error) {
	if isLocalUserRefundHeld(userId) {
		return true, nil
	}
	if !common.RedisEnabled {
		return false, nil
	}
	exists, err := common.RDB.Exists(context.Background(), userRefundHoldKey(userId)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func LoadPromotionWithdrawalOperationsTx(tx *gorm.DB, withdrawal *PromotionWithdrawal) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if withdrawal == nil || withdrawal.Id <= 0 {
		return errors.New("invalid withdrawal")
	}
	return tx.Where("withdrawal_id = ?", withdrawal.Id).
		Order("created_at ASC").
		Order("id ASC").
		Find(&withdrawal.Operations).Error
}

// validatePromotionWithdrawalLedgerIntegrityTx proves that a withdrawal is
// exactly the sum of the commission ledgers assigned to it. Status-specific
// checks stay with the caller, while ownership, currency, and amount
// invariants are shared by payout, refund, and migration paths.
func validatePromotionWithdrawalLedgerIntegrityTx(tx *gorm.DB, withdrawal *PromotionWithdrawal) ([]PromotionWithdrawalItem, []PromotionCommissionLedger, error) {
	if tx == nil {
		return nil, nil, errors.New("transaction is required")
	}
	if withdrawal == nil || withdrawal.Id <= 0 || withdrawal.UserId <= 0 ||
		withdrawal.GrossAmountCents <= 0 || withdrawal.FeeAmountCents < 0 ||
		withdrawal.TaxAmountCents < 0 || withdrawal.NetAmountCents <= 0 {
		return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
	}
	currency := strings.ToUpper(strings.TrimSpace(withdrawal.Currency))
	if withdrawal.Currency != currency || !isISOCurrencyCode(currency) ||
		withdrawal.FeeAmountCents > withdrawal.GrossAmountCents ||
		withdrawal.TaxAmountCents > withdrawal.GrossAmountCents-withdrawal.FeeAmountCents ||
		withdrawal.NetAmountCents != withdrawal.GrossAmountCents-withdrawal.FeeAmountCents-withdrawal.TaxAmountCents {
		return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
	}

	var items []PromotionWithdrawalItem
	if err := tx.Where("withdrawal_id = ?", withdrawal.Id).Order("id ASC").Find(&items).Error; err != nil {
		return nil, nil, err
	}
	if len(items) == 0 {
		return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
	}

	ledgerIds := make([]int, 0, len(items))
	seenLedgerIds := make(map[int]struct{}, len(items))
	itemTotal := int64(0)
	for i := range items {
		item := &items[i]
		if item.WithdrawalId != withdrawal.Id || item.LedgerId <= 0 || item.AmountCents <= 0 ||
			itemTotal > math.MaxInt64-item.AmountCents {
			return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
		}
		if _, exists := seenLedgerIds[item.LedgerId]; exists {
			return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
		}
		seenLedgerIds[item.LedgerId] = struct{}{}
		ledgerIds = append(ledgerIds, item.LedgerId)
		itemTotal += item.AmountCents
	}
	if itemTotal != withdrawal.GrossAmountCents {
		return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
	}

	var ledgers []PromotionCommissionLedger
	if err := lockForUpdate(tx).Where("id IN ?", ledgerIds).Order("id ASC").Find(&ledgers).Error; err != nil {
		return nil, nil, err
	}
	if len(ledgers) != len(ledgerIds) {
		return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
	}
	ledgerById := make(map[int]*PromotionCommissionLedger, len(ledgers))
	for i := range ledgers {
		ledger := &ledgers[i]
		if ledger.UserId != withdrawal.UserId || ledger.NetAmountCents <= 0 ||
			strings.ToUpper(strings.TrimSpace(ledger.Currency)) != currency || ledger.Currency != currency {
			return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
		}
		ledgerById[ledger.Id] = ledger
	}
	for i := range items {
		ledger := ledgerById[items[i].LedgerId]
		if ledger == nil || items[i].AmountCents != ledger.NetAmountCents {
			return nil, nil, ErrPromotionWithdrawalLedgerNotPayable
		}
	}
	return items, ledgers, nil
}

// ValidatePromotionWithdrawalLedgerIntegrityTx verifies the immutable
// withdrawal-to-ledger relationship without requiring the commissions to
// remain payable. Rejection and failed-payout paths use this to safely return
// already-reserved ledgers after a later eligibility check has failed.
func ValidatePromotionWithdrawalLedgerIntegrityTx(tx *gorm.DB, withdrawalId int) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if withdrawalId <= 0 {
		return ErrPromotionWithdrawalLedgerNotPayable
	}
	withdrawal, err := LockPromotionWithdrawalTx(tx, withdrawalId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPromotionWithdrawalLedgerNotPayable
		}
		return err
	}
	_, _, err = validatePromotionWithdrawalLedgerIntegrityTx(tx, withdrawal)
	return err
}

// ValidatePromotionWithdrawalPaidTransactionTx treats the immutable payout
// journal as the economic proof that reserved commission already left the
// platform. A processing status alone is never proof of payment.
func ValidatePromotionWithdrawalPaidTransactionTx(tx *gorm.DB, withdrawal *PromotionWithdrawal) (bool, error) {
	if tx == nil {
		return false, errors.New("transaction is required")
	}
	if withdrawal == nil || withdrawal.Id <= 0 {
		return false, errors.New("invalid withdrawal")
	}

	var payout PromotionFundTransaction
	payoutKeys := []string{
		fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id),
		fmt.Sprintf("pfb:promotion_withdrawals:%d:paid", withdrawal.Id),
	}
	found := false
	for _, payoutKey := range payoutKeys {
		err := lockForUpdate(tx).Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
			return legTx.Order("id ASC")
		}).Where("transaction_key = ?", payoutKey).First(&payout).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		found = true
		break
	}
	if !found {
		return false, nil
	}

	items, ledgers, err := validatePromotionWithdrawalLedgerIntegrityTx(tx, withdrawal)
	if err != nil {
		return true, err
	}
	if payout.Kind != PromotionFundKindCommissionWithdrawalPaid ||
		payout.UserId != withdrawal.UserId ||
		payout.SourceType != "promotion_withdrawals" ||
		payout.SourceId != withdrawal.Id ||
		payout.ExternalRef != withdrawal.TradeNo ||
		len(payout.Legs) != len(items) {
		return true, ErrPromotionFundTransactionConflict
	}

	for i := range items {
		item := items[i]
		leg := payout.Legs[i]
		if leg.Account != PromotionFundAccountCommissionReserved ||
			leg.Asset != PromotionFundAssetCash ||
			leg.Currency != withdrawal.Currency ||
			leg.Amount != -item.AmountCents ||
			leg.SourceType != "promotion_commission_ledgers" ||
			leg.SourceId != item.LedgerId {
			return true, ErrPromotionFundTransactionConflict
		}
	}

	// A later refund legitimately moves a paid ledger from withdrawn to
	// reversed. The immutable payout journal remains the proof that cash left
	// the platform, so both states are valid when confirming historical payment.
	for i := range ledgers {
		if ledgers[i].Status != PromotionCommissionStatusWithdrawn && ledgers[i].Status != PromotionCommissionStatusReversed {
			return true, ErrPromotionWithdrawalLedgerNotPayable
		}
	}
	return true, nil
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
	account := PromotionFundAccountCommissionPending
	kind := PromotionFundKindCommissionPendingAccrued
	if ledger.Status == PromotionCommissionStatusSettled {
		eventType = PromotionEventTypeCommissionSettled
		account = PromotionFundAccountCommissionAvailable
		kind = PromotionFundKindCommissionAvailableAccrued
	}
	if ledger.NetAmountCents > 0 {
		if err := CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
			TransactionKey: fmt.Sprintf("commission:%d:accrued", ledger.Id),
			Kind:           kind,
			UserId:         ledger.UserId,
			SourceType:     "promotion_commission_ledgers",
			SourceId:       ledger.Id,
			SourceKey:      fmt.Sprintf("%s:%d", ledger.SourceType, ledger.SourceId),
			ActorType:      "system",
			ExternalRef:    ledger.SourceTradeNo,
			Remark:         ledger.Remark,
			OccurredAt:     ledger.CreatedAt,
		}, []PromotionFundTransactionLeg{{
			Account: account, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		}}); err != nil {
			return err
		}
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
	if err := CreatePromotionCommissionEventTx(tx, &ledger, PromotionEventTypeCommissionSettled); err != nil {
		return err
	}
	if ledger.NetAmountCents <= 0 {
		return nil
	}
	return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("commission:%d:settled", ledger.Id),
		Kind:           PromotionFundKindCommissionSettled,
		UserId:         ledger.UserId,
		SourceType:     "promotion_commission_ledgers",
		SourceId:       ledger.Id,
		SourceKey:      fmt.Sprintf("%s:%d", ledger.SourceType, ledger.SourceId),
		ActorType:      "system",
		ExternalRef:    ledger.SourceTradeNo,
		Remark:         ledger.Remark,
		OccurredAt:     ledger.SettledAt,
	}, []PromotionFundTransactionLeg{
		{
			Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: -ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		},
		{
			Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		},
	})
}

// AvailablePromotionCommissionLedgersQuery is the single eligibility rule for
// cash commission shown as available or selected for an outflow.
func AvailablePromotionCommissionLedgersQuery(db *gorm.DB, userId int) *gorm.DB {
	verifiedRebateIds := db.Model(&InvitationRebate{}).
		Select("id").
		Where("paid_amount_verified = ?", true)
	return db.Model(&PromotionCommissionLedger{}).
		Where("user_id = ? AND status = ? AND cashable = ? AND currency = ?", userId, PromotionCommissionStatusSettled, true, "CNY").
		Where("source_type <> ? OR source_id IN (?)", PromotionCommissionSourceTopUpRebate, verifiedRebateIds)
}

// LockSettledPromotionCommissionLedgersTx returns the CNY commission rows
// eligible for a transfer or withdrawal while holding real row locks on
// MySQL/PostgreSQL. Callers must still use a status predicate on their UPDATE
// so SQLite and concurrent state transitions remain CAS-safe.
func LockSettledPromotionCommissionLedgersTx(tx *gorm.DB, userId int) ([]*PromotionCommissionLedger, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	var ledgers []*PromotionCommissionLedger
	err := lockForUpdate(AvailablePromotionCommissionLedgersQuery(tx, userId)).
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

	withdrawal, err := LockPromotionWithdrawalTx(tx, withdrawalId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPromotionWithdrawalLedgerNotPayable
		}
		return err
	}
	_, ledgers, err := validatePromotionWithdrawalLedgerIntegrityTx(tx, withdrawal)
	if err != nil {
		return err
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
	previousStatus := ledger.Status
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
	var legs []PromotionFundTransactionLeg
	originalKey := fmt.Sprintf("commission:%d:accrued", ledger.Id)
	backfillKey := fmt.Sprintf("pfb:promotion_commission_ledgers:%d:accrued", ledger.Id)
	switch previousStatus {
	case PromotionCommissionStatusPending:
		if ledger.NetAmountCents > 0 {
			legs = []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash,
				Currency: ledger.Currency, Amount: -ledger.NetAmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}}
		}
	case PromotionCommissionStatusSettled:
		if ledger.NetAmountCents > 0 {
			legs = []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash,
				Currency: ledger.Currency, Amount: -ledger.NetAmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}}
		}
	case PromotionCommissionStatusTransferred:
		originalKey = fmt.Sprintf("commission:%d:transferred", ledger.Id)
		backfillKey = fmt.Sprintf("pfb:promotion_commission_ledgers:%d:transferred", ledger.Id)
		if ledger.QuotaEquivalent > 0 {
			var user User
			if err := tx.Select("quota").Where("id = ?", ledger.UserId).First(&user).Error; err != nil {
				return err
			}
			balanceAfter := int64(user.Quota)
			legs = []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
				Amount: -int64(ledger.QuotaEquivalent), SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
				BalanceAfter: &balanceAfter,
			}}
		}
	}
	if len(legs) > 0 {
		transaction := &PromotionFundTransaction{
			TransactionKey: fmt.Sprintf("commission:%d:reversed", ledger.Id),
			Kind:           PromotionFundKindCommissionReversed,
			UserId:         ledger.UserId,
			SourceType:     "promotion_commission_ledgers",
			SourceId:       ledger.Id,
			SourceKey:      fmt.Sprintf("%s:%d", ledger.SourceType, ledger.SourceId),
			ActorType:      "system",
			ExternalRef:    refundTradeNo,
			Remark:         remark,
			OccurredAt:     ledger.ReversedAt,
		}
		var original PromotionFundTransaction
		originalErr := tx.Select("id").Where("transaction_key = ?", originalKey).First(&original).Error
		if errors.Is(originalErr, gorm.ErrRecordNotFound) {
			originalErr = tx.Select("id").Where("transaction_key = ?", backfillKey).First(&original).Error
		}
		if originalErr != nil && !errors.Is(originalErr, gorm.ErrRecordNotFound) {
			return originalErr
		}
		if originalErr == nil {
			transaction.ReversesTransactionId = original.Id
		}
		if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
			return err
		}
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

func GetPromotionWithdrawal(userId int, id int) (*PromotionWithdrawal, error) {
	var withdrawal PromotionWithdrawal
	err := DB.Where("id = ? AND user_id = ?", id, userId).First(&withdrawal).Error
	return &withdrawal, err
}

func GetPromotionWithdrawalById(id int) (*PromotionWithdrawal, error) {
	var withdrawal PromotionWithdrawal
	err := DB.Preload("Operations", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("created_at ASC").Order("id ASC")
	}).Where("id = ?", id).First(&withdrawal).Error
	return &withdrawal, err
}

func ListAdminPromotionWithdrawals(pageInfo *common.PageInfo, status string) ([]*PromotionWithdrawal, int64, error) {
	query := DB.Model(&PromotionWithdrawal{})
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var withdrawals []*PromotionWithdrawal
	err := query.Preload("Operations", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("created_at ASC").Order("id ASC")
	}).Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&withdrawals).Error
	return withdrawals, total, err
}
