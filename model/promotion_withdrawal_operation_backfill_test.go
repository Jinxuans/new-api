package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openPromotionWithdrawalOperationBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openPromotionFundTransactionTestDB(t)
	require.NoError(t, db.AutoMigrate(&PromotionWithdrawal{}, &PromotionWithdrawalOperation{}))
	return db
}

func TestBackfillPromotionWithdrawalOperationsReconstructsOnlyProvableSteps(t *testing.T) {
	db := openPromotionWithdrawalOperationBackfillTestDB(t)
	require.NoError(t, db.Create(&[]PromotionWithdrawal{
		{Id: 1, UserId: 11, Status: PromotionWithdrawalStatusPendingReview, AppliedAt: 10, CreatedAt: 10},
		{Id: 2, UserId: 12, Status: PromotionWithdrawalStatusApproved, ReviewerId: 21, ReviewNote: "approved", AppliedAt: 20, ReviewedAt: 22, CreatedAt: 20},
		{Id: 3, UserId: 13, Status: PromotionWithdrawalStatusProcessing, ReviewerId: 22, ReviewNote: "sent", TradeNo: " payout-3 ", AppliedAt: 30, PayoutInitiatedAt: 33, CreatedAt: 30},
		{Id: 4, UserId: 14, Status: PromotionWithdrawalStatusPaid, ReviewerId: 23, ReviewNote: "confirmed", TradeNo: "payout-4", AppliedAt: 40, PaidAt: 44, CreatedAt: 40},
		{Id: 5, UserId: 15, Status: PromotionWithdrawalStatusFailed, ReviewNote: "legacy failure", TradeNo: "payout-5", AppliedAt: 50, ReviewedAt: 55, CreatedAt: 50},
	}).Error)

	require.NoError(t, BackfillPromotionWithdrawalOperations(db))
	require.NoError(t, BackfillPromotionWithdrawalOperations(db))

	var operations []PromotionWithdrawalOperation
	require.NoError(t, db.Order("withdrawal_id ASC").Order("created_at ASC").Order("id ASC").Find(&operations).Error)
	require.Len(t, operations, 9)
	assert.Equal(t, []string{
		PromotionWithdrawalActionSubmitted,
		PromotionWithdrawalActionSubmitted, PromotionWithdrawalActionApproved,
		PromotionWithdrawalActionSubmitted, PromotionWithdrawalActionPayoutInitiated,
		PromotionWithdrawalActionSubmitted, PromotionWithdrawalActionPaid,
		PromotionWithdrawalActionSubmitted, PromotionWithdrawalActionPayoutFailed,
	}, []string{
		operations[0].Action,
		operations[1].Action, operations[2].Action,
		operations[3].Action, operations[4].Action,
		operations[5].Action, operations[6].Action,
		operations[7].Action, operations[8].Action,
	})
	for i := range operations {
		assert.True(t, operations[i].Reconstructed)
	}
	assert.Equal(t, "payout-4", operations[6].ExternalReference)
	assert.Equal(t, "confirmed", operations[6].Note)
	assert.Equal(t, PromotionWithdrawalActorLegacy, operations[8].ActorType)
	assert.Zero(t, operations[8].ActorId)
}

func TestBackfillPromotionWithdrawalOperationsCompletesPartialExistingHistory(t *testing.T) {
	db := openPromotionWithdrawalOperationBackfillTestDB(t)
	withdrawal := &PromotionWithdrawal{
		Id: 10, UserId: 20, Status: PromotionWithdrawalStatusPaid,
		TradeNo: "paid-10", AppliedAt: 100, PaidAt: 110, CreatedAt: 100,
	}
	require.NoError(t, db.Create(withdrawal).Error)
	require.NoError(t, db.Create(&PromotionWithdrawalOperation{
		WithdrawalId: withdrawal.Id, Action: PromotionWithdrawalActionSubmitted,
		ActorType: PromotionWithdrawalActorUser, ActorId: withdrawal.UserId, CreatedAt: withdrawal.AppliedAt,
	}).Error)

	require.NoError(t, BackfillPromotionWithdrawalOperations(db))
	var operations []PromotionWithdrawalOperation
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).
		Order("created_at ASC").Order("id ASC").Find(&operations).Error)
	require.Len(t, operations, 2)
	assert.False(t, operations[0].Reconstructed)
	assert.Equal(t, PromotionWithdrawalActionPaid, operations[1].Action)
	assert.True(t, operations[1].Reconstructed)
}

func TestBackfillPromotionWithdrawalOperationsSkipsUnprovableTerminalStep(t *testing.T) {
	db := openPromotionWithdrawalOperationBackfillTestDB(t)
	withdrawal := &PromotionWithdrawal{
		Id: 11, UserId: 21, Status: PromotionWithdrawalStatusPaid,
		ReviewerId: 31, ReviewNote: "legacy paid state", AppliedAt: 120, CreatedAt: 120,
	}
	require.NoError(t, db.Create(withdrawal).Error)

	require.NoError(t, BackfillPromotionWithdrawalOperations(db))
	var operations []PromotionWithdrawalOperation
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).Find(&operations).Error)
	require.Len(t, operations, 1)
	assert.Equal(t, PromotionWithdrawalActionSubmitted, operations[0].Action)
	assert.True(t, operations[0].Reconstructed)
}

func TestBackfillPromotionWithdrawalOperationsPreservesRefundCancellation(t *testing.T) {
	db := openPromotionWithdrawalOperationBackfillTestDB(t)
	withdrawal := &PromotionWithdrawal{
		Id: 12, UserId: 22, Status: PromotionWithdrawalStatusFailed,
		TradeNo: "cancelled-payout", ReviewNote: "cancelled by refund",
		AppliedAt: 140, ReviewedAt: 150, CreatedAt: 140,
	}
	require.NoError(t, db.Create(withdrawal).Error)
	require.NoError(t, CreatePromotionWithdrawalOperationTx(db, &PromotionWithdrawalOperation{
		WithdrawalId: withdrawal.Id, Action: PromotionWithdrawalActionCancelledByRefund,
		ActorType: PromotionWithdrawalActorSystem, ExternalReference: withdrawal.TradeNo,
		Note: withdrawal.ReviewNote, CreatedAt: withdrawal.ReviewedAt,
	}))

	require.NoError(t, BackfillPromotionWithdrawalOperations(db))
	var operations []PromotionWithdrawalOperation
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).
		Order("created_at ASC").Order("id ASC").Find(&operations).Error)
	require.Len(t, operations, 2)
	assert.Equal(t, PromotionWithdrawalActionSubmitted, operations[0].Action)
	assert.Equal(t, PromotionWithdrawalActionCancelledByRefund, operations[1].Action)
	assert.True(t, operations[0].Reconstructed)
	assert.False(t, operations[1].Reconstructed)
}
