package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createSubscriptionPreConsumeFixture(t *testing.T, username string, resetPeriod string) (*User, *SubscriptionPlan, *UserSubscription) {
	t.Helper()
	now := GetDBTimestamp()
	user := &User{
		Username: username,
		AffCode:  username,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title:            username + " plan",
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      1_000,
		QuotaResetPeriod: resetPeriod,
		Enabled:          true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	sub := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id,
		AmountTotal: 1_000, AmountUsed: 0,
		StartTime: now - 60, EndTime: now + 30*24*3600,
		Status: "active", Source: "test",
	}
	require.NoError(t, DB.Create(sub).Error)
	return user, plan, sub
}

func TestSubscriptionPreConsumeLifecycleIsIdempotentAndRevisable(t *testing.T) {
	truncateTables(t)
	user, _, sub := createSubscriptionPreConsumeFixture(t, "subscription-lifecycle", SubscriptionResetNever)
	const requestId = "subscription-lifecycle-request"

	reserved, err := PreConsumeUserSubscription(requestId, user.Id, "test-model", "default", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(20), reserved.PreConsumed)
	assert.Equal(t, SubscriptionPreConsumeStatusReserved, reserved.Status)
	assert.Equal(t, int64(20), reserved.AmountUsedAfter)

	reserved, err = ReserveSubscriptionPreConsume(requestId, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(50), reserved.PreConsumed)
	assert.Equal(t, int64(50), reserved.AmountUsedAfter)
	reserved, err = ReserveSubscriptionPreConsume(requestId, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(50), reserved.AmountUsedAfter)

	settled, err := SettleSubscriptionPreConsume(requestId, 30)
	require.NoError(t, err)
	assert.Equal(t, SubscriptionPreConsumeStatusSettled, settled.Status)
	assert.Equal(t, int64(30), settled.FinalConsumed)
	assert.Equal(t, int64(30), settled.AmountUsedAfter)
	settled, err = SettleSubscriptionPreConsume(requestId, 30)
	require.NoError(t, err)
	assert.Equal(t, int64(30), settled.AmountUsedAfter)

	// An asynchronous terminal result may revise the submit-time estimate.
	settled, err = SettleSubscriptionPreConsume(requestId, 40)
	require.NoError(t, err)
	assert.Equal(t, int64(40), settled.FinalConsumed)
	assert.Equal(t, int64(40), settled.AmountUsedAfter)

	require.NoError(t, RefundSubscriptionPreConsume(requestId))
	require.NoError(t, RefundSubscriptionPreConsume(requestId))
	var storedSub UserSubscription
	require.NoError(t, DB.First(&storedSub, sub.Id).Error)
	assert.Zero(t, storedSub.AmountUsed)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", requestId).First(&record).Error)
	assert.Equal(t, SubscriptionPreConsumeStatusRefunded, record.Status)
	assert.Equal(t, int64(50), record.PreConsumed)
	assert.Zero(t, record.FinalConsumed)
}

func TestSubscriptionPreConsumeLifecycleDoesNotMutateNewResetGeneration(t *testing.T) {
	truncateTables(t)
	user, plan, sub := createSubscriptionPreConsumeFixture(t, "subscription-generation", SubscriptionResetDaily)
	actor := createSubscriptionAdminTestActor(t, "subscription-generation-reset-actor", common.RoleRootUser)

	_, err := PreConsumeUserSubscription("generation-refund", user.Id, "test-model", "default", 0, 30)
	require.NoError(t, err)
	_, _, err = ResetUserSubscriptionsByPlanByAdmin(AdminSubscriptionOperationInput{
		UserId: user.Id, PlanId: plan.Id, ActorId: actor.Id, ActorRole: common.RoleRootUser,
		Reason: "generation refund test", IdempotencyKey: "generation-refund-reset",
	})
	require.NoError(t, err)
	require.NoError(t, PostConsumeUserSubscriptionDelta(sub.Id, 10))
	require.NoError(t, RefundSubscriptionPreConsume("generation-refund"))

	var storedSub UserSubscription
	require.NoError(t, DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(1), storedSub.QuotaGeneration)
	assert.Equal(t, int64(10), storedSub.AmountUsed)

	_, err = PreConsumeUserSubscription("generation-settle", user.Id, "test-model", "default", 0, 20)
	require.NoError(t, err)
	_, _, err = ResetUserSubscriptionsByPlanByAdmin(AdminSubscriptionOperationInput{
		UserId: user.Id, PlanId: plan.Id, ActorId: actor.Id, ActorRole: common.RoleRootUser,
		Reason: "generation settle test", IdempotencyKey: "generation-settle-reset",
	})
	require.NoError(t, err)
	require.NoError(t, PostConsumeUserSubscriptionDelta(sub.Id, 7))
	settled, err := SettleSubscriptionPreConsume("generation-settle", 5)
	require.NoError(t, err)
	assert.False(t, settled.GenerationMatched)

	require.NoError(t, DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(2), storedSub.QuotaGeneration)
	assert.Equal(t, int64(7), storedSub.AmountUsed)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "generation-settle").First(&record).Error)
	assert.Equal(t, SubscriptionPreConsumeStatusSettled, record.Status)
	assert.Equal(t, int64(5), record.FinalConsumed)
}

func TestResetDueSubscriptionsAdvancesQuotaGeneration(t *testing.T) {
	truncateTables(t)
	_, plan, sub := createSubscriptionPreConsumeFixture(t, "subscription-period-reset", SubscriptionResetDaily)
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
		"amount_used":      40,
		"quota_generation": 4,
		"last_reset_time":  now - 2*24*3600,
		"next_reset_time":  now - 24*3600,
		"end_time":         now + 30*24*3600,
	}).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	resetCount, err := ResetDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, resetCount)
	var storedSub UserSubscription
	require.NoError(t, DB.First(&storedSub, sub.Id).Error)
	assert.Zero(t, storedSub.AmountUsed)
	assert.Equal(t, int64(5), storedSub.QuotaGeneration)
	assert.Greater(t, storedSub.NextResetTime, now)
}

func TestSubscriptionUsageUnderflowFailsWithoutChangingLifecycle(t *testing.T) {
	truncateTables(t)
	user, _, sub := createSubscriptionPreConsumeFixture(t, "subscription-underflow", SubscriptionResetNever)
	require.NoError(t, PostConsumeUserSubscriptionDelta(sub.Id, 5))
	err := PostConsumeUserSubscriptionDelta(sub.Id, -6)
	require.ErrorIs(t, err, ErrSubscriptionUsageUnderflow)

	_, err = PreConsumeUserSubscription("underflow-refund", user.Id, "test-model", "default", 0, 20)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("amount_used", 10).Error)
	err = RefundSubscriptionPreConsume("underflow-refund")
	require.ErrorIs(t, err, ErrSubscriptionUsageUnderflow)

	var storedSub UserSubscription
	require.NoError(t, DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(10), storedSub.AmountUsed)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "underflow-refund").First(&record).Error)
	assert.Equal(t, SubscriptionPreConsumeStatusReserved, record.Status)
}

func TestSettleSubscriptionPreConsumeTxRollsBackWithCallerTransaction(t *testing.T) {
	truncateTables(t)
	user, _, sub := createSubscriptionPreConsumeFixture(t, "subscription-tx", SubscriptionResetNever)
	_, err := PreConsumeUserSubscription("subscription-tx-request", user.Id, "test-model", "default", 0, 20)
	require.NoError(t, err)

	rollbackErr := errors.New("rollback task update")
	err = DB.Transaction(func(tx *gorm.DB) error {
		_, err := SettleSubscriptionPreConsumeTx(tx, "subscription-tx-request", 5)
		if err != nil {
			return err
		}
		return rollbackErr
	})
	require.ErrorIs(t, err, rollbackErr)

	var storedSub UserSubscription
	require.NoError(t, DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(20), storedSub.AmountUsed)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "subscription-tx-request").First(&record).Error)
	assert.Equal(t, SubscriptionPreConsumeStatusReserved, record.Status)
	assert.Zero(t, record.FinalConsumed)
}

func TestRefundedPreConsumeCleanupRetainsRevisableSettlements(t *testing.T) {
	truncateTables(t)
	user, _, _ := createSubscriptionPreConsumeFixture(t, "subscription-cleanup", SubscriptionResetNever)
	_, err := PreConsumeUserSubscription("cleanup-settled", user.Id, "test-model", "default", 0, 10)
	require.NoError(t, err)
	_, err = SettleSubscriptionPreConsume("cleanup-settled", 10)
	require.NoError(t, err)
	_, err = PreConsumeUserSubscription("cleanup-refunded", user.Id, "test-model", "default", 0, 10)
	require.NoError(t, err)
	require.NoError(t, RefundSubscriptionPreConsume("cleanup-refunded"))
	oldTime := time.Now().Add(-10 * 24 * time.Hour).Unix()
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id IN ?", []string{
		"cleanup-settled", "cleanup-refunded",
	}).Update("updated_at", oldTime).Error)

	deleted, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	var settledCount int64
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", "cleanup-settled").Count(&settledCount).Error)
	assert.Equal(t, int64(1), settledCount)
}
