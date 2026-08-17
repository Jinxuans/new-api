package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromotionWithdrawalOperationHistoryAndQueries(t *testing.T) {
	truncate(t)
	seedUser(t, 3040, 0)
	seedFinancialActor(t, 41, common.RoleAdminUser)
	seedFinancialActor(t, 42, common.RoleAdminUser)
	seedPromotionCommissionLedger(t, 3040, 1200, 6000)

	withdrawal, err := CreatePromotionWithdrawal(3040, withPromotionCommissionBalanceExpectation(t, 3040, PromotionWithdrawalRequest{
		PayoutMethod:  "alipay",
		PayoutAccount: "user@example.com",
		Remark:        "first cash withdrawal",
	}))
	require.NoError(t, err)
	require.Len(t, withdrawal.Operations, 1)
	submitted := withdrawal.Operations[0]
	assert.Equal(t, model.PromotionWithdrawalActionSubmitted, submitted.Action)
	assert.Equal(t, model.PromotionWithdrawalActorUser, submitted.ActorType)
	assert.Equal(t, 3040, submitted.ActorId)
	assert.Equal(t, "first cash withdrawal", submitted.Note)
	assert.Empty(t, submitted.ExternalReference)
	assert.Equal(t, withdrawal.AppliedAt, submitted.CreatedAt)
	assert.NotZero(t, submitted.CreatedAt)
	require.ErrorIs(t, model.DB.Model(&model.PromotionWithdrawalOperation{Id: submitted.Id}).
		Update("note", "tampered").Error, model.ErrPromotionWithdrawalOperationImmutable)
	require.ErrorIs(t, model.DB.Delete(&model.PromotionWithdrawalOperation{Id: submitted.Id}).Error,
		model.ErrPromotionWithdrawalOperationImmutable)

	userDetail, err := GetPromotionWithdrawal(3040, withdrawal.Id)
	require.NoError(t, err)
	assert.Equal(t, withdrawal.Id, userDetail.Id)
	publicPayload, err := common.Marshal(userDetail)
	require.NoError(t, err)
	for _, internalField := range []string{
		`"user_id"`, `"payout_account_snapshot"`, `"reviewer_id"`,
		`"review_note"`, `"operations"`, `"actor_id"`, `"note"`,
	} {
		assert.False(t, strings.Contains(string(publicPayload), internalField), internalField)
	}

	_, err = GetPromotionWithdrawal(3041, withdrawal.Id)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 41, PromotionWithdrawalReviewRequest{
		ReviewNote: "identity checked",
	})
	require.NoError(t, err)
	require.Len(t, withdrawal.Operations, 2)
	approved := withdrawal.Operations[1]
	assert.Equal(t, model.PromotionWithdrawalActionApproved, approved.Action)
	assert.Equal(t, model.PromotionWithdrawalActorAdmin, approved.ActorType)
	assert.Equal(t, 41, approved.ActorId)
	assert.Equal(t, "identity checked", approved.Note)
	assert.Empty(t, approved.ExternalReference)
	assert.Equal(t, withdrawal.ReviewedAt, approved.CreatedAt)

	withdrawal, err = AdminRejectPromotionWithdrawal(withdrawal.Id, 42, PromotionWithdrawalReviewRequest{
		ReviewNote: "payout cancelled",
	})
	require.NoError(t, err)
	require.Len(t, withdrawal.Operations, 3)
	rejected := withdrawal.Operations[2]
	assert.Equal(t, model.PromotionWithdrawalActionRejected, rejected.Action)
	assert.Equal(t, model.PromotionWithdrawalActorAdmin, rejected.ActorType)
	assert.Equal(t, 42, rejected.ActorId)
	assert.Equal(t, "payout cancelled", rejected.Note)
	assert.Empty(t, rejected.ExternalReference)
	assert.Equal(t, withdrawal.ReviewedAt, rejected.CreatedAt)

	adminDetail, err := GetAdminPromotionWithdrawal(withdrawal.Id)
	require.NoError(t, err)
	require.Len(t, adminDetail.Operations, 3)

	withdrawals, total, err := ListAdminPromotionWithdrawals(&common.PageInfo{Page: 1, PageSize: 20}, "rejected")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, withdrawals, 1)
	require.Len(t, withdrawals[0].Operations, 3)
	assert.Equal(t, []string{
		model.PromotionWithdrawalActionSubmitted,
		model.PromotionWithdrawalActionApproved,
		model.PromotionWithdrawalActionRejected,
	}, []string{
		withdrawals[0].Operations[0].Action,
		withdrawals[0].Operations[1].Action,
		withdrawals[0].Operations[2].Action,
	})
}

func TestPromotionWithdrawalPayoutMustBeInitiatedBeforePaid(t *testing.T) {
	truncate(t)
	seedUser(t, 3042, 0)
	for _, actorId := range []int{51, 52, 53, 54} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	ledger := seedPromotionCommissionLedger(t, 3042, 1300, 6500)

	withdrawal, err := CreatePromotionWithdrawal(3042, withPromotionCommissionBalanceExpectation(t, 3042, PromotionWithdrawalRequest{
		PayoutMethod:  "bank",
		PayoutAccount: "account-3042",
	}))
	require.NoError(t, err)
	var reserve model.PromotionFundTransaction
	require.NoError(t, model.DB.Preload("Legs").
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:reserved", withdrawal.Id)).
		First(&reserve).Error)
	require.Len(t, reserve.Legs, 2)
	assert.Equal(t, model.PromotionFundAccountCommissionAvailable, reserve.Legs[0].Account)
	assert.Equal(t, -ledger.NetAmountCents, reserve.Legs[0].Amount)
	assert.Equal(t, model.PromotionFundAccountCommissionReserved, reserve.Legs[1].Account)
	assert.Equal(t, ledger.NetAmountCents, reserve.Legs[1].Amount)
	assert.Equal(t, ledger.Id, reserve.Legs[0].SourceId)
	assert.Equal(t, ledger.Id, reserve.Legs[1].SourceId)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 51, PromotionWithdrawalReviewRequest{
		ReviewNote: "approved",
	})
	require.NoError(t, err)

	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 52, PromotionWithdrawalReviewRequest{TradeNo: "payout-3042"})
	require.EqualError(t, err, "withdrawal status does not allow this operation")

	_, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 52, PromotionWithdrawalReviewRequest{TradeNo: "   "})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalPayoutReferenceRequired)

	withdrawal, err = GetAdminPromotionWithdrawal(withdrawal.Id)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusApproved, withdrawal.Status)
	assert.Empty(t, withdrawal.TradeNo)
	assert.Zero(t, withdrawal.PaidAt)
	require.Len(t, withdrawal.Operations, 2)
	require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusWithdrawing, ledger.Status)

	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 52, PromotionWithdrawalReviewRequest{
		TradeNo:    "  payout-3042  ",
		ReviewNote: "bank transfer submitted",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusProcessing, withdrawal.Status)
	assert.Equal(t, "payout-3042", withdrawal.TradeNo)
	assert.NotZero(t, withdrawal.PayoutInitiatedAt)
	require.Len(t, withdrawal.Operations, 3)
	initiated := withdrawal.Operations[2]
	assert.Equal(t, model.PromotionWithdrawalActionPayoutInitiated, initiated.Action)
	assert.Equal(t, model.PromotionWithdrawalActorAdmin, initiated.ActorType)
	assert.Equal(t, 52, initiated.ActorId)
	assert.Equal(t, "bank transfer submitted", initiated.Note)
	assert.Equal(t, "payout-3042", initiated.ExternalReference)
	assert.Equal(t, withdrawal.PayoutInitiatedAt, initiated.CreatedAt)
	var payoutCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id)).
		Count(&payoutCount).Error)
	assert.Zero(t, payoutCount, "initiation records audit state but does not claim the payout completed")

	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 53, PromotionWithdrawalReviewRequest{TradeNo: "different-reference"})
	require.EqualError(t, err, "payout reference does not match the initiated payout")

	withdrawal, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 53, PromotionWithdrawalReviewRequest{
		ReviewNote: "bank transfer completed",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusPaid, withdrawal.Status)
	assert.Equal(t, "payout-3042", withdrawal.TradeNo)
	require.Len(t, withdrawal.Operations, 4)
	paid := withdrawal.Operations[3]
	assert.Equal(t, model.PromotionWithdrawalActionPaid, paid.Action)
	assert.Equal(t, model.PromotionWithdrawalActorAdmin, paid.ActorType)
	assert.Equal(t, 53, paid.ActorId)
	assert.Equal(t, "bank transfer completed", paid.Note)
	assert.Equal(t, "payout-3042", paid.ExternalReference)
	assert.Equal(t, withdrawal.PaidAt, paid.CreatedAt)
	assert.NotZero(t, paid.CreatedAt)

	require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusWithdrawn, ledger.Status)
	var payout model.PromotionFundTransaction
	require.NoError(t, model.DB.Preload("Legs").
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id)).
		First(&payout).Error)
	require.Len(t, payout.Legs, 1)
	assert.Equal(t, model.PromotionFundAccountCommissionReserved, payout.Legs[0].Account)
	assert.Equal(t, -ledger.NetAmountCents, payout.Legs[0].Amount)
	assert.Equal(t, ledger.Id, payout.Legs[0].SourceId)

	retried, err := AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 54, PromotionWithdrawalReviewRequest{
		TradeNo:    "  PAYOUT-3042  ",
		ReviewNote: "  bank transfer completed  ",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusPaid, retried.Status)
	require.Len(t, retried.Operations, 4)

	assertPromotionWithdrawalPaidRecordCounts(t, withdrawal.Id, 1, 1, 1)
	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 54, PromotionWithdrawalReviewRequest{
		TradeNo:    "another-payout",
		ReviewNote: "bank transfer completed",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalPayoutReferenceConflict)
	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 54, PromotionWithdrawalReviewRequest{
		TradeNo:    "payout-3042",
		ReviewNote: "different confirmation",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalPaidConflict)
	assertPromotionWithdrawalPaidRecordCounts(t, withdrawal.Id, 1, 1, 1)

	// A later refund changes the commission ledger to reversed, but the paid
	// operation and immutable journal still make the original confirmation
	// safely retryable.
	require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
		Where("id = ?", ledger.Id).
		Update("status", model.PromotionCommissionStatusReversed).Error)
	retried, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 54, PromotionWithdrawalReviewRequest{
		ReviewNote: "bank transfer completed",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusPaid, retried.Status)
	assertPromotionWithdrawalPaidRecordCounts(t, withdrawal.Id, 1, 1, 1)
}

func assertPromotionWithdrawalPaidRecordCounts(t *testing.T, withdrawalId int, operationCount, eventCount, journalCount int64) {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.PromotionWithdrawalOperation{}).
		Where("withdrawal_id = ? AND action = ?", withdrawalId, model.PromotionWithdrawalActionPaid).
		Count(&count).Error)
	assert.Equal(t, operationCount, count)
	require.NoError(t, model.DB.Model(&model.PromotionEvent{}).
		Where("event_type = ? AND source_table = ? AND source_id = ?",
			model.PromotionEventTypeCommissionWithdrawPaid, model.PromotionEventSourceWithdrawal, withdrawalId).
		Count(&count).Error)
	assert.Equal(t, eventCount, count)
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawalId)).
		Count(&count).Error)
	assert.Equal(t, journalCount, count)
}

func TestPromotionWithdrawalPaidRollsBackOnJournalConflict(t *testing.T) {
	truncate(t)
	seedUser(t, 3043, 0)
	for _, actorId := range []int{61, 62, 63} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	ledger := seedPromotionCommissionLedger(t, 3043, 900, 4500)
	withdrawal, err := CreatePromotionWithdrawal(3043, withPromotionCommissionBalanceExpectation(t, 3043, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account-3043",
	}))
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 61, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 62, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3043", ReviewNote: "initiated",
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.CreatePromotionFundTransactionTx(tx, &model.PromotionFundTransaction{
			TransactionKey: fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id),
			Kind:           "conflicting_test_record",
			UserId:         withdrawal.UserId,
			SourceType:     "promotion_withdrawals",
			SourceId:       withdrawal.Id,
			ExternalRef:    withdrawal.TradeNo,
		}, []model.PromotionFundTransactionLeg{{
			Account: model.PromotionFundAccountAPIBalance, Asset: model.PromotionFundAssetQuota, Amount: 1,
		}})
	}))

	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 63, PromotionWithdrawalReviewRequest{ReviewNote: "paid"})
	require.ErrorIs(t, err, model.ErrPromotionFundTransactionConflict)
	withdrawal, err = GetAdminPromotionWithdrawal(withdrawal.Id)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusProcessing, withdrawal.Status)
	assert.Zero(t, withdrawal.PaidAt)
	require.Len(t, withdrawal.Operations, 3)
	require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusWithdrawing, ledger.Status)
}

func TestPromotionWithdrawalPaidRetryUsesReconstructedLegacyConfirmation(t *testing.T) {
	truncate(t)
	seedUser(t, 3045, 0)
	for _, actorId := range []int{81, 82, 83, 84} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	seedPromotionCommissionLedger(t, 3045, 800, 4000)

	withdrawal, err := CreatePromotionWithdrawal(3045, withPromotionCommissionBalanceExpectation(t, 3045, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account-3045",
	}))
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 81, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 82, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3045", ReviewNote: "initiated",
	})
	require.NoError(t, err)
	withdrawal, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 83, PromotionWithdrawalReviewRequest{
		ReviewNote: "paid from bank statement",
	})
	require.NoError(t, err)

	require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).
		Where("withdrawal_id = ?", withdrawal.Id).Delete(&model.PromotionWithdrawalOperation{}).Error)
	require.NoError(t, model.BackfillPromotionWithdrawalOperations(model.DB))

	retried, err := AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 84, PromotionWithdrawalReviewRequest{
		TradeNo: "PAYOUT-3045", ReviewNote: "paid from bank statement",
	})
	require.NoError(t, err)
	require.Len(t, retried.Operations, 2)
	assert.Equal(t, model.PromotionWithdrawalActionPaid, retried.Operations[1].Action)
	assert.True(t, retried.Operations[1].Reconstructed)
	assertPromotionWithdrawalPaidRecordCounts(t, withdrawal.Id, 1, 1, 1)
	adminRows, total, err := ListAdminPromotionWithdrawals(&common.PageInfo{Page: 1, PageSize: 20}, model.PromotionWithdrawalStatusPaid)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, adminRows, 1)
	require.Len(t, adminRows[0].Operations, 2)
	assert.True(t, adminRows[0].Operations[1].Reconstructed)
}

func TestPromotionWithdrawalPaidRetryAcceptsJournalWhenLegacyOperationCannotBeReconstructed(t *testing.T) {
	truncate(t)
	seedUser(t, 3046, 0)
	for _, actorId := range []int{91, 92, 93, 94} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	seedPromotionCommissionLedger(t, 3046, 700, 3500)

	withdrawal, err := CreatePromotionWithdrawal(3046, withPromotionCommissionBalanceExpectation(t, 3046, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account-3046",
	}))
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 91, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 92, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3046", ReviewNote: "initiated",
	})
	require.NoError(t, err)
	withdrawal, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 93, PromotionWithdrawalReviewRequest{
		ReviewNote: "confirmed without legacy timestamp",
	})
	require.NoError(t, err)

	require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).
		Where("withdrawal_id = ?", withdrawal.Id).Delete(&model.PromotionWithdrawalOperation{}).Error)
	require.NoError(t, model.DB.Model(&model.PromotionWithdrawal{}).
		Where("id = ?", withdrawal.Id).Update("paid_at", 0).Error)
	require.NoError(t, model.BackfillPromotionWithdrawalOperations(model.DB))

	retried, err := AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 94, PromotionWithdrawalReviewRequest{
		TradeNo: "PAYOUT-3046", ReviewNote: "confirmed without legacy timestamp",
	})
	require.NoError(t, err)
	require.Len(t, retried.Operations, 1)
	assert.Equal(t, model.PromotionWithdrawalActionSubmitted, retried.Operations[0].Action)
	assertPromotionWithdrawalPaidRecordCounts(t, withdrawal.Id, 0, 1, 1)

	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 94, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3046", ReviewNote: "different confirmation",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalPaidConflict)
}

func TestPromotionWithdrawalPayoutFailureReleasesReservedCommissionOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 3044, 0)
	for _, actorId := range []int{71, 72, 73, 74} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	ledger := seedPromotionCommissionLedger(t, 3044, 1450, 7250)
	withdrawal, err := CreatePromotionWithdrawal(3044, withPromotionCommissionBalanceExpectation(t, 3044, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account-3044",
	}))
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 71, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 72, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3044", ReviewNote: "submitted to bank",
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.InvitationRebate{}).
		Where("id = ?", ledger.SourceId).
		Update("paid_amount_verified", false).Error)
	require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
		Where("id = ?", ledger.Id).
		Update("cashable", false).Error)

	_, err = AdminMarkPromotionWithdrawalFailed(withdrawal.Id, 73, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3044",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalFailureReasonRequired)
	_, err = AdminMarkPromotionWithdrawalFailed(withdrawal.Id, 73, PromotionWithdrawalReviewRequest{
		TradeNo: "another-payout", FailureNote: "bank rejected the transfer",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalPayoutReferenceConflict)

	withdrawal, err = AdminMarkPromotionWithdrawalFailed(withdrawal.Id, 73, PromotionWithdrawalReviewRequest{
		TradeNo: "PAYOUT-3044", FailureNote: "bank rejected the transfer",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusFailed, withdrawal.Status)
	assert.Equal(t, "bank rejected the transfer", withdrawal.ReviewNote)
	assert.Zero(t, withdrawal.PaidAt)
	require.Len(t, withdrawal.Operations, 4)
	failed := withdrawal.Operations[3]
	assert.Equal(t, model.PromotionWithdrawalActionPayoutFailed, failed.Action)
	assert.Equal(t, model.PromotionWithdrawalActorAdmin, failed.ActorType)
	assert.Equal(t, 73, failed.ActorId)
	assert.Equal(t, "bank rejected the transfer", failed.Note)
	assert.Equal(t, "payout-3044", failed.ExternalReference)

	require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
	assert.False(t, ledger.Cashable)
	var release model.PromotionFundTransaction
	require.NoError(t, model.DB.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:released", withdrawal.Id)).First(&release).Error)
	require.Len(t, release.Legs, 2)
	assert.Equal(t, model.PromotionFundAccountCommissionReserved, release.Legs[0].Account)
	assert.Equal(t, -ledger.NetAmountCents, release.Legs[0].Amount)
	assert.Equal(t, model.PromotionFundAccountCommissionAvailable, release.Legs[1].Account)
	assert.Equal(t, ledger.NetAmountCents, release.Legs[1].Amount)

	retried, err := AdminMarkPromotionWithdrawalFailed(withdrawal.Id, 74, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3044", FailureNote: "bank rejected the transfer",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusFailed, retried.Status)
	require.Len(t, retried.Operations, 4)
	var releaseCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:released", withdrawal.Id)).Count(&releaseCount).Error)
	assert.Equal(t, int64(1), releaseCount)

	_, err = AdminMarkPromotionWithdrawalFailed(withdrawal.Id, 74, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3044", FailureNote: "different failure",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalFailureConflict)
	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 74, PromotionWithdrawalReviewRequest{
		TradeNo: "payout-3044", ReviewNote: "late payment confirmation",
	})
	require.EqualError(t, err, "withdrawal status does not allow this operation")

	var payoutCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id)).Count(&payoutCount).Error)
	assert.Zero(t, payoutCount)
}
