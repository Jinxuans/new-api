package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromotionFundReconcileTaskIgnoresProviderPollingSwitch(t *testing.T) {
	previousUpdateTask := constant.UpdateTask
	constant.UpdateTask = false
	t.Cleanup(func() { constant.UpdateTask = previousUpdateTask })

	assert.True(t, promotionFundReconcileHandler{}.Enabled())
}

func TestPromotionFundReconcileTaskRepairsLateLegacyTransition(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.AutoMigrate(
		&model.PromotionFundTransaction{},
		&model.PromotionFundTransactionLeg{},
		&model.PromotionFundBackfillCheckpoint{},
		&model.GrowthReward{},
		&model.InvitationRebate{},
		&model.InvitationReward{},
		&model.PromotionCommissionLedger{},
		&model.PromotionWithdrawal{},
		&model.PromotionWithdrawalItem{},
		&model.PromotionWithdrawalOperation{},
		&model.Redemption{},
		&model.SubscriptionOrder{},
		&model.TopUp{},
		&model.PromotionRefundCase{},
		&model.PromotionRefundObligation{},
		&model.PromotionRefundAction{},
		&model.PromotionEvent{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	reward := &model.GrowthReward{
		UserId: 77, ItemCode: "late_legacy_transition", RewardQuota: 50,
		Status: model.GrowthRewardStatusPending, CreatedAt: 100,
	}
	require.NoError(t, db.Create(reward).Error)
	require.NoError(t, model.BackfillPromotionFundTransactions(db))
	require.NoError(t, db.Model(reward).Updates(map[string]interface{}{
		"status": model.GrowthRewardStatusSettled, "settled_at": int64(110),
	}).Error)
	withdrawal := &model.PromotionWithdrawal{
		UserId: 78, Status: model.PromotionWithdrawalStatusPaid,
		Currency: "CNY", GrossAmountCents: 100, NetAmountCents: 100,
		TradeNo: "late-legacy-payout", ReviewerId: 9, ReviewNote: "confirmed",
		AppliedAt: 120, PaidAt: 130, CreatedAt: 120,
	}
	require.NoError(t, db.Create(withdrawal).Error)
	ledger := &model.PromotionCommissionLedger{
		UserId: 78, SourceType: "scheduled_reconcile_test", SourceId: 1,
		Cashable: true, Currency: "CNY", GrossAmountCents: 100, NetAmountCents: 100,
		Status:    model.PromotionCommissionStatusWithdrawn,
		SettledAt: 115, WithdrawnAt: 130, CreatedAt: 110,
	}
	require.NoError(t, db.Create(ledger).Error)
	require.NoError(t, db.Create(&model.PromotionWithdrawalItem{
		WithdrawalId: withdrawal.Id, LedgerId: ledger.Id, AmountCents: 100, CreatedAt: 120,
	}).Error)

	task, err := model.CreateSystemTask(model.SystemTaskTypePromotionFundReconcile, nil, nil)
	require.NoError(t, err)
	claimedTask, claimed, err := model.ClaimSystemTask(
		task.ID, task.Type, "promotion-fund-test-runner", common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)

	promotionFundReconcileHandler{}.Run(context.Background(), claimedTask, "promotion-fund-test-runner")

	completedTask, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusSucceeded, completedTask.Status)
	var journalCount int64
	require.NoError(t, db.Model(&model.PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "growth_rewards", reward.Id).
		Count(&journalCount).Error)
	assert.Equal(t, int64(1), journalCount)
	var operations []model.PromotionWithdrawalOperation
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).
		Order("created_at ASC").Order("id ASC").Find(&operations).Error)
	require.Len(t, operations, 2)
	assert.Equal(t, model.PromotionWithdrawalActionSubmitted, operations[0].Action)
	assert.Equal(t, model.PromotionWithdrawalActionPaid, operations[1].Action)
	assert.True(t, operations[0].Reconstructed)
	assert.True(t, operations[1].Reconstructed)
	journalFound := false
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		found, err := model.ValidatePromotionWithdrawalPaidTransactionTx(tx, withdrawal)
		journalFound = found
		return err
	}))
	assert.True(t, journalFound)

	// A broken fund source must fail the task without starving the independent
	// withdrawal-operation repair.
	invalidLedger := &model.PromotionCommissionLedger{
		UserId: 79, SourceType: "scheduled_reconcile_invalid", SourceId: 2,
		Cashable: true, Currency: "CNY", GrossAmountCents: 25, NetAmountCents: 25,
		Status: "unknown_legacy_state", CreatedAt: 200,
	}
	require.NoError(t, db.Create(invalidLedger).Error)
	lateWithdrawal := &model.PromotionWithdrawal{
		UserId: 80, Status: model.PromotionWithdrawalStatusPendingReview,
		Currency: "CNY", GrossAmountCents: 30, NetAmountCents: 30,
		AppliedAt: 210, CreatedAt: 210,
	}
	require.NoError(t, db.Create(lateWithdrawal).Error)

	failedTask, err := model.CreateSystemTask(model.SystemTaskTypePromotionFundReconcile, nil, nil)
	require.NoError(t, err)
	claimedFailedTask, claimed, err := model.ClaimSystemTask(
		failedTask.ID, failedTask.Type, "promotion-fund-failure-runner", common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	promotionFundReconcileHandler{}.Run(context.Background(), claimedFailedTask, "promotion-fund-failure-runner")

	failedTask, err = model.GetSystemTaskByTaskID(failedTask.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, failedTask.Status)
	assert.Contains(t, failedTask.Error, "unknown promotion commission status")
	var repairedOperations []model.PromotionWithdrawalOperation
	require.NoError(t, db.Where("withdrawal_id = ?", lateWithdrawal.Id).Find(&repairedOperations).Error)
	require.Len(t, repairedOperations, 1)
	assert.Equal(t, model.PromotionWithdrawalActionSubmitted, repairedOperations[0].Action)
	assert.True(t, repairedOperations[0].Reconstructed)

	// A failed checkpoint batch remains retryable. Once the source anomaly is
	// corrected, the next leased run resumes and records the missing journal.
	require.NoError(t, db.Model(&model.PromotionCommissionLedger{}).
		Where("id = ?", invalidLedger.Id).
		Updates(map[string]interface{}{
			"status":     model.PromotionCommissionStatusSettled,
			"settled_at": int64(220),
		}).Error)
	retryTask, err := model.CreateSystemTask(model.SystemTaskTypePromotionFundReconcile, nil, nil)
	require.NoError(t, err)
	claimedRetryTask, claimed, err := model.ClaimSystemTask(
		retryTask.ID, retryTask.Type, "promotion-fund-retry-runner", common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	promotionFundReconcileHandler{}.Run(context.Background(), claimedRetryTask, "promotion-fund-retry-runner")

	retryTask, err = model.GetSystemTaskByTaskID(retryTask.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusSucceeded, retryTask.Status)
	var retriedJournalCount int64
	require.NoError(t, db.Model(&model.PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", invalidLedger.Id).
		Count(&retriedJournalCount).Error)
	assert.Positive(t, retriedJournalCount)
}
