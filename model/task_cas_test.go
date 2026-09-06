package model

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
		&Token{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Log{},
		&Channel{},
		&QuotaData{},
		&Ability{},
		&TopUp{},
		&Redemption{},
		&InvitationRebate{},
		&InvitationReward{},
		&PromotionEvent{},
		&PromotionCommissionLedger{},
		&PromotionWithdrawal{},
		&PromotionWithdrawalItem{},
		&PromotionWithdrawalOperation{},
		&PromotionWithdrawalPayoutReference{},
		&PromotionRefundCase{},
		&PromotionRefundCaseUser{},
		&PromotionRefundObligation{},
		&PromotionRefundAction{},
		&PromotionRefundRecoveryReceipt{},
		&PromotionFundTransaction{},
		&PromotionFundTransactionLeg{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&BillingAdjustmentJournal{},
		&SubscriptionAdminOperation{},
		&SubscriptionAdminOperationItem{},
		&GrowthRewardItem{},
		&GrowthReward{},
		&GrowthRewardBudget{},
		&GrowthSubmission{},
		&Checkin{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		localUserRefundHoldFenceLock.Lock()
		localUserRefundHoldFenceLeases = make(map[int]map[string]time.Time)
		localUserRefundHoldFenceLock.Unlock()
		DB.Exec("DELETE FROM promotion_fund_legs")
		DB.Exec("DELETE FROM promotion_fund_transactions")
		DB.Exec("DELETE FROM promotion_refund_actions")
		DB.Exec("DELETE FROM promotion_refund_recovery_receipts")
		DB.Exec("DELETE FROM promotion_refund_obligations")
		DB.Exec("DELETE FROM promotion_refund_case_users")
		DB.Exec("DELETE FROM promotion_withdrawal_operations")
		DB.Exec("DELETE FROM promotion_withdrawal_payout_references")
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM auth_flows")
		DB.Exec("DELETE FROM external_identity_claims")
		DB.Exec("DELETE FROM user_sessions")
		DB.Exec("DELETE FROM passkey_credentials")
		DB.Exec("DELETE FROM two_fa_backup_codes")
		DB.Exec("DELETE FROM two_fas")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM redemptions")
		DB.Exec("DELETE FROM invitation_rebates")
		DB.Exec("DELETE FROM invitation_rewards")
		DB.Exec("DELETE FROM promotion_events")
		DB.Exec("DELETE FROM promotion_commission_ledgers")
		DB.Exec("DELETE FROM promotion_withdrawals")
		DB.Exec("DELETE FROM promotion_withdrawal_items")
		DB.Exec("DELETE FROM promotion_refund_cases")
		DB.Exec("DELETE FROM billing_adjustment_journals")
		DB.Exec("DELETE FROM subscription_pre_consume_records")
		DB.Exec("DELETE FROM subscription_admin_operation_items")
		DB.Exec("DELETE FROM subscription_admin_operations")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM growth_reward_items")
		DB.Exec("DELETE FROM growth_rewards")
		DB.Exec("DELETE FROM growth_reward_budgets")
		DB.Exec("DELETE FROM growth_submissions")
		DB.Exec("DELETE FROM checkins")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM system_instances")
		DB.Exec("DELETE FROM system_task_locks")
		DB.Exec("DELETE FROM system_tasks")
		DB.Exec("DELETE FROM users")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

func TestGetTaskForProtocolObservationScopesOwnerAndPlatform(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:   "task_protocol_scope",
		UserId:   7,
		Platform: "plugin-a",
		Status:   TaskStatusInProgress,
	}
	insertTask(t, task)

	got, exists, err := GetTaskForProtocolObservation(context.Background(), 7, "plugin-a", task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, task.ID, got.ID)

	for _, query := range []struct {
		userID   int
		platform string
	}{
		{userID: 8, platform: "plugin-a"},
		{userID: 7, platform: "plugin-b"},
	} {
		got, exists, err = GetTaskForProtocolObservation(context.Background(), query.userID, constant.TaskPlatform(query.platform), task.TaskID)
		require.NoError(t, err)
		assert.False(t, exists)
		assert.Nil(t, got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = GetTaskForProtocolObservation(cancelled, 7, "plugin-a", task.TaskID)
	require.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshotEqual_PluginStateAndPollFailures(t *testing.T) {
	base := taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"a"}`),
		PollFailures: 2,
	}
	assert.True(t, base.Equal(taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"a"}`),
		PollFailures: 2,
	}))
	assert.False(t, base.Equal(taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"b"}`),
		PollFailures: 2,
	}))
	assert.False(t, base.Equal(taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"a"}`),
		PollFailures: 3,
	}))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL:    "https://example.com/result.mp4",
			PluginState:  json.RawMessage(`{"req_key":"keep"}`),
			PollFailures: 3,
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
	assert.Equal(t, task.PrivateData.PluginState, snap.PluginState)
	assert.Equal(t, task.PrivateData.PollFailures, snap.PollFailures)
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

func createTaskUsageProjectionFixture(t *testing.T, operationKey string, userId int, channelId int, delta int64) {
	t.Helper()
	_, err := CreateBillingAdjustment(BillingAdjustmentInput{
		OperationKey:    operationKey,
		RequestId:       operationKey,
		Kind:            BillingAdjustmentKindTaskProjection,
		FundingSource:   taskBillingSourceWallet,
		UserId:          userId,
		ChannelId:       channelId,
		UsageDelta:      delta,
		UsageRequired:   true,
		FundingRequired: false,
		TokenRequired:   false,
		LogRequired:     false,
	})
	require.NoError(t, err)
}

func createTaskBillingTestUser(t *testing.T, id int, usedQuota int) *User {
	t.Helper()
	identity := fmt.Sprintf("task_projection_user_%d", id)
	user := &User{
		Id:          id,
		Username:    identity,
		AffCode:     identity,
		Status:      common.UserStatusEnabled,
		AuthVersion: 1,
		Quota:       10_000,
		UsedQuota:   usedQuota,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func createTaskBillingTestChannel(t *testing.T, id int, usedQuota int64) *Channel {
	t.Helper()
	channel := &Channel{Id: id, Name: fmt.Sprintf("task_projection_channel_%d", id), Key: "sk-test", UsedQuota: usedQuota}
	require.NoError(t, DB.Create(channel).Error)
	return channel
}

func TestApplyTaskQuotaTransitionReplayInvalidatesWalletCache(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := createTaskBillingTestUser(t, 4101, 100)
	channel := createTaskBillingTestChannel(t, 4101, 100)
	task := &Task{
		TaskID:    "task_wallet_cache_replay",
		UserId:    user.Id,
		ChannelId: channel.Id,
		Quota:     100,
		Group:     "default",
		Status:    TaskStatusInProgress,
		PrivateData: TaskPrivateData{
			BillingSource: taskBillingSourceWallet,
		},
		Properties: Properties{OriginModelName: "test-model"},
		Data:       json.RawMessage(`{}`),
	}
	insertTask(t, task)

	applied, err := ApplyTaskQuotaTransitionWithProjection(task.ID, 100, 150, TaskBillingProjectionInput{})
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, server.Set(getUserCacheKey(user.Id), "stale-pre-commit-snapshot"))
	require.True(t, server.Exists(getUserCacheKey(user.Id)))

	applied, err = ApplyTaskQuotaTransitionWithProjection(task.ID, 100, 150, TaskBillingProjectionInput{})
	require.NoError(t, err)
	assert.False(t, applied)
	assert.False(t, server.Exists(getUserCacheKey(user.Id)), "an idempotent replay must clear a cache that may predate an unknown commit")
}

func TestTaskUsageProjectionCompletesAcrossDeletedTargets(t *testing.T) {
	t.Run("soft-deleted user remains auditable", func(t *testing.T) {
		truncateTables(t)
		user := createTaskBillingTestUser(t, 4201, 100)
		channel := createTaskBillingTestChannel(t, 4201, 100)
		createTaskUsageProjectionFixture(t, "task-usage-soft-user", user.Id, channel.Id, 50)
		require.NoError(t, DB.Delete(user).Error)

		require.NoError(t, ApplyBillingUsageProjection("task-usage-soft-user"))

		var deletedUser User
		require.NoError(t, DB.Unscoped().First(&deletedUser, user.Id).Error)
		assert.Equal(t, 150, deletedUser.UsedQuota)
		var persistedChannel Channel
		require.NoError(t, DB.First(&persistedChannel, channel.Id).Error)
		assert.Equal(t, int64(150), persistedChannel.UsedQuota)
	})

	t.Run("hard-deleted user does not roll back channel", func(t *testing.T) {
		truncateTables(t)
		user := createTaskBillingTestUser(t, 4202, 100)
		channel := createTaskBillingTestChannel(t, 4202, 100)
		createTaskUsageProjectionFixture(t, "task-usage-hard-user", user.Id, channel.Id, 50)
		require.NoError(t, DB.Unscoped().Delete(user).Error)

		require.NoError(t, ApplyBillingUsageProjection("task-usage-hard-user"))

		var persistedChannel Channel
		require.NoError(t, DB.First(&persistedChannel, channel.Id).Error)
		assert.Equal(t, int64(150), persistedChannel.UsedQuota)
		row, err := GetBillingAdjustment("task-usage-hard-user")
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.True(t, row.UsageApplied)
		assert.Equal(t, BillingAdjustmentStatusCompleted, row.Status)
	})

	t.Run("hard-deleted channel does not roll back user", func(t *testing.T) {
		truncateTables(t)
		user := createTaskBillingTestUser(t, 4203, 100)
		channel := createTaskBillingTestChannel(t, 4203, 100)
		createTaskUsageProjectionFixture(t, "task-usage-hard-channel", user.Id, channel.Id, 50)
		require.NoError(t, DB.Delete(channel).Error)

		require.NoError(t, ApplyBillingUsageProjection("task-usage-hard-channel"))

		var persistedUser User
		require.NoError(t, DB.First(&persistedUser, user.Id).Error)
		assert.Equal(t, 150, persistedUser.UsedQuota)
		row, err := GetBillingAdjustment("task-usage-hard-channel")
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.True(t, row.UsageApplied)
		assert.Equal(t, BillingAdjustmentStatusCompleted, row.Status)
	})
}

func TestTaskUsageProjectionSaturatesEveryUsedQuotaToInt32(t *testing.T) {
	truncateTables(t)
	user := createTaskBillingTestUser(t, 4301, common.MaxQuota-2)
	channel := createTaskBillingTestChannel(t, 4301, int64(common.MaxQuota-2))
	createTaskUsageProjectionFixture(t, "task-usage-positive-saturation", user.Id, channel.Id, 10)

	require.NoError(t, ApplyBillingUsageProjection("task-usage-positive-saturation"))
	var persistedUser User
	require.NoError(t, DB.First(&persistedUser, user.Id).Error)
	assert.Equal(t, common.MaxQuota, persistedUser.UsedQuota)
	var persistedChannel Channel
	require.NoError(t, DB.First(&persistedChannel, channel.Id).Error)
	assert.Equal(t, int64(common.MaxQuota), persistedChannel.UsedQuota)

	minQuota := -common.MaxQuota - 1
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("used_quota", minQuota+2).Error)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("used_quota", minQuota+2).Error)
	createTaskUsageProjectionFixture(t, "task-usage-negative-saturation", user.Id, channel.Id, -10)
	require.NoError(t, ApplyBillingUsageProjection("task-usage-negative-saturation"))
	require.NoError(t, DB.First(&persistedUser, user.Id).Error)
	assert.Equal(t, minQuota, persistedUser.UsedQuota)
	require.NoError(t, DB.First(&persistedChannel, channel.Id).Error)
	assert.Equal(t, int64(minQuota), persistedChannel.UsedQuota)
}

func TestTaskBillingLogAndDashboardProjectionAreReplaySafe(t *testing.T) {
	truncateTables(t)
	oldDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = true
	t.Cleanup(func() { common.DataExportEnabled = oldDataExportEnabled })
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	user := createTaskBillingTestUser(t, 4401, 0)
	channel := createTaskBillingTestChannel(t, 4401, 0)
	const operationKey = "task-log-projection-replay"
	row, err := CreateBillingAdjustment(BillingAdjustmentInput{
		OperationKey:       operationKey,
		RequestId:          operationKey,
		Kind:               BillingAdjustmentKindTaskProjection,
		FundingSource:      taskBillingSourceWallet,
		UserId:             user.Id,
		ChannelId:          channel.Id,
		ModelName:          "test-model",
		UsingGroup:         "default",
		TaskId:             4401,
		LogRequired:        true,
		ProjectionLogType:  LogTypeConsume,
		ProjectionLogQuota: 50,
		ProjectionNodeName: "origin-node",
	})
	require.NoError(t, err)

	// Ordinary logs may legitimately share a request ID. They must neither be
	// rejected nor mistaken for the dedicated task projection.
	require.NoError(t, DB.Create(&Log{
		UserId:    user.Id,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeSystem,
		Content:   "ordinary log sharing a request id",
		RequestId: operationKey,
	}).Error)
	claimed, err := ClaimBillingAdjustmentLogProjection(operationKey, "worker-one", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, EnsureTaskBillingProjectionLog(row))
	require.NoError(t, ReleaseBillingAdjustmentLogProjection(operationKey, "worker-one"))

	claimed, err = ClaimBillingAdjustmentLogProjection(operationKey, "worker-two", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, EnsureTaskBillingProjectionLog(row))
	require.NoError(t, CompleteBillingAdjustmentLogProjection(operationKey, "worker-two"))
	require.NoError(t, CompleteBillingAdjustmentLogProjection(operationKey, "worker-two"))

	var taskLogCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).
		Where("request_id = ? AND type = ?", operationKey, LogTypeConsume).
		Count(&taskLogCount).Error)
	assert.Equal(t, int64(1), taskLogCount)
	var sharedRequestCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", operationKey).Count(&sharedRequestCount).Error)
	assert.Equal(t, int64(2), sharedRequestCount)

	var quotaData QuotaData
	require.NoError(t, DB.Where(map[string]any{
		"user_id":    user.Id,
		"model_name": "test-model",
		"use_group":  "default",
		"channel_id": channel.Id,
		"node_name":  "origin-node",
	}).First(&quotaData).Error)
	assert.Equal(t, 1, quotaData.Count)
	assert.Equal(t, 50, quotaData.Quota)
	finalRow, err := GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	require.NotNil(t, finalRow)
	assert.True(t, finalRow.LogApplied)
	assert.Equal(t, BillingAdjustmentStatusCompleted, finalRow.Status)
}

func TestTaskInitialUsagePersistsBeforeBatchAdjustmentJournal(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true
	user := createTaskBillingTestUser(t, 4501, 0)
	channel := createTaskBillingTestChannel(t, 4501, 0)

	require.NoError(t, RecordTaskInitialUsage(user.Id, channel.Id, 100))
	var persistedUser User
	require.NoError(t, DB.First(&persistedUser, user.Id).Error)
	assert.Equal(t, 100, persistedUser.UsedQuota)
	assert.Equal(t, 1, persistedUser.RequestCount)
	var persistedChannel Channel
	require.NoError(t, DB.First(&persistedChannel, channel.Id).Error)
	assert.Equal(t, int64(100), persistedChannel.UsedQuota)

	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		assert.Empty(t, batchUpdateStores[i], "initial task usage must not depend on an in-process batch queue")
		batchUpdateLocks[i].Unlock()
	}
	createTaskUsageProjectionFixture(t, "task-usage-after-durable-baseline", user.Id, channel.Id, 50)
	require.NoError(t, ApplyBillingUsageProjection("task-usage-after-durable-baseline"))
	require.NoError(t, DB.First(&persistedUser, user.Id).Error)
	assert.Equal(t, 150, persistedUser.UsedQuota)
	require.NoError(t, DB.First(&persistedChannel, channel.Id).Error)
	assert.Equal(t, int64(150), persistedChannel.UsedQuota)
}

func TestUpdateWithStatus_PersistsPluginStateAndPollFailures(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_plugin_state",
		Status: TaskStatusInProgress,
		Data:   json.RawMessage(`{}`),
		PrivateData: TaskPrivateData{
			PluginState:  json.RawMessage(`{"req_key":"old"}`),
			PollFailures: 1,
		},
	}
	insertTask(t, task)

	task.PrivateData.PluginState = json.RawMessage(`{"req_key":"new"}`)
	task.PrivateData.PollFailures = 4
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusInProgress, reloaded.Status)
	assert.JSONEq(t, `{"req_key":"new"}`, string(reloaded.PrivateData.PluginState))
	assert.Equal(t, 4, reloaded.PrivateData.PollFailures)
}
