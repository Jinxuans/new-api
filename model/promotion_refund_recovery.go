package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PromotionRefundObligationStatusOpen      = "open"
	PromotionRefundObligationStatusRecovered = "recovered"
	PromotionRefundObligationStatusWaived    = "waived"

	PromotionRefundActionRetryWalletDebit            = "retry_wallet_debit"
	PromotionRefundActionRecordExternalRepayment     = "record_external_repayment"
	PromotionRefundActionRecoverPaidCommission       = "recover_paid_commission"
	PromotionRefundActionDefineManualObligation      = "define_manual_obligation"
	PromotionRefundActionQuarantineUnknownCommission = "quarantine_unknown_commission"
	PromotionRefundActionWaive                       = "waive"
	PromotionRefundActionReleaseHold                 = "release_hold"

	PromotionRefundReceiptPurposeRepayment      = "repayment"
	PromotionRefundReceiptPurposeManualEvidence = "manual_evidence"

	maxPromotionRefundIdempotencyKeyLength = 128
	maxPromotionRefundExternalRefLength    = 191
	maxPromotionRefundActionRemarkLength   = 1000
)

var (
	ErrPromotionRefundActionImmutable    = errors.New("refund recovery action history is immutable")
	ErrPromotionRefundObligationConflict = errors.New("refund obligation key was already used for a different payload")
)

type PromotionRefundObligation struct {
	Id              int    `json:"id"`
	ObligationKey   string `json:"obligation_key" gorm:"type:varchar(128);not null;uniqueIndex"`
	RefundCaseId    int    `json:"refund_case_id" gorm:"not null;index"`
	UserId          int    `json:"user_id" gorm:"not null;index"`
	Account         string `json:"account" gorm:"type:varchar(64);not null;index"`
	Asset           string `json:"asset" gorm:"type:varchar(16);not null;index"`
	Currency        string `json:"currency" gorm:"type:varchar(3);index"`
	Amount          int64  `json:"amount" gorm:"type:bigint;not null"`
	RecoveredAmount int64  `json:"recovered_amount" gorm:"type:bigint;not null"`
	WaivedAmount    int64  `json:"waived_amount" gorm:"type:bigint;not null"`
	SourceType      string `json:"source_type" gorm:"type:varchar(64);index"`
	SourceId        int    `json:"source_id" gorm:"index"`
	Status          string `json:"status" gorm:"type:varchar(32);not null;index"`
	CreatedAt       int64  `json:"created_at" gorm:"not null;index"`
	UpdatedAt       int64  `json:"updated_at" gorm:"not null;index"`
}

type PromotionRefundAction struct {
	Id                        int    `json:"id"`
	ActionKey                 string `json:"action_key" gorm:"type:varchar(128);not null;uniqueIndex"`
	RefundCaseId              int    `json:"refund_case_id" gorm:"not null;index;uniqueIndex:idx_refund_case_subscription_action,priority:1"`
	ObligationId              int    `json:"obligation_id" gorm:"index"`
	UserId                    int    `json:"user_id" gorm:"index"`
	TopUpId                   int    `json:"top_up_id" gorm:"index"`
	Action                    string `json:"action" gorm:"type:varchar(48);not null;index"`
	Asset                     string `json:"asset" gorm:"type:varchar(16);index"`
	Currency                  string `json:"currency" gorm:"type:varchar(3);index"`
	Amount                    int64  `json:"amount" gorm:"type:bigint"`
	ActorId                   int    `json:"actor_id" gorm:"not null;index"`
	ActorRole                 int    `json:"actor_role" gorm:"not null;index"`
	ExternalRef               string `json:"external_ref" gorm:"type:varchar(191);index"`
	Remark                    string `json:"remark" gorm:"type:text"`
	CommissionLedgerId        int    `json:"commission_ledger_id" gorm:"index"`
	CommissionLedgerStatus    string `json:"commission_ledger_status" gorm:"type:varchar(32);index"`
	ResponsibilityFingerprint string `json:"responsibility_fingerprint" gorm:"type:char(40);index"`
	UserSubscriptionId        *int   `json:"user_subscription_id,omitempty" gorm:"index;uniqueIndex:idx_refund_case_subscription_action,priority:2"`
	SubscriptionEndTimeBefore int64  `json:"subscription_end_time_before"`
	FundTransactionId         int    `json:"fund_transaction_id" gorm:"index"`
	CreatedAt                 int64  `json:"created_at" gorm:"not null;index"`
}

// PromotionRefundRecoveryReceipt reserves one external repayment reference
// for exactly one immutable recovery action. A dedicated table avoids
// dialect-specific partial unique indexes for actions without a reference.
type PromotionRefundRecoveryReceipt struct {
	Id           int    `json:"id"`
	ReceiptKey   string `json:"receipt_key" gorm:"type:char(40);not null;uniqueIndex"`
	Purpose      string `json:"purpose" gorm:"type:varchar(32);not null;index"`
	ExternalRef  string `json:"external_ref" gorm:"type:varchar(191);not null;index"`
	ActionKey    string `json:"action_key" gorm:"type:varchar(128);not null;uniqueIndex"`
	RefundCaseId int    `json:"refund_case_id" gorm:"not null;index"`
	ObligationId int    `json:"obligation_id" gorm:"not null;index"`
	CreatedAt    int64  `json:"created_at" gorm:"not null;index"`
}

type PromotionRefundRecoveryActionInput struct {
	RefundCaseId                      int
	IdempotencyKey                    string
	Action                            string
	ObligationId                      int
	UserId                            int
	TopUpId                           int
	Asset                             string
	Currency                          string
	Amount                            int64
	ActorId                           int
	ActorRole                         int
	ExternalRef                       string
	Remark                            string
	ExpectedCommissionLedgerId        int
	ExpectedCommissionLedgerStatus    *string
	ExpectedResponsibilityFingerprint string
	UserSubscriptionId                int
}

func (obligation *PromotionRefundObligation) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if obligation.CreatedAt == 0 {
		obligation.CreatedAt = now
	}
	if obligation.UpdatedAt == 0 {
		obligation.UpdatedAt = now
	}
	if obligation.Status == "" {
		obligation.Status = PromotionRefundObligationStatusOpen
	}
	return nil
}

func (action *PromotionRefundAction) BeforeCreate(_ *gorm.DB) error {
	if action.CreatedAt == 0 {
		action.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func (action *PromotionRefundAction) BeforeUpdate(_ *gorm.DB) error {
	return ErrPromotionRefundActionImmutable
}

func (action *PromotionRefundAction) BeforeDelete(_ *gorm.DB) error {
	return ErrPromotionRefundActionImmutable
}

func (receipt *PromotionRefundRecoveryReceipt) BeforeCreate(_ *gorm.DB) error {
	if receipt.CreatedAt == 0 {
		receipt.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func (receipt *PromotionRefundRecoveryReceipt) BeforeUpdate(_ *gorm.DB) error {
	return ErrPromotionRefundActionImmutable
}

func (receipt *PromotionRefundRecoveryReceipt) BeforeDelete(_ *gorm.DB) error {
	return ErrPromotionRefundActionImmutable
}

func (obligation *PromotionRefundObligation) OutstandingAmount() int64 {
	if obligation == nil {
		return 0
	}
	outstanding := obligation.Amount - obligation.RecoveredAmount - obligation.WaivedAmount
	if outstanding < 0 {
		return 0
	}
	return outstanding
}

func CreatePromotionRefundObligationTx(tx *gorm.DB, obligation *PromotionRefundObligation) error {
	if tx == nil || obligation == nil {
		return errors.New("refund obligation and transaction are required")
	}
	obligation.ObligationKey = strings.TrimSpace(obligation.ObligationKey)
	obligation.Currency = strings.ToUpper(strings.TrimSpace(obligation.Currency))
	if obligation.ObligationKey == "" || len(obligation.ObligationKey) > 128 || obligation.RefundCaseId <= 0 || obligation.UserId <= 0 || obligation.Amount <= 0 {
		return errors.New("invalid refund obligation")
	}
	if obligation.RecoveredAmount != 0 || obligation.WaivedAmount != 0 ||
		(obligation.Status != "" && obligation.Status != PromotionRefundObligationStatusOpen) {
		return errors.New("new refund obligation must have an open zero-recovery balance")
	}
	if obligation.Account != PromotionFundAccountRefundDebt {
		return errors.New("refund obligation must use refund debt account")
	}
	if obligation.Asset == PromotionFundAssetQuota {
		if obligation.Currency != "" {
			return errors.New("quota refund obligation cannot have currency")
		}
	} else if obligation.Asset == PromotionFundAssetCash {
		if !isISOCurrencyCode(obligation.Currency) {
			return errors.New("cash refund obligation requires currency")
		}
	} else {
		return errors.New("invalid refund obligation asset")
	}

	created := *obligation
	created.Id = 0
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "obligation_key"}},
		DoNothing: true,
	}).Create(&created)
	if result.Error != nil {
		return result.Error
	}
	var persisted PromotionRefundObligation
	if err := lockForUpdate(tx).Where("obligation_key = ?", obligation.ObligationKey).First(&persisted).Error; err != nil {
		return err
	}
	if persisted.RefundCaseId != obligation.RefundCaseId || persisted.UserId != obligation.UserId ||
		persisted.Account != obligation.Account || persisted.Asset != obligation.Asset ||
		persisted.Currency != obligation.Currency || persisted.Amount != obligation.Amount ||
		persisted.SourceType != obligation.SourceType || persisted.SourceId != obligation.SourceId {
		return ErrPromotionRefundObligationConflict
	}
	if err := recordPromotionRefundCaseUsersTx(tx, persisted.RefundCaseId, persisted.UserId); err != nil {
		return err
	}
	*obligation = persisted
	return nil
}

// promotionRefundActionUserIdsTx gathers the administrator and every durable
// user currently tied to the reviewed case. Snapshot actions pass the complete
// set into responsibility binding so downstream rows are locked before all
// user rows in one stable order.
func promotionRefundActionUserIdsTx(tx *gorm.DB, refundCase *PromotionRefundCase, input PromotionRefundRecoveryActionInput) ([]int, error) {
	if tx == nil || refundCase == nil || refundCase.Id <= 0 || input.ActorId <= 0 {
		return nil, errors.New("refund action users and transaction are required")
	}
	userIds := []int{input.ActorId}
	for _, userId := range []int{refundCase.UserId, input.UserId} {
		if userId > 0 {
			userIds = append(userIds, userId)
		}
	}

	var obligationUserIds []int
	if err := tx.Model(&PromotionRefundObligation{}).
		Where("refund_case_id = ?", refundCase.Id).
		Pluck("user_id", &obligationUserIds).Error; err != nil {
		return nil, err
	}
	userIds = append(userIds, obligationUserIds...)
	var caseUserIds []int
	if err := tx.Model(&PromotionRefundCaseUser{}).
		Where("refund_case_id = ?", refundCase.Id).
		Pluck("user_id", &caseUserIds).Error; err != nil {
		return nil, err
	}
	userIds = append(userIds, caseUserIds...)

	if refundCase.TopUpId > 0 {
		var topUpUserIds []int
		if err := tx.Model(&TopUp{}).Where("id = ?", refundCase.TopUpId).
			Pluck("user_id", &topUpUserIds).Error; err != nil {
			return nil, err
		}
		userIds = append(userIds, topUpUserIds...)

		var rewardUserIds []int
		if err := tx.Model(&InvitationReward{}).
			Where("trigger_top_up_id = ?", refundCase.TopUpId).
			Pluck("inviter_id", &rewardUserIds).Error; err != nil {
			return nil, err
		}
		userIds = append(userIds, rewardUserIds...)
	}
	if refundCase.InvitationRebateId > 0 {
		var rebateUserIds []int
		if err := tx.Model(&InvitationRebate{}).Where("id = ?", refundCase.InvitationRebateId).
			Pluck("inviter_id", &rebateUserIds).Error; err != nil {
			return nil, err
		}
		userIds = append(userIds, rebateUserIds...)
	}
	commissionLedgerId := refundCase.CommissionLedgerId
	if input.ExpectedCommissionLedgerId > 0 {
		commissionLedgerId = input.ExpectedCommissionLedgerId
	}
	if commissionLedgerId > 0 {
		var commissionUserIds []int
		if err := tx.Model(&PromotionCommissionLedger{}).Where("id = ?", commissionLedgerId).
			Pluck("user_id", &commissionUserIds).Error; err != nil {
			return nil, err
		}
		userIds = append(userIds, commissionUserIds...)
	}
	if input.UserSubscriptionId > 0 {
		var subscriptionUserIds []int
		if err := tx.Model(&UserSubscription{}).Where("id = ?", input.UserSubscriptionId).
			Pluck("user_id", &subscriptionUserIds).Error; err != nil {
			return nil, err
		}
		userIds = append(userIds, subscriptionUserIds...)
	}

	return userIds, nil
}

// lockPromotionRefundActionUsersTx validates the actor after the caller has
// acquired every downstream responsibility row. Soft-deleted debt owners stay
// recoverable, while the administrator creating new evidence must be active.
func lockPromotionRefundActionUsersTx(tx *gorm.DB, input PromotionRefundRecoveryActionInput, userIds ...int) error {
	lockedUsers, err := lockUsersForFinancialWriteTx(tx, userIds...)
	if err != nil {
		return err
	}
	if lockedUsers[input.ActorId].DeletedAt.Valid {
		return gorm.ErrRecordNotFound
	}
	if lockedUsers[input.ActorId].Role != input.ActorRole {
		return errors.New("refund recovery actor role changed")
	}
	return nil
}

// ApplyPromotionRefundRecoveryAction applies one explicit administrator
// recovery decision. The obligation, user balances, immutable fund journal,
// action history, and case state are committed in one database transaction.
func ApplyPromotionRefundRecoveryAction(input PromotionRefundRecoveryActionInput) (*PromotionRefundCase, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Action = strings.TrimSpace(input.Action)
	input.Asset = strings.ToLower(strings.TrimSpace(input.Asset))
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.ExternalRef = strings.TrimSpace(input.ExternalRef)
	input.Remark = strings.TrimSpace(input.Remark)
	input.ExpectedResponsibilityFingerprint = strings.TrimSpace(input.ExpectedResponsibilityFingerprint)
	if input.RefundCaseId <= 0 || input.ActorId <= 0 {
		return nil, errors.New("invalid refund recovery action")
	}
	if input.ActorRole != common.RoleAdminUser && input.ActorRole != common.RoleRootUser {
		return nil, errors.New("refund recovery action requires an administrator")
	}
	if input.IdempotencyKey == "" || utf8.RuneCountInString(input.IdempotencyKey) > maxPromotionRefundIdempotencyKeyLength {
		return nil, fmt.Errorf("idempotency key must contain 1 to %d characters", maxPromotionRefundIdempotencyKeyLength)
	}
	if utf8.RuneCountInString(input.ExternalRef) > maxPromotionRefundExternalRefLength {
		return nil, fmt.Errorf("external reference cannot exceed %d characters", maxPromotionRefundExternalRefLength)
	}
	if utf8.RuneCountInString(input.Remark) > maxPromotionRefundActionRemarkLength {
		return nil, fmt.Errorf("remark cannot exceed %d characters", maxPromotionRefundActionRemarkLength)
	}

	switch input.Action {
	case PromotionRefundActionRetryWalletDebit:
		if input.ObligationId <= 0 || input.Amount <= 0 {
			return nil, errors.New("wallet recovery requires an obligation and a positive amount")
		}
	case PromotionRefundActionRecordExternalRepayment, PromotionRefundActionRecoverPaidCommission:
		if input.ObligationId <= 0 || input.Amount <= 0 {
			return nil, errors.New("external recovery requires an obligation and a positive amount")
		}
		if input.ExternalRef == "" {
			return nil, errors.New("external reference is required")
		}
	case PromotionRefundActionDefineManualObligation:
		if input.ActorRole != common.RoleRootUser {
			return nil, errors.New("only root can define a manual refund obligation")
		}
		if input.ObligationId != 0 || input.UserId <= 0 || input.TopUpId < 0 || input.Amount <= 0 {
			return nil, errors.New("manual obligation requires a user and a positive amount")
		}
		if input.Asset == PromotionFundAssetQuota {
			if input.Currency != "" || input.Amount > int64(common.MaxQuota) {
				return nil, errors.New("manual quota obligation has an invalid amount or currency")
			}
		} else if input.Asset == PromotionFundAssetCash {
			if !isISOCurrencyCode(input.Currency) {
				return nil, errors.New("manual cash obligation requires a currency")
			}
		} else {
			return nil, errors.New("manual obligation asset must be quota or cash")
		}
		if input.ExternalRef == "" || input.Remark == "" {
			return nil, errors.New("manual obligation evidence reference and remark are required")
		}
	case PromotionRefundActionQuarantineUnknownCommission:
		if input.ActorRole != common.RoleRootUser {
			return nil, errors.New("only root can quarantine an unknown commission ledger")
		}
		if input.ObligationId != 0 || input.Amount != 0 {
			return nil, errors.New("commission ledger quarantine cannot specify an obligation or amount")
		}
		if input.ExpectedCommissionLedgerId <= 0 || input.ExpectedCommissionLedgerStatus == nil {
			return nil, errors.New("commission ledger quarantine requires the reviewed ledger snapshot")
		}
		if input.ExternalRef == "" || input.Remark == "" {
			return nil, errors.New("commission ledger quarantine evidence reference and remark are required")
		}
	case PromotionRefundActionRevokeSubscription:
		if input.ActorRole != common.RoleRootUser {
			return nil, errors.New("only root can dispose of a refunded subscription entitlement")
		}
		if input.ObligationId != 0 || input.Amount != 0 || input.UserSubscriptionId <= 0 {
			return nil, errors.New("subscription disposition requires an entitlement without an obligation or amount")
		}
		if input.Remark == "" {
			return nil, errors.New("subscription disposition remark is required")
		}
	case PromotionRefundActionWaive:
		if input.ActorRole != common.RoleRootUser {
			return nil, errors.New("only root can waive refund recovery")
		}
		caseLevelWaiver := input.ObligationId == 0 && input.Amount == 0
		obligationWaiver := input.ObligationId > 0 && input.Amount > 0
		if !caseLevelWaiver && !obligationWaiver {
			return nil, errors.New("waiver requires either a positive obligation amount or a zero-value root review waiver")
		}
		if input.Remark == "" {
			return nil, errors.New("waiver remark is required")
		}
	case PromotionRefundActionReleaseHold:
		if input.ObligationId != 0 || input.Amount != 0 {
			return nil, errors.New("hold release cannot specify an obligation or amount")
		}
		if input.Remark == "" {
			return nil, errors.New("hold release remark is required")
		}
	default:
		return nil, errors.New("invalid refund recovery action")
	}
	if input.Action != PromotionRefundActionDefineManualObligation &&
		(input.UserId != 0 || input.TopUpId != 0 || input.Asset != "" || input.Currency != "") {
		return nil, errors.New("user, top-up, asset, and currency are only valid for a manual obligation")
	}
	if input.Action != PromotionRefundActionQuarantineUnknownCommission &&
		(input.ExpectedCommissionLedgerId != 0 || input.ExpectedCommissionLedgerStatus != nil) {
		return nil, errors.New("commission ledger snapshot is only valid for a quarantine action")
	}
	if input.Action != PromotionRefundActionRevokeSubscription && input.UserSubscriptionId != 0 {
		return nil, errors.New("subscription ID is only valid for a subscription disposition action")
	}
	requiresResponsibilitySnapshot := input.Action == PromotionRefundActionWaive || input.Action == PromotionRefundActionReleaseHold ||
		input.Action == PromotionRefundActionQuarantineUnknownCommission ||
		input.Action == PromotionRefundActionDefineManualObligation ||
		input.Action == PromotionRefundActionRevokeSubscription
	if requiresResponsibilitySnapshot && len(input.ExpectedResponsibilityFingerprint) != 40 {
		return nil, errors.New("refund action requires the reviewed responsibility fingerprint")
	}

	actionKey := fmt.Sprintf("refund_action:%d:%s", input.RefundCaseId, common.Sha1([]byte(input.IdempotencyKey)))
	topUpIdToLock := 0
	if input.Action == PromotionRefundActionDefineManualObligation {
		topUpIdToLock = input.TopUpId
	}
	if requiresResponsibilitySnapshot {
		var existingAction PromotionRefundAction
		existingErr := DB.Select("id").Where("action_key = ?", actionKey).First(&existingAction).Error
		if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return nil, existingErr
		}
		if errors.Is(existingErr, gorm.ErrRecordNotFound) {
			changed, err := ReconcilePromotionRefundCaseResponsibility(input.RefundCaseId)
			if err != nil {
				return nil, err
			}
			if changed {
				return nil, ErrPromotionRefundResponsibilityChanged
			}
			var responsibilitySnapshot PromotionRefundCase
			if err := DB.Select("top_up_id", "responsibility_fingerprint").Where("id = ?", input.RefundCaseId).
				First(&responsibilitySnapshot).Error; err != nil {
				return nil, err
			}
			if responsibilitySnapshot.ResponsibilityFingerprint != input.ExpectedResponsibilityFingerprint {
				return nil, ErrPromotionRefundResponsibilityChanged
			}
			topUpIdToLock = responsibilitySnapshot.TopUpId
		}
	}
	fencedUserIds := newRefundHoldFenceScope()
	if input.Action == PromotionRefundActionRetryWalletDebit {
		var obligation PromotionRefundObligation
		err := DB.Select("user_id").
			Where("id = ? AND refund_case_id = ?", input.ObligationId, input.RefundCaseId).
			First(&obligation).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			if err := fencedUserIds.Ensure(obligation.UserId); err != nil {
				return nil, err
			}
		}
	}
	if input.Action == PromotionRefundActionDefineManualObligation {
		var userExists int64
		if err := DB.Unscoped().Model(&User{}).Where("id = ?", input.UserId).Count(&userExists).Error; err != nil {
			return nil, err
		}
		if userExists != 1 {
			return nil, errors.New("manual obligation user not found")
		}
		if err := fencedUserIds.Ensure(input.UserId); err != nil {
			return nil, err
		}
	}
	refundCase := &PromotionRefundCase{}
	subscriptionGroupChanged := false
	subscriptionUserId := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		if topUpIdToLock > 0 {
			var lockedTopUp TopUp
			if err := lockForUpdate(tx).Select("id").Where("id = ?", topUpIdToLock).First(&lockedTopUp).Error; err != nil {
				return err
			}
		}
		if err := lockForUpdate(tx).Where("id = ?", input.RefundCaseId).First(refundCase).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("refund case not found")
			}
			return err
		}

		var existing PromotionRefundAction
		err := lockForUpdate(tx).Where("action_key = ?", actionKey).First(&existing).Error
		if err == nil {
			matches := existing.RefundCaseId == input.RefundCaseId && existing.Action == input.Action &&
				existing.Amount == input.Amount && existing.ActorId == input.ActorId && existing.ActorRole == input.ActorRole &&
				canonicalPromotionRefundExternalReference(existing.ExternalRef) == canonicalPromotionRefundExternalReference(input.ExternalRef) &&
				existing.Remark == input.Remark
			if input.Action == PromotionRefundActionDefineManualObligation {
				matches = matches && existing.ObligationId > 0 && existing.UserId == input.UserId &&
					existing.TopUpId == input.TopUpId && existing.Asset == input.Asset && existing.Currency == input.Currency &&
					existing.ResponsibilityFingerprint == input.ExpectedResponsibilityFingerprint
			} else if input.Action == PromotionRefundActionQuarantineUnknownCommission {
				matches = matches && existing.ObligationId == 0 &&
					existing.CommissionLedgerId == input.ExpectedCommissionLedgerId &&
					existing.CommissionLedgerStatus == *input.ExpectedCommissionLedgerStatus &&
					existing.ResponsibilityFingerprint == input.ExpectedResponsibilityFingerprint
			} else if input.Action == PromotionRefundActionRevokeSubscription {
				matches = matches && existing.ObligationId == 0 &&
					existing.UserSubscriptionId != nil &&
					*existing.UserSubscriptionId == input.UserSubscriptionId &&
					existing.ResponsibilityFingerprint == input.ExpectedResponsibilityFingerprint
			} else if requiresResponsibilitySnapshot {
				matches = matches && existing.ObligationId == input.ObligationId &&
					existing.ResponsibilityFingerprint == input.ExpectedResponsibilityFingerprint
			} else {
				matches = matches && existing.ObligationId == input.ObligationId
			}
			if !matches {
				return errors.New("idempotency key was already used for a different refund recovery action")
			}
			return loadPromotionRefundRecoveryTx(tx, refundCase)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if refundCase.Status != PromotionRefundCaseStatusPendingReview {
			return errors.New("refund case is not pending recovery")
		}
		actionUserIds, err := promotionRefundActionUserIdsTx(tx, refundCase, input)
		if err != nil {
			return err
		}
		var responsibilityTopUp *TopUp
		if requiresResponsibilitySnapshot {
			var responsibilityChanged bool
			responsibilityTopUp, responsibilityChanged, err = bindPromotionRefundResponsibilityTx(
				tx, refundCase, fencedUserIds, actionUserIds...,
			)
			if err != nil {
				return err
			}
			if responsibilityChanged || refundCase.ResponsibilityFingerprint != input.ExpectedResponsibilityFingerprint {
				return ErrPromotionRefundResponsibilityChanged
			}
		}
		if err := lockPromotionRefundActionUsersTx(tx, input, actionUserIds...); err != nil {
			return err
		}

		action := &PromotionRefundAction{
			ActionKey: actionKey, RefundCaseId: refundCase.Id, ObligationId: input.ObligationId,
			Action: input.Action, Amount: input.Amount, ActorId: input.ActorId, ActorRole: input.ActorRole,
			ExternalRef: input.ExternalRef, Remark: input.Remark,
			ResponsibilityFingerprint: input.ExpectedResponsibilityFingerprint,
		}
		if input.Action == PromotionRefundActionDefineManualObligation {
			return defineManualPromotionRefundObligationTx(tx, refundCase, action, actionKey, input, fencedUserIds)
		}
		if input.Action == PromotionRefundActionQuarantineUnknownCommission {
			if refundCase.CommissionLedgerId <= 0 {
				return errors.New("refund case has no verifiable linked commission ledger")
			}
			if refundCase.CommissionLedgerId != input.ExpectedCommissionLedgerId {
				return errors.New("commission ledger changed since it was reviewed; reload the refund case")
			}
			var ledger PromotionCommissionLedger
			if err := lockForUpdate(tx).Where("id = ?", refundCase.CommissionLedgerId).First(&ledger).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("linked commission ledger not found")
				}
				return err
			}
			if ledger.Status != *input.ExpectedCommissionLedgerStatus {
				return errors.New("commission ledger changed since it was reviewed; reload the refund case")
			}
			requiresQuarantine, err := promotionCommissionReconciliationQuarantineRequired(&ledger)
			if err != nil {
				return err
			}
			if !requiresQuarantine {
				return errors.New("linked commission ledger does not require reconciliation quarantine")
			}
			action.UserId = ledger.UserId
			action.CommissionLedgerId = ledger.Id
			action.CommissionLedgerStatus = ledger.Status
			if err := tx.Create(action).Error; err != nil {
				return err
			}
			if !refundCase.RequiresRootReview {
				result := tx.Model(&PromotionRefundCase{}).
					Where("id = ? AND status = ? AND requires_root_review = ?", refundCase.Id, PromotionRefundCaseStatusPendingReview, false).
					Update("requires_root_review", true)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return errors.New("refund case root-review state changed during commission quarantine")
				}
				refundCase.RequiresRootReview = true
			}
			return loadPromotionRefundRecoveryTx(tx, refundCase)
		}
		if input.Action == PromotionRefundActionRevokeSubscription {
			topUp := responsibilityTopUp

			var existingDisposition PromotionRefundAction
			err = lockForUpdate(tx).Select("id").
				Where("refund_case_id = ? AND user_subscription_id = ?", refundCase.Id, input.UserSubscriptionId).
				First(&existingDisposition).Error
			if err == nil {
				return errors.New("subscription entitlement disposition was already recorded")
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			subscription, endTimeBefore, groupChanged, err := revokePromotionRefundSubscriptionEntitlementTx(
				tx, refundCase, topUp, input.UserSubscriptionId,
			)
			if err != nil {
				return err
			}
			subscriptionId := subscription.Id
			action.UserId = subscription.UserId
			action.UserSubscriptionId = &subscriptionId
			action.SubscriptionEndTimeBefore = endTimeBefore
			if err := tx.Create(action).Error; err != nil {
				return err
			}
			if _, err := bindPromotionRefundCaseToTopUpTx(tx, refundCase, topUp, fencedUserIds, false); err != nil {
				return err
			}
			subscriptionGroupChanged = groupChanged
			subscriptionUserId = subscription.UserId
			return loadPromotionRefundRecoveryTx(tx, refundCase)
		}
		if input.Action == PromotionRefundActionWaive && input.ObligationId == 0 {
			topUp := responsibilityTopUp
			if !refundCase.RequiresRootReview {
				return errors.New("refund case does not require root review")
			}
			if err := validatePromotionRefundPrincipalAssessmentTx(tx, refundCase, topUp); err != nil {
				return err
			}
			if err := validatePromotionRefundSubscriptionDispositionTx(tx, refundCase, topUp); err != nil {
				return err
			}
			var reviewLedger *PromotionCommissionLedger
			if refundCase.CommissionLedgerId > 0 {
				var ledger PromotionCommissionLedger
				if err := lockForUpdate(tx).Where("id = ?", refundCase.CommissionLedgerId).First(&ledger).Error; err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
				} else {
					reviewLedger = &ledger
				}
			} else if refundCase.TradeNo != "" && refundCase.Provider != "" {
				var topUps []TopUp
				if err := lockForUpdate(tx).
					Where("trade_no = ? AND payment_provider = ?", refundCase.TradeNo, refundCase.Provider).
					Order("id ASC").Limit(2).Find(&topUps).Error; err != nil {
					return err
				}
				if len(topUps) > 1 {
					return errors.New("refund case has multiple matching top-ups")
				}
				if len(topUps) == 1 && topUps[0].UserId > 0 &&
					(refundCase.UserId == 0 || refundCase.UserId == topUps[0].UserId) {
					_, ledger, err := loadPromotionRefundLinkedCommissionTx(tx, refundCase, &topUps[0])
					if err != nil {
						return err
					}
					reviewLedger = ledger
				}
			}
			if reviewLedger != nil {
				requiresQuarantine, err := promotionCommissionReconciliationQuarantineRequired(reviewLedger)
				if err != nil {
					return err
				}
				if requiresQuarantine {
					quarantined, err := isPromotionCommissionReconciliationQuarantinedTx(tx, reviewLedger)
					if err != nil {
						return err
					}
					if !quarantined {
						return errors.New("unknown commission ledger must be quarantined before completing root review")
					}
				}
			}
			action.UserId = refundCase.UserId
			if err := tx.Create(action).Error; err != nil {
				return err
			}
			result := tx.Model(&PromotionRefundCase{}).
				Where("id = ? AND status = ? AND requires_root_review = ?", refundCase.Id, PromotionRefundCaseStatusPendingReview, true).
				Update("requires_root_review", false)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("refund case root-review state changed during waiver")
			}
			refundCase.RequiresRootReview = false
			return loadPromotionRefundRecoveryTx(tx, refundCase)
		}
		if input.Action == PromotionRefundActionReleaseHold {
			topUp := responsibilityTopUp
			if refundCase.RequiresRootReview {
				return errors.New("refund case requires a root review waiver before hold release")
			}
			switch topUp.Status {
			case common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired:
			case common.TopUpStatusPending:
				return errors.New("pending top-up cannot be released from refund recovery")
			default:
				return errors.New("top-up has an invalid status for refund recovery release")
			}
			if err := validatePromotionRefundPrincipalAssessmentTx(tx, refundCase, topUp); err != nil {
				return err
			}
			if err := validatePromotionRefundSubscriptionDispositionTx(tx, refundCase, topUp); err != nil {
				return err
			}
			userId, err := releasePromotionRefundHoldTx(tx, refundCase, fencedUserIds)
			if err != nil {
				return err
			}
			action.UserId = userId
			if err := tx.Create(action).Error; err != nil {
				return err
			}
			now := common.GetTimestamp()
			result := tx.Model(&PromotionRefundCase{}).
				Where("id = ? AND status = ?", refundCase.Id, PromotionRefundCaseStatusPendingReview).
				Updates(map[string]interface{}{
					"status": PromotionRefundCaseStatusResolved, "reviewer_id": input.ActorId,
					"review_note": input.Remark, "resolved_at": now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("refund case status changed during hold release")
			}
			refundCase.Status = PromotionRefundCaseStatusResolved
			refundCase.ReviewerId = input.ActorId
			refundCase.ReviewNote = input.Remark
			refundCase.ResolvedAt = now
			return loadPromotionRefundRecoveryTx(tx, refundCase)
		}

		var obligation PromotionRefundObligation
		if err := lockForUpdate(tx).
			Where("id = ? AND refund_case_id = ?", input.ObligationId, refundCase.Id).
			First(&obligation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("refund obligation not found")
			}
			return err
		}
		if obligation.Status != PromotionRefundObligationStatusOpen {
			return errors.New("refund obligation is already closed")
		}
		if obligation.Amount <= 0 || obligation.RecoveredAmount < 0 || obligation.WaivedAmount < 0 ||
			obligation.RecoveredAmount > obligation.Amount || obligation.WaivedAmount > obligation.Amount-obligation.RecoveredAmount {
			return errors.New("refund obligation balance is inconsistent")
		}
		if input.Amount > obligation.Amount-obligation.RecoveredAmount-obligation.WaivedAmount {
			return errors.New("recovery amount exceeds the outstanding obligation")
		}
		switch input.Action {
		case PromotionRefundActionRetryWalletDebit:
			if obligation.Asset != PromotionFundAssetQuota {
				return errors.New("this action requires a quota obligation")
			}
		case PromotionRefundActionRecordExternalRepayment:
			if obligation.Asset != PromotionFundAssetQuota && obligation.Asset != PromotionFundAssetCash {
				return errors.New("external repayment requires a quota or cash obligation")
			}
			if obligation.Asset == PromotionFundAssetCash && obligation.SourceType == "promotion_commission_ledgers" {
				return errors.New("this action requires a quota obligation or a non-commission cash obligation")
			}
		case PromotionRefundActionRecoverPaidCommission:
			if obligation.Asset != PromotionFundAssetCash || obligation.SourceType != "promotion_commission_ledgers" {
				return errors.New("paid commission recovery requires a cash commission obligation")
			}
		}
		if input.Action == PromotionRefundActionRecordExternalRepayment || input.Action == PromotionRefundActionRecoverPaidCommission {
			if err := claimPromotionRefundRecoveryReceiptTx(tx, PromotionRefundReceiptPurposeRepayment, actionKey, refundCase.Id, obligation.Id, input.ExternalRef); err != nil {
				return err
			}
		}

		if err := fencedUserIds.Ensure(obligation.UserId); err != nil {
			return err
		}
		var user User
		if err := lockForUpdate(tx.Unscoped()).Where("id = ?", obligation.UserId).First(&user).Error; err != nil {
			return err
		}
		if !user.RefundHold {
			return errors.New("refund recovery user is not on hold")
		}

		transaction, err := applyPromotionRefundObligationActionTx(tx, refundCase, &obligation, &user, actionKey, input)
		if err != nil {
			return err
		}
		action.UserId = obligation.UserId
		action.Asset = obligation.Asset
		action.Currency = obligation.Currency
		action.FundTransactionId = transaction.Id
		if err := tx.Create(action).Error; err != nil {
			return err
		}
		return loadPromotionRefundRecoveryTx(tx, refundCase)
	})

	if err == nil && subscriptionGroupChanged && subscriptionUserId > 0 {
		refreshSubscriptionUserGroupCache(subscriptionUserId, "subscription refund disposition")
	}
	reconcileErr := reconcilePromotionRefundHoldFences(fencedUserIds)
	if err != nil {
		return nil, errors.Join(err, reconcileErr)
	}
	if reconcileErr != nil {
		return nil, reconcileErr
	}
	return refundCase, nil
}

func claimPromotionRefundRecoveryReceiptTx(tx *gorm.DB, purpose string, actionKey string, refundCaseId int, obligationId int, externalRef string) error {
	if purpose != PromotionRefundReceiptPurposeRepayment && purpose != PromotionRefundReceiptPurposeManualEvidence {
		return errors.New("invalid refund recovery receipt purpose")
	}
	externalRef = strings.TrimSpace(externalRef)
	canonicalReference := canonicalPromotionRefundExternalReference(externalRef)
	if canonicalReference == "" {
		return errors.New("external reference is required")
	}
	receiptKey := common.Sha1([]byte(canonicalReference))
	var historicalClaims []PromotionRefundRecoveryReceipt
	if err := lockForUpdate(tx).
		Where("receipt_key = ? OR UPPER(TRIM(external_ref)) = ?", receiptKey, canonicalReference).
		Order("id ASC").Find(&historicalClaims).Error; err != nil {
		return err
	}
	if len(historicalClaims) > 0 {
		if len(historicalClaims) != 1 {
			return errors.New("external evidence reference has conflicting historical claims")
		}
		existing := historicalClaims[0]
		if existing.Purpose != purpose || existing.ActionKey != actionKey || existing.ObligationId != obligationId ||
			canonicalPromotionRefundExternalReference(existing.ExternalRef) != canonicalReference {
			return errors.New("external evidence reference was already used")
		}
		return nil
	}
	receipt := &PromotionRefundRecoveryReceipt{
		// A provider or bank reference identifies one piece of evidence globally.
		// Do not let the same reference prove both an obligation and a repayment.
		ReceiptKey: receiptKey, Purpose: purpose,
		ExternalRef: externalRef, ActionKey: actionKey,
		RefundCaseId: refundCaseId, ObligationId: obligationId,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "receipt_key"}},
		DoNothing: true,
	}).Create(receipt)
	if result.Error != nil {
		return result.Error
	}
	var existing PromotionRefundRecoveryReceipt
	if err := lockForUpdate(tx).Where("receipt_key = ?", receipt.ReceiptKey).First(&existing).Error; err != nil {
		return err
	}
	if existing.Purpose != purpose || existing.ActionKey != actionKey || existing.ObligationId != obligationId ||
		canonicalPromotionRefundExternalReference(existing.ExternalRef) != canonicalReference {
		return errors.New("external evidence reference was already used")
	}
	return nil
}

func canonicalPromotionRefundExternalReference(externalRef string) string {
	return strings.ToUpper(strings.TrimSpace(externalRef))
}

// loadPromotionRefundLinkedCommissionTx follows the immutable top-up rebate
// relationship instead of trusting an administrator-supplied user id. The
// returned ledger owner is therefore a provable recovery party for this
// payment; missing rebate/ledger rows simply mean that no commission party can
// be established from local evidence.
func loadPromotionRefundLinkedCommissionTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp) (*InvitationRebate, *PromotionCommissionLedger, error) {
	if tx == nil || topUp == nil || topUp.Id <= 0 || topUp.UserId <= 0 {
		return nil, nil, nil
	}

	var rebate InvitationRebate
	err := lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).First(&rebate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if refundCase != nil && refundCase.InvitationRebateId > 0 {
			return nil, nil, errors.New("persisted invitation rebate link is missing")
		}
		rebate = InvitationRebate{}
		err = nil
	}
	if err != nil {
		return nil, nil, err
	}
	if rebate.Id > 0 {
		if rebate.TopUpId != topUp.Id || rebate.InviteeId != topUp.UserId || rebate.InviterId <= 0 ||
			(rebate.TradeNo != "" && rebate.TradeNo != topUp.TradeNo) ||
			(refundCase != nil && refundCase.InvitationRebateId > 0 && refundCase.InvitationRebateId != rebate.Id) {
			return nil, nil, errors.New("linked invitation rebate is inconsistent with the refunded top-up")
		}
	}

	var ledger PromotionCommissionLedger
	if rebate.Id > 0 {
		err = lockForUpdate(tx).
			Where("source_type = ? AND source_id = ?", PromotionCommissionSourceTopUpRebate, rebate.Id).
			First(&ledger).Error
	}
	if rebate.Id == 0 || errors.Is(err, gorm.ErrRecordNotFound) {
		if refundCase == nil || refundCase.CommissionLedgerId <= 0 {
			if rebate.Id == 0 {
				return nil, nil, nil
			}
			return &rebate, nil, nil
		}
		err = lockForUpdate(tx).Where("id = ?", refundCase.CommissionLedgerId).First(&ledger).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("persisted commission ledger link is missing")
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if ledger.UserId <= 0 ||
		(rebate.Id > 0 && (ledger.UserId != rebate.InviterId || ledger.SourceType != PromotionCommissionSourceTopUpRebate || ledger.SourceId != rebate.Id)) ||
		(rebate.Id == 0 && (ledger.InviteeId != topUp.UserId || ledger.SourceTradeNo == "" || ledger.SourceTradeNo != topUp.TradeNo)) ||
		(ledger.InviteeId > 0 && ledger.InviteeId != topUp.UserId) ||
		(ledger.SourceTradeNo != "" && ledger.SourceTradeNo != topUp.TradeNo) ||
		(refundCase != nil && refundCase.CommissionLedgerId > 0 && refundCase.CommissionLedgerId != ledger.Id) {
		return nil, nil, errors.New("linked commission ledger is inconsistent with the refunded top-up")
	}
	if rebate.Id == 0 {
		return nil, &ledger, nil
	}
	return &rebate, &ledger, nil
}

type promotionRefundManualResponsibility struct {
	topUp            *TopUp
	rebate           *InvitationRebate
	commissionLedger *PromotionCommissionLedger
	invitationReward *InvitationReward
	sourceType       string
	sourceId         int
	isTopUpPrincipal bool
}

func resolvePromotionRefundManualResponsibilityTx(tx *gorm.DB, refundCase *PromotionRefundCase, userId int, topUpId int) (*promotionRefundManualResponsibility, error) {
	if tx == nil || refundCase == nil || userId <= 0 || topUpId < 0 {
		return nil, errors.New("invalid manual refund responsibility")
	}
	if refundCase.TopUpId > 0 && refundCase.TopUpId != topUpId {
		return nil, errors.New("manual obligation top-up does not match the refund case")
	}

	var topUp *TopUp
	if topUpId > 0 {
		var stored TopUp
		if err := lockForUpdate(tx).Where("id = ?", topUpId).First(&stored).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New("manual obligation top-up not found")
			}
			return nil, err
		}
		if stored.TradeNo == "" || stored.TradeNo != refundCase.TradeNo ||
			(refundCase.Provider != "" && stored.PaymentProvider != "" && stored.PaymentProvider != refundCase.Provider) {
			return nil, errors.New("manual obligation top-up does not match the refunded payment")
		}
		topUp = &stored
	}

	responsibility := &promotionRefundManualResponsibility{
		topUp: topUp, sourceType: "promotion_refund_cases", sourceId: refundCase.Id,
	}
	if topUp == nil {
		if refundCase.UserId != userId {
			return nil, errors.New("manual obligation user is not responsible for the refund case")
		}
		return responsibility, nil
	}
	if topUp.UserId == userId {
		responsibility.sourceType = "top_ups"
		responsibility.sourceId = topUp.Id
		responsibility.isTopUpPrincipal = true
		return responsibility, nil
	}

	rebate, ledger, err := loadPromotionRefundLinkedCommissionTx(tx, refundCase, topUp)
	if err != nil {
		return nil, err
	}
	if ledger != nil && ledger.UserId == userId {
		responsibility.commissionLedger = ledger
		responsibility.sourceType = "promotion_commission_ledgers"
		responsibility.sourceId = ledger.Id
		return responsibility, nil
	}
	var invitationReward InvitationReward
	err = lockForUpdate(tx).
		Where("trigger_top_up_id = ? AND inviter_id = ? AND reward_type = ? AND status = ?",
			topUp.Id, userId, InvitationRewardTypeFirstTopUp, InvitationRewardStatusSettled).
		First(&invitationReward).Error
	if err == nil {
		responsibility.invitationReward = &invitationReward
		responsibility.sourceType = "invitation_rewards"
		responsibility.sourceId = invitationReward.Id
		return responsibility, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if rebate != nil && rebate.InviterId == userId {
		responsibility.rebate = rebate
		responsibility.sourceType = "invitation_rebates"
		responsibility.sourceId = rebate.Id
		return responsibility, nil
	}
	return nil, errors.New("manual obligation user is not responsible for the refund case")
}

func defineManualPromotionRefundObligationTx(tx *gorm.DB, refundCase *PromotionRefundCase, action *PromotionRefundAction,
	actionKey string, input PromotionRefundRecoveryActionInput, fencedUserIds refundHoldFenceScope,
) error {
	if !refundCase.RequiresRootReview {
		return errors.New("refund case does not require a manual obligation")
	}
	responsibility, err := resolvePromotionRefundManualResponsibilityTx(tx, refundCase, input.UserId, input.TopUpId)
	if err != nil {
		return err
	}
	effectiveTopUpId := input.TopUpId
	isLegacyAccountingCase := refundCase.AccountingMigrationVersion > 0
	maximumResponsibilityAmount := int64(0)
	maximumResponsibilityVerified := false
	if responsibility.commissionLedger != nil {
		maximumResponsibilityVerified = true
		ledger := responsibility.commissionLedger
		if input.Asset == PromotionFundAssetCash {
			if input.Currency != ledger.Currency {
				return errors.New("manual commission obligation currency does not match the commission ledger")
			}
			maximumAmount := ledger.NetAmountCents
			if ledger.ReversalAmountCents > 0 && ledger.ReversalAmountCents < maximumAmount {
				maximumAmount = ledger.ReversalAmountCents
			}
			if maximumAmount <= 0 || input.Amount > maximumAmount {
				return errors.New("manual commission obligation exceeds the linked commission amount")
			}
			maximumResponsibilityAmount = maximumAmount
		} else if ledger.QuotaEquivalent <= 0 || input.Amount > int64(ledger.QuotaEquivalent) {
			return errors.New("manual commission obligation exceeds the linked commission quota")
		} else {
			maximumResponsibilityAmount = int64(ledger.QuotaEquivalent)
		}
	} else if responsibility.invitationReward != nil {
		if input.Asset != PromotionFundAssetQuota || input.Currency != "" || responsibility.invitationReward.RewardQuota <= 0 {
			return errors.New("manual invitation reward obligation must use the linked quota amount")
		}
		maximumResponsibilityAmount = int64(responsibility.invitationReward.RewardQuota)
		maximumResponsibilityVerified = true
	} else if responsibility.rebate != nil {
		rebate := responsibility.rebate
		maximumResponsibilityVerified = true
		if input.Asset == PromotionFundAssetCash {
			if input.Currency != rebate.RebateCurrency || rebate.RebateAmountMinor <= 0 {
				return errors.New("manual rebate obligation currency or amount does not match the linked rebate")
			}
			maximumResponsibilityAmount = rebate.RebateAmountMinor
		} else if input.Asset == PromotionFundAssetQuota && input.Currency == "" && rebate.RebateQuota > 0 {
			maximumResponsibilityAmount = int64(rebate.RebateQuota)
		} else {
			return errors.New("manual rebate obligation must use a linked cash or quota amount")
		}
	} else if input.Asset == PromotionFundAssetCash && refundCase.Currency != "" && refundCase.Currency != input.Currency {
		return errors.New("manual obligation currency does not match the refund case")
	} else if responsibility.isTopUpPrincipal && responsibility.topUp != nil &&
		responsibility.topUp.Status == common.TopUpStatusSuccess && responsibility.topUp.PaidAmountVerified &&
		responsibility.topUp.PaidAmountMinor > 0 {
		topUp := responsibility.topUp
		refundedAmountMinor := refundCase.RefundedAmountMinor
		if refundCase.Kind == PromotionRefundKindFull || refundCase.Kind == PromotionRefundKindDispute {
			refundedAmountMinor = topUp.PaidAmountMinor
		}
		if refundedAmountMinor <= 0 || refundedAmountMinor > topUp.PaidAmountMinor {
			return errors.New("verified refund amount is outside the paid amount")
		}
		maximumResponsibilityVerified = true
		if input.Asset == PromotionFundAssetCash {
			if input.Currency != topUp.PaidCurrency {
				return errors.New("manual principal cash obligation currency does not match the paid currency")
			}
			maximumResponsibilityAmount = refundedAmountMinor
		} else if input.Asset == PromotionFundAssetQuota {
			if topUp.CreditedQuota <= 0 {
				return errors.New("verified top-up has no credited quota snapshot")
			}
			quotaAmount, err := common.QuotaFromDecimalStrict(
				decimal.NewFromInt(int64(topUp.CreditedQuota)).
					Mul(decimal.NewFromInt(refundedAmountMinor)).
					Div(decimal.NewFromInt(topUp.PaidAmountMinor)).Floor(),
			)
			if err != nil || quotaAmount <= 0 {
				return errors.New("verified refund quota amount is invalid")
			}
			maximumResponsibilityAmount = int64(quotaAmount)
		}
	} else if responsibility.isTopUpPrincipal && input.Asset == PromotionFundAssetQuota &&
		isLegacyAccountingCase && refundCase.QuotaAmount > 0 {
		maximumResponsibilityAmount = int64(refundCase.QuotaAmount)
		maximumResponsibilityVerified = true
	}
	if maximumResponsibilityVerified {
		if maximumResponsibilityAmount <= 0 || input.Amount > maximumResponsibilityAmount {
			return errors.New("manual obligation exceeds the verified responsibility amount")
		}
		var existingObligations []PromotionRefundObligation
		if err := tx.Where("refund_case_id = ? AND user_id = ? AND source_type = ? AND source_id = ? AND asset = ? AND currency = ?",
			refundCase.Id, input.UserId, responsibility.sourceType, responsibility.sourceId, input.Asset, input.Currency).
			Find(&existingObligations).Error; err != nil {
			return err
		}
		assessedAmount := int64(0)
		for i := range existingObligations {
			if existingObligations[i].Amount <= 0 || assessedAmount > math.MaxInt64-existingObligations[i].Amount {
				return errors.New("existing manual refund obligations have an invalid total")
			}
			assessedAmount += existingObligations[i].Amount
		}
		if assessedAmount > maximumResponsibilityAmount-input.Amount {
			return errors.New("manual obligation exceeds the verified responsibility amount")
		}

		if responsibility.isTopUpPrincipal && responsibility.topUp != nil {
			topUp := responsibility.topUp
			var relatedCases []PromotionRefundCase
			if err := tx.Select("wallet_debited_quota").Where("top_up_id = ?", topUp.Id).Find(&relatedCases).Error; err != nil {
				return err
			}
			walletDebitedQuota := int64(0)
			for i := range relatedCases {
				if relatedCases[i].WalletDebitedQuota < 0 ||
					walletDebitedQuota > math.MaxInt64-int64(relatedCases[i].WalletDebitedQuota) {
					return errors.New("existing principal refund accounting is inconsistent")
				}
				walletDebitedQuota += int64(relatedCases[i].WalletDebitedQuota)
			}
			var principalObligations []PromotionRefundObligation
			if err := tx.Select("amount", "asset").
				Where("source_type = ? AND source_id = ?", "top_ups", topUp.Id).
				Find(&principalObligations).Error; err != nil {
				return err
			}
			quotaAssessed := walletDebitedQuota
			cashAssessed := int64(0)
			for i := range principalObligations {
				obligation := &principalObligations[i]
				if obligation.Amount <= 0 {
					return errors.New("existing principal refund obligation is inconsistent")
				}
				if obligation.Asset == PromotionFundAssetQuota {
					if quotaAssessed > math.MaxInt64-obligation.Amount {
						return errors.New("existing principal quota assessment overflow")
					}
					quotaAssessed += obligation.Amount
				} else if obligation.Asset == PromotionFundAssetCash {
					if cashAssessed > math.MaxInt64-obligation.Amount {
						return errors.New("existing principal cash assessment overflow")
					}
					cashAssessed += obligation.Amount
				}
			}
			if input.Asset == PromotionFundAssetQuota {
				if topUp.CreditedQuota <= 0 || quotaAssessed > int64(topUp.CreditedQuota)-input.Amount {
					return errors.New("aggregate top-up refund quota exceeds the credited quota")
				}
			} else if topUp.PaidAmountMinor <= 0 || cashAssessed > topUp.PaidAmountMinor-input.Amount {
				return errors.New("aggregate top-up refund cash exceeds the verified paid amount")
			}
		} else {
			var sourceObligations []PromotionRefundObligation
			if err := tx.Select("amount").
				Where("source_type = ? AND source_id = ? AND asset = ? AND currency = ?",
					responsibility.sourceType, responsibility.sourceId, input.Asset, input.Currency).
				Find(&sourceObligations).Error; err != nil {
				return err
			}
			totalAssessed := int64(0)
			for i := range sourceObligations {
				if sourceObligations[i].Amount <= 0 || totalAssessed > math.MaxInt64-sourceObligations[i].Amount {
					return errors.New("existing refund responsibility assessment is inconsistent")
				}
				totalAssessed += sourceObligations[i].Amount
			}
			if totalAssessed > maximumResponsibilityAmount-input.Amount {
				return errors.New("aggregate refund obligation exceeds the verified responsibility amount")
			}
		}
	}

	if err := fencedUserIds.Ensure(input.UserId); err != nil {
		return err
	}
	var user User
	if err := lockForUpdate(tx.Unscoped()).Where("id = ?", input.UserId).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("manual obligation user not found")
		}
		return err
	}
	if input.Asset == PromotionFundAssetQuota {
		if user.RefundDebtQuota > math.MaxInt64-input.Amount {
			return errors.New("refund debt overflow")
		}
		user.RefundDebtQuota += input.Amount
	}
	user.RefundHold = true
	if err := tx.Unscoped().Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"refund_debt_quota": user.RefundDebtQuota, "refund_hold": true,
	}).Error; err != nil {
		return err
	}

	sourceType := responsibility.sourceType
	sourceId := responsibility.sourceId
	obligation := &PromotionRefundObligation{
		ObligationKey: actionKey + ":obligation", RefundCaseId: refundCase.Id, UserId: user.Id,
		Account: PromotionFundAccountRefundDebt, Asset: input.Asset, Currency: input.Currency,
		Amount: input.Amount, SourceType: sourceType, SourceId: sourceId,
	}
	if err := CreatePromotionRefundObligationTx(tx, obligation); err != nil {
		return err
	}
	if err := claimPromotionRefundRecoveryReceiptTx(tx, PromotionRefundReceiptPurposeManualEvidence,
		actionKey, refundCase.Id, obligation.Id, input.ExternalRef); err != nil {
		return err
	}

	leg := PromotionFundTransactionLeg{
		Account: PromotionFundAccountRefundDebt, Asset: input.Asset, Currency: input.Currency,
		Amount: input.Amount, SourceType: sourceType, SourceId: sourceId,
	}
	if input.Asset == PromotionFundAssetQuota {
		balance := user.RefundDebtQuota
		leg.BalanceAfter = &balance
	}
	transaction := &PromotionFundTransaction{
		TransactionKey: actionKey + ":fund", Kind: "refund_debt_assessment", UserId: user.Id,
		SourceType: "promotion_refund_cases", SourceId: refundCase.Id, SourceKey: actionKey,
		ActorType: "admin", ActorId: input.ActorId, ExternalRef: input.ExternalRef, Remark: input.Remark,
	}
	if err := CreatePromotionFundTransactionTx(tx, transaction, []PromotionFundTransactionLeg{leg}); err != nil {
		return err
	}
	action.ObligationId = obligation.Id
	action.UserId = user.Id
	action.TopUpId = effectiveTopUpId
	action.Asset = input.Asset
	action.Currency = input.Currency
	action.FundTransactionId = transaction.Id
	if err := tx.Create(action).Error; err != nil {
		return err
	}

	caseUpdates := map[string]interface{}{}
	if refundCase.UserId == 0 && responsibility.isTopUpPrincipal {
		caseUpdates["user_id"] = user.Id
	}
	if refundCase.TopUpId == 0 && effectiveTopUpId > 0 {
		caseUpdates["top_up_id"] = effectiveTopUpId
	}
	if input.Asset == PromotionFundAssetQuota {
		if refundCase.QuotaAmount < 0 || refundCase.DebtCreatedQuota < 0 ||
			refundCase.DebtCreatedQuota > math.MaxInt64-input.Amount {
			return errors.New("manual quota obligation exceeds the refund case range")
		}
		if isLegacyAccountingCase && responsibility.isTopUpPrincipal {
			// The migration stores a verified upper-bound estimate in QuotaAmount,
			// not an already-assessed debt. Root's decision replaces that estimate
			// so the case total is not counted twice.
			if input.Amount >= int64(common.MaxQuota) ||
				(refundCase.QuotaAmount > 0 && input.Amount > int64(refundCase.QuotaAmount)) {
				return errors.New("manual quota obligation exceeds the verified legacy refund amount")
			}
			refundCase.QuotaAmount = int(input.Amount)
		} else if !isLegacyAccountingCase {
			if input.Amount > int64(common.MaxQuota-refundCase.QuotaAmount) {
				return errors.New("manual quota obligation exceeds the refund case range")
			}
			refundCase.QuotaAmount += int(input.Amount)
		}
		refundCase.DebtCreatedQuota += input.Amount
		if responsibility.isTopUpPrincipal || !isLegacyAccountingCase {
			caseUpdates["quota_amount"] = refundCase.QuotaAmount
		}
		caseUpdates["debt_created_quota"] = refundCase.DebtCreatedQuota
	} else {
		if refundCase.CashDebtCreatedMinor < 0 || refundCase.CashDebtCreatedMinor > math.MaxInt64-input.Amount {
			return errors.New("manual cash obligation exceeds the refund case range")
		}
		refundCase.CashDebtCreatedMinor += input.Amount
		caseUpdates["cash_debt_created_minor"] = refundCase.CashDebtCreatedMinor
	}

	result := tx.Model(&PromotionRefundCase{}).
		Where("id = ? AND status = ? AND requires_root_review = ?", refundCase.Id, PromotionRefundCaseStatusPendingReview, true).
		Updates(caseUpdates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("refund case root-review state changed during manual assessment")
	}
	if refundCase.UserId == 0 && responsibility.isTopUpPrincipal {
		refundCase.UserId = user.Id
	}
	if refundCase.TopUpId == 0 && effectiveTopUpId > 0 {
		refundCase.TopUpId = effectiveTopUpId
	}
	return loadPromotionRefundRecoveryTx(tx, refundCase)
}

func applyPromotionRefundObligationActionTx(tx *gorm.DB, refundCase *PromotionRefundCase, obligation *PromotionRefundObligation,
	user *User, actionKey string, input PromotionRefundRecoveryActionInput,
) (*PromotionFundTransaction, error) {
	if tx == nil || refundCase == nil || obligation == nil || user == nil {
		return nil, errors.New("refund recovery state is required")
	}
	if obligation.Asset == PromotionFundAssetQuota && user.RefundDebtQuota < input.Amount {
		return nil, errors.New("refund debt balance is lower than the requested recovery amount")
	}

	legs := make([]PromotionFundTransactionLeg, 0, 2)
	kind := "refund_recovery"
	switch input.Action {
	case PromotionRefundActionRetryWalletDebit:
		if int64(user.Quota) < input.Amount {
			return nil, errors.New("user API balance is insufficient for this recovery")
		}
		user.Quota -= int(input.Amount)
		user.RefundDebtQuota -= input.Amount
		walletBalance := int64(user.Quota)
		debtBalance := user.RefundDebtQuota
		legs = append(legs,
			PromotionFundTransactionLeg{
				Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: -input.Amount,
				SourceType: obligation.SourceType, SourceId: obligation.SourceId, BalanceAfter: &walletBalance,
			},
			PromotionFundTransactionLeg{
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: -input.Amount,
				SourceType: obligation.SourceType, SourceId: obligation.SourceId, BalanceAfter: &debtBalance,
			},
		)
	case PromotionRefundActionRecordExternalRepayment:
		if obligation.Asset == PromotionFundAssetQuota {
			user.RefundDebtQuota -= input.Amount
			debtBalance := user.RefundDebtQuota
			legs = append(legs, PromotionFundTransactionLeg{
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: -input.Amount,
				SourceType: obligation.SourceType, SourceId: obligation.SourceId, BalanceAfter: &debtBalance,
			})
		} else {
			legs = append(legs, PromotionFundTransactionLeg{
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
				Currency: obligation.Currency, Amount: -input.Amount,
				SourceType: obligation.SourceType, SourceId: obligation.SourceId,
			})
		}
	case PromotionRefundActionRecoverPaidCommission:
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash, Currency: obligation.Currency, Amount: -input.Amount,
			SourceType: obligation.SourceType, SourceId: obligation.SourceId,
		})
	case PromotionRefundActionWaive:
		kind = "refund_waiver"
		if obligation.Asset == PromotionFundAssetQuota {
			user.RefundDebtQuota -= input.Amount
			debtBalance := user.RefundDebtQuota
			legs = append(legs, PromotionFundTransactionLeg{
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: -input.Amount,
				SourceType: obligation.SourceType, SourceId: obligation.SourceId, BalanceAfter: &debtBalance,
			})
		} else {
			legs = append(legs, PromotionFundTransactionLeg{
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash, Currency: obligation.Currency, Amount: -input.Amount,
				SourceType: obligation.SourceType, SourceId: obligation.SourceId,
			})
		}
	default:
		return nil, errors.New("invalid refund obligation action")
	}

	userUpdates := map[string]interface{}{"refund_debt_quota": user.RefundDebtQuota}
	if input.Action == PromotionRefundActionRetryWalletDebit {
		userUpdates["quota"] = user.Quota
	}
	if err := tx.Unscoped().Model(&User{}).Where("id = ?", user.Id).Updates(userUpdates).Error; err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	obligationUpdates := map[string]interface{}{"updated_at": now}
	if input.Action == PromotionRefundActionWaive {
		obligation.WaivedAmount += input.Amount
		obligationUpdates["waived_amount"] = obligation.WaivedAmount
	} else {
		obligation.RecoveredAmount += input.Amount
		obligationUpdates["recovered_amount"] = obligation.RecoveredAmount
	}
	if obligation.OutstandingAmount() == 0 {
		if obligation.WaivedAmount > 0 {
			obligation.Status = PromotionRefundObligationStatusWaived
		} else {
			obligation.Status = PromotionRefundObligationStatusRecovered
		}
		obligationUpdates["status"] = obligation.Status
	}
	result := tx.Model(&PromotionRefundObligation{}).
		Where("id = ? AND status = ?", obligation.Id, PromotionRefundObligationStatusOpen).
		Updates(obligationUpdates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, errors.New("refund obligation status changed during recovery")
	}

	transaction := &PromotionFundTransaction{
		TransactionKey: actionKey + ":fund", Kind: kind, UserId: user.Id,
		SourceType: "promotion_refund_cases", SourceId: refundCase.Id, SourceKey: actionKey,
		ActorType: "admin", ActorId: input.ActorId, ExternalRef: input.ExternalRef, Remark: input.Remark,
	}
	if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
		return nil, err
	}
	return transaction, nil
}

func validatePromotionRefundPrincipalAssessmentTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp) error {
	if tx == nil || refundCase == nil || topUp == nil || refundCase.TopUpId <= 0 || refundCase.UserId <= 0 ||
		refundCase.TopUpId != topUp.Id || refundCase.UserId != topUp.UserId {
		return errors.New("refund case has no verifiable principal responsibility")
	}
	if topUp.Status != common.TopUpStatusSuccess || !topUp.PaidAmountVerified || topUp.CreditedQuota <= 0 {
		return nil
	}
	if refundCase.WalletDebitedQuota > 0 {
		return nil
	}
	var assessedPrincipal int64
	if err := tx.Model(&PromotionRefundObligation{}).
		Where("refund_case_id = ? AND source_type = ? AND source_id = ? AND amount > 0",
			refundCase.Id, "top_ups", topUp.Id).
		Count(&assessedPrincipal).Error; err != nil {
		return err
	}
	if assessedPrincipal == 0 {
		return errors.New("verified successful top-up requires a quantified principal obligation before root review")
	}
	return nil
}

func releasePromotionRefundHoldTx(tx *gorm.DB, refundCase *PromotionRefundCase, fencedUserIds refundHoldFenceScope) (int, error) {
	if tx == nil || refundCase == nil || refundCase.TopUpId <= 0 || refundCase.UserId <= 0 {
		return 0, errors.New("refund case has no verifiable responsible user")
	}
	if refundCase.CommissionLedgerId > 0 {
		var ledger PromotionCommissionLedger
		err := lockForUpdate(tx).Where("id = ?", refundCase.CommissionLedgerId).First(&ledger).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errors.New("persisted commission ledger link is missing")
		}
		if err != nil {
			return 0, err
		}
		if err == nil {
			requiresQuarantine, err := promotionCommissionReconciliationQuarantineRequired(&ledger)
			if err != nil {
				return 0, err
			}
			if requiresQuarantine {
				quarantined, err := isPromotionCommissionReconciliationQuarantinedTx(tx, &ledger)
				if err != nil {
					return 0, err
				}
				if !quarantined {
					return 0, errors.New("unknown commission ledger requires root reconciliation quarantine before hold release")
				}
			}
		}
	}

	var caseObligations []PromotionRefundObligation
	if err := lockForUpdate(tx).
		Where("refund_case_id = ?", refundCase.Id).
		Order("id ASC").
		Find(&caseObligations).Error; err != nil {
		return 0, err
	}
	for i := range caseObligations {
		obligation := &caseObligations[i]
		if obligation.Amount <= 0 || obligation.RecoveredAmount < 0 || obligation.WaivedAmount < 0 ||
			obligation.RecoveredAmount > obligation.Amount || obligation.WaivedAmount > obligation.Amount-obligation.RecoveredAmount {
			return 0, errors.New("refund obligation balance is inconsistent")
		}
		if obligation.Status == PromotionRefundObligationStatusOpen || obligation.OutstandingAmount() != 0 {
			return 0, errors.New("refund case still has open obligations")
		}
	}

	userIds := make([]int, 0, 2)
	seenUserIds := map[int]struct{}{}
	if refundCase.UserId > 0 {
		seenUserIds[refundCase.UserId] = struct{}{}
		userIds = append(userIds, refundCase.UserId)
	}
	if refundCase.TopUpId > 0 {
		var topUp TopUp
		if err := lockForUpdate(tx).Where("id = ?", refundCase.TopUpId).First(&topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, errors.New("persisted refund top-up link is missing")
			}
			return 0, err
		} else {
			rebate, ledger, err := loadPromotionRefundLinkedCommissionTx(tx, refundCase, &topUp)
			if err != nil {
				return 0, err
			}
			if rebate != nil && rebate.InviterId > 0 {
				if _, exists := seenUserIds[rebate.InviterId]; !exists {
					seenUserIds[rebate.InviterId] = struct{}{}
					userIds = append(userIds, rebate.InviterId)
				}
			}
			if ledger != nil && ledger.UserId > 0 {
				if _, exists := seenUserIds[ledger.UserId]; !exists {
					seenUserIds[ledger.UserId] = struct{}{}
					userIds = append(userIds, ledger.UserId)
				}
			}
			var invitationRewards []InvitationReward
			if err := lockForUpdate(tx).Select("inviter_id").
				Where("trigger_top_up_id = ? AND reward_type = ? AND status = ?",
					topUp.Id, InvitationRewardTypeFirstTopUp, InvitationRewardStatusSettled).
				Find(&invitationRewards).Error; err != nil {
				return 0, err
			}
			for i := range invitationRewards {
				userId := invitationRewards[i].InviterId
				if userId <= 0 {
					continue
				}
				if _, exists := seenUserIds[userId]; !exists {
					seenUserIds[userId] = struct{}{}
					userIds = append(userIds, userId)
				}
			}
		}
	}
	for _, obligation := range caseObligations {
		userId := obligation.UserId
		if userId <= 0 {
			continue
		}
		if _, exists := seenUserIds[userId]; exists {
			continue
		}
		seenUserIds[userId] = struct{}{}
		userIds = append(userIds, userId)
	}
	var persistedCaseUserIds []int
	if err := tx.Model(&PromotionRefundCaseUser{}).
		Where("refund_case_id = ?", refundCase.Id).
		Pluck("user_id", &persistedCaseUserIds).Error; err != nil {
		return 0, err
	}
	for _, userId := range persistedCaseUserIds {
		if userId <= 0 {
			return 0, errors.New("refund case has an invalid responsible user")
		}
		if _, exists := seenUserIds[userId]; exists {
			continue
		}
		seenUserIds[userId] = struct{}{}
		userIds = append(userIds, userId)
	}
	if len(userIds) == 0 {
		return 0, errors.New("refund case has no verifiable responsible user")
	}
	sort.Ints(userIds)

	anyHeld := false
	for _, userId := range userIds {
		var user User
		if err := lockForUpdate(tx.Unscoped()).Where("id = ?", userId).First(&user).Error; err != nil {
			return 0, err
		}
		if user.RefundDebtQuota != 0 {
			return 0, fmt.Errorf("user %d still has refund quota debt", userId)
		}
		var userOpenObligations int64
		if err := tx.Model(&PromotionRefundObligation{}).
			Where("user_id = ? AND status = ?", userId, PromotionRefundObligationStatusOpen).
			Count(&userOpenObligations).Error; err != nil {
			return 0, err
		}
		if userOpenObligations != 0 {
			return 0, fmt.Errorf("user %d still has open refund obligations", userId)
		}

		obligationCases := tx.Model(&PromotionRefundObligation{}).Select("refund_case_id").Where("user_id = ?", userId)
		responsibilityCases := tx.Model(&PromotionRefundCaseUser{}).Select("refund_case_id").Where("user_id = ?", userId)
		commissionLedgers := tx.Model(&PromotionCommissionLedger{}).Select("id").Where("user_id = ?", userId)
		rebateIds := tx.Model(&InvitationRebate{}).Select("id").Where("inviter_id = ?", userId)
		rewardTopUpIds := tx.Model(&InvitationReward{}).Select("trigger_top_up_id").
			Where("inviter_id = ? AND trigger_top_up_id > 0 AND status = ?", userId, InvitationRewardStatusSettled)
		var pendingCases int64
		if err := tx.Model(&PromotionRefundCase{}).
			Where("id <> ? AND status = ?", refundCase.Id, PromotionRefundCaseStatusPendingReview).
			Where("(user_id = ? OR id IN (?) OR id IN (?) OR commission_ledger_id IN (?) OR invitation_rebate_id IN (?) OR top_up_id IN (?))",
				userId, obligationCases, responsibilityCases, commissionLedgers, rebateIds, rewardTopUpIds).
			Count(&pendingCases).Error; err != nil {
			return 0, err
		}
		if pendingCases != 0 {
			return 0, fmt.Errorf("user %d still has another pending refund case", userId)
		}
		if user.RefundHold {
			anyHeld = true
		}
		if err := fencedUserIds.Ensure(userId); err != nil {
			return 0, err
		}
		if err := tx.Unscoped().Model(&User{}).Where("id = ?", userId).Update("refund_hold", false).Error; err != nil {
			return 0, err
		}
	}
	if !anyHeld {
		return 0, errors.New("refund hold is already released")
	}
	if refundCase.UserId > 0 {
		return refundCase.UserId, nil
	}
	return userIds[0], nil
}
