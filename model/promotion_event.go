package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PromotionEventDirectionIncome  = "income"
	PromotionEventDirectionOutcome = "outcome"
	PromotionEventDirectionStatus  = "status"

	PromotionEventSourceInvitationReward     = "invitation_rewards"
	PromotionEventSourceInvitationRebate     = "invitation_rebates"
	PromotionEventSourceCommissionLedger     = "promotion_commission_ledgers"
	PromotionEventSourceWithdrawal           = "promotion_withdrawals"
	PromotionEventSourceCommissionTransfer   = "promotion_commission_transfer"
	PromotionEventSourceGrowthReward         = "growth_rewards"
	PromotionEventSourceGrowthSubmission     = "growth_submissions"
	PromotionEventSourceInvitationQuota      = "invitation_quota"
	PromotionEventSourceCommissionWithdrawal = "promotion_commission_withdrawal"

	PromotionEventTypeInvitationRegisterReward          = "invitation_register_reward"
	PromotionEventTypeInvitationFirstRequestReward      = "invitation_first_request_reward"
	PromotionEventTypeInvitationFirstTopUpReward        = "invitation_first_topup_reward"
	PromotionEventTypeCommissionPending                 = "commission_pending"
	PromotionEventTypeCommissionSettled                 = "commission_settled"
	PromotionEventTypeCommissionTransferred             = "commission_transferred"
	PromotionEventTypePromotionRewardTransferred        = "promotion_reward_transferred"
	PromotionEventTypeCommissionWithdrawSubmitted       = "commission_withdraw_submitted"
	PromotionEventTypeCommissionWithdrawApproved        = "commission_withdraw_approved"
	PromotionEventTypeCommissionWithdrawPayoutInitiated = "commission_withdraw_payout_initiated"
	PromotionEventTypeCommissionWithdrawFailed          = "commission_withdraw_failed"
	PromotionEventTypeCommissionWithdrawRejected        = "commission_withdraw_rejected"
	PromotionEventTypeCommissionWithdrawPaid            = "commission_withdraw_paid"
	PromotionEventTypeCommissionReversed                = "commission_reversed"
	PromotionEventTypeGrowthRewardSettled               = "growth_reward_settled"
	PromotionEventTypeGrowthSubmissionCreated           = "growth_submission_created"
	PromotionEventTypeGrowthSubmissionApproved          = "growth_submission_approved"
	PromotionEventTypeGrowthSubmissionRejected          = "growth_submission_rejected"
)

type PromotionEvent struct {
	Id              int    `json:"id"`
	EventKey        string `json:"event_key" gorm:"type:varchar(128);uniqueIndex"`
	UserId          int    `json:"user_id" gorm:"index"`
	EventType       string `json:"event_type" gorm:"type:varchar(64);index"`
	SourceTable     string `json:"source_table" gorm:"type:varchar(64);index"`
	SourceId        int    `json:"source_id" gorm:"index"`
	Direction       string `json:"direction" gorm:"type:varchar(16);index"`
	QuotaDelta      int    `json:"quota_delta"`
	CashAmountCents int64  `json:"cash_amount_cents"`
	Currency        string `json:"currency" gorm:"type:varchar(16);index"`
	Status          string `json:"status" gorm:"type:varchar(32);index"`
	Title           string `json:"title" gorm:"type:varchar(255)"`
	Remark          string `json:"remark" gorm:"type:text"`
	CreatedAt       int64  `json:"created_at" gorm:"index"`
}

func (event *PromotionEvent) BeforeCreate(_ *gorm.DB) error {
	if event.CreatedAt == 0 {
		event.CreatedAt = common.GetTimestamp()
	}
	if event.Currency == "" {
		event.Currency = "CNY"
	}
	if event.EventKey == "" {
		event.EventKey = BuildPromotionEventKey(event.EventType, event.SourceTable, event.SourceId)
	}
	return nil
}

func BuildPromotionEventKey(eventType string, sourceTable string, sourceId int) string {
	return fmt.Sprintf("%s:%s:%d", eventType, sourceTable, sourceId)
}

func CreatePromotionEventTx(tx *gorm.DB, event *PromotionEvent) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if event == nil || event.UserId <= 0 || event.EventType == "" || event.SourceTable == "" {
		return nil
	}
	if event.EventKey == "" {
		event.EventKey = BuildPromotionEventKey(event.EventType, event.SourceTable, event.SourceId)
	}
	var existing PromotionEvent
	err := tx.Select("id").Where("event_key = ?", event.EventKey).First(&existing).Error
	if err == nil {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(event).Error
}

func ListPromotionEvents(userId int, pageInfo *common.PageInfo) ([]*PromotionEvent, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
	var total int64
	if err := DB.Model(&PromotionEvent{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []*PromotionEvent
	err := DB.Where("user_id = ?", userId).
		Order("created_at DESC, id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&events).Error
	return events, total, err
}

func BackfillPromotionEventsForUser(userId int) error {
	if userId <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var invitationRewards []InvitationReward
		if err := tx.Where("inviter_id = ?", userId).Find(&invitationRewards).Error; err != nil {
			return err
		}
		for i := range invitationRewards {
			if err := CreateInvitationRewardEventTx(tx, &invitationRewards[i]); err != nil {
				return err
			}
		}

		var growthRewards []GrowthReward
		if err := tx.Where("user_id = ?", userId).Find(&growthRewards).Error; err != nil {
			return err
		}
		for i := range growthRewards {
			if err := CreateGrowthRewardEventTx(tx, &growthRewards[i]); err != nil {
				return err
			}
		}

		var ledgers []PromotionCommissionLedger
		if err := tx.Where("user_id = ?", userId).Find(&ledgers).Error; err != nil {
			return err
		}
		for i := range ledgers {
			if err := backfillPromotionCommissionLedgerEventsTx(tx, &ledgers[i]); err != nil {
				return err
			}
		}

		var withdrawals []PromotionWithdrawal
		if err := tx.Where("user_id = ?", userId).Find(&withdrawals).Error; err != nil {
			return err
		}
		for i := range withdrawals {
			if err := backfillPromotionWithdrawalEventsTx(tx, &withdrawals[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func backfillPromotionCommissionLedgerEventsTx(tx *gorm.DB, ledger *PromotionCommissionLedger) error {
	if ledger == nil || ledger.Id <= 0 {
		return nil
	}
	if ledger.Status == PromotionCommissionStatusPending {
		return CreatePromotionCommissionEventTx(tx, ledger, PromotionEventTypeCommissionPending)
	}
	if ledger.SettledAt > 0 || ledger.Status == PromotionCommissionStatusSettled ||
		ledger.Status == PromotionCommissionStatusTransferred ||
		ledger.Status == PromotionCommissionStatusWithdrawing ||
		ledger.Status == PromotionCommissionStatusWithdrawn {
		settledLedger := *ledger
		settledLedger.Status = PromotionCommissionStatusSettled
		if err := CreatePromotionCommissionEventTx(tx, &settledLedger, PromotionEventTypeCommissionSettled); err != nil {
			return err
		}
	}
	if ledger.Status == PromotionCommissionStatusReversed {
		return CreatePromotionCommissionEventTx(tx, ledger, PromotionEventTypeCommissionReversed)
	}
	return nil
}

func backfillPromotionWithdrawalEventsTx(tx *gorm.DB, withdrawal *PromotionWithdrawal) error {
	if withdrawal == nil || withdrawal.Id <= 0 {
		return nil
	}
	if err := CreatePromotionEventTx(tx, &PromotionEvent{
		UserId:          withdrawal.UserId,
		EventType:       PromotionEventTypeCommissionWithdrawSubmitted,
		SourceTable:     PromotionEventSourceWithdrawal,
		SourceId:        withdrawal.Id,
		Direction:       PromotionEventDirectionStatus,
		CashAmountCents: withdrawal.NetAmountCents,
		Currency:        withdrawal.Currency,
		Status:          PromotionWithdrawalStatusPendingReview,
		Title:           "Cash withdrawal request submitted",
		CreatedAt:       withdrawal.AppliedAt,
	}); err != nil {
		return err
	}
	switch withdrawal.Status {
	case PromotionWithdrawalStatusApproved:
		return createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawApproved, "Cash withdrawal request approved", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.ReviewedAt)
	case PromotionWithdrawalStatusProcessing:
		if err := createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawApproved, "Cash withdrawal request approved", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.ReviewedAt); err != nil {
			return err
		}
		return createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawPayoutInitiated, "Cash withdrawal payout initiated", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.PayoutInitiatedAt)
	case PromotionWithdrawalStatusRejected:
		return createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawRejected, "Cash withdrawal request rejected", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.ReviewedAt)
	case PromotionWithdrawalStatusFailed:
		return createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawFailed, "Cash withdrawal payout failed", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.ReviewedAt)
	case PromotionWithdrawalStatusPaid:
		if err := createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawApproved, "Cash withdrawal request approved", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.ReviewedAt); err != nil {
			return err
		}
		if withdrawal.PayoutInitiatedAt > 0 {
			if err := createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawPayoutInitiated, "Cash withdrawal payout initiated", PromotionEventDirectionStatus, withdrawal.NetAmountCents, withdrawal.PayoutInitiatedAt); err != nil {
				return err
			}
		}
		return createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawPaid, "Cash withdrawal paid", PromotionEventDirectionOutcome, -withdrawal.NetAmountCents, withdrawal.PaidAt)
	default:
		return nil
	}
}

func createPromotionWithdrawalStatusEventTx(tx *gorm.DB, withdrawal *PromotionWithdrawal, eventType string, title string, direction string, amountCents int64, createdAt int64) error {
	if createdAt == 0 {
		createdAt = withdrawal.CreatedAt
	}
	return CreatePromotionEventTx(tx, &PromotionEvent{
		UserId:          withdrawal.UserId,
		EventType:       eventType,
		SourceTable:     PromotionEventSourceWithdrawal,
		SourceId:        withdrawal.Id,
		Direction:       direction,
		CashAmountCents: amountCents,
		Currency:        withdrawal.Currency,
		Status:          withdrawal.Status,
		Title:           title,
		Remark:          withdrawal.ReviewNote,
		CreatedAt:       createdAt,
	})
}

func CreateInvitationRewardEventTx(tx *gorm.DB, reward *InvitationReward) error {
	if reward == nil || reward.Id <= 0 || reward.RewardQuota <= 0 {
		return nil
	}
	eventType := PromotionEventTypeInvitationRegisterReward
	title := "Invitation registration reward"
	switch reward.RewardType {
	case InvitationRewardTypeFirstRequest:
		eventType = PromotionEventTypeInvitationFirstRequestReward
		title = "Invitation first request reward"
	case InvitationRewardTypeFirstTopUp:
		eventType = PromotionEventTypeInvitationFirstTopUpReward
		title = "Invitation first top-up reward"
	}
	createdAt := reward.SettledAt
	if createdAt == 0 {
		createdAt = reward.CreatedAt
	}
	return CreatePromotionEventTx(tx, &PromotionEvent{
		UserId:      reward.InviterId,
		EventType:   eventType,
		SourceTable: PromotionEventSourceInvitationReward,
		SourceId:    reward.Id,
		Direction:   PromotionEventDirectionIncome,
		QuotaDelta:  reward.RewardQuota,
		Status:      reward.Status,
		Title:       title,
		Remark:      reward.Remark,
		CreatedAt:   createdAt,
	})
}

func CreateGrowthRewardEventTx(tx *gorm.DB, reward *GrowthReward) error {
	if reward == nil || reward.Id <= 0 {
		return nil
	}
	createdAt := reward.SettledAt
	if createdAt == 0 {
		createdAt = reward.CreatedAt
	}
	return CreatePromotionEventTx(tx, &PromotionEvent{
		UserId:      reward.UserId,
		EventType:   PromotionEventTypeGrowthRewardSettled,
		SourceTable: PromotionEventSourceGrowthReward,
		SourceId:    reward.Id,
		Direction:   PromotionEventDirectionIncome,
		QuotaDelta:  reward.RewardQuota,
		Status:      reward.Status,
		Title:       "Growth reward settled",
		Remark:      reward.Remark,
		CreatedAt:   createdAt,
	})
}

func CreateGrowthSubmissionEventTx(tx *gorm.DB, submission *GrowthSubmission, eventType string) error {
	if submission == nil || submission.Id <= 0 {
		return nil
	}
	title := ""
	remark := submission.ReviewNote
	createdAt := submission.ReviewedAt
	switch eventType {
	case PromotionEventTypeGrowthSubmissionCreated:
		title = "Growth submission created"
		remark = submission.Remark
		createdAt = submission.CreatedAt
	case PromotionEventTypeGrowthSubmissionApproved:
		title = "Growth submission approved"
	case PromotionEventTypeGrowthSubmissionRejected:
		title = "Growth submission rejected"
	default:
		return nil
	}
	if createdAt == 0 {
		createdAt = submission.CreatedAt
	}
	return CreatePromotionEventTx(tx, &PromotionEvent{
		UserId:      submission.UserId,
		EventType:   eventType,
		SourceTable: PromotionEventSourceGrowthSubmission,
		SourceId:    submission.Id,
		Direction:   PromotionEventDirectionStatus,
		Status:      submission.Status,
		Title:       title,
		Remark:      remark,
		CreatedAt:   createdAt,
	})
}

func CreatePromotionCommissionEventTx(tx *gorm.DB, ledger *PromotionCommissionLedger, eventType string) error {
	if ledger == nil || ledger.Id <= 0 || eventType == "" {
		return nil
	}
	direction := PromotionEventDirectionStatus
	quotaDelta := 0
	cashAmountCents := ledger.NetAmountCents
	title := "Cash commission updated"
	createdAt := ledger.CreatedAt
	switch eventType {
	case PromotionEventTypeCommissionPending:
		title = "Cash commission pending settlement"
	case PromotionEventTypeCommissionSettled:
		direction = PromotionEventDirectionIncome
		title = "Cash commission settled"
		createdAt = ledger.SettledAt
	case PromotionEventTypeCommissionReversed:
		direction = PromotionEventDirectionOutcome
		quotaDelta = -ledger.ReversalQuota
		if quotaDelta == 0 {
			quotaDelta = -ledger.QuotaEquivalent
		}
		cashAmountCents = -ledger.ReversalAmountCents
		if cashAmountCents == 0 {
			cashAmountCents = -ledger.NetAmountCents
		}
		title = "Cash commission reversed"
		createdAt = ledger.ReversedAt
	}
	if createdAt == 0 {
		createdAt = common.GetTimestamp()
	}
	return CreatePromotionEventTx(tx, &PromotionEvent{
		UserId:          ledger.UserId,
		EventType:       eventType,
		SourceTable:     PromotionEventSourceCommissionLedger,
		SourceId:        ledger.Id,
		Direction:       direction,
		QuotaDelta:      quotaDelta,
		CashAmountCents: cashAmountCents,
		Currency:        ledger.Currency,
		Status:          ledger.Status,
		Title:           title,
		Remark:          ledger.Remark,
		CreatedAt:       createdAt,
	})
}
