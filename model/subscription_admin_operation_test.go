package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createSubscriptionAdminTestActor(t *testing.T, username string, role int) *User {
	t.Helper()
	actor := &User{
		Username: username, Password: "password123", Status: common.UserStatusEnabled,
		Role: role, Group: "default", AffCode: username + "-aff",
	}
	require.NoError(t, DB.Create(actor).Error)
	return actor
}

func TestGrantUserSubscriptionByAdminIsAuditedAndIdempotent(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "audited-subscription-grant", Password: "password123", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Audited grant", DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 5_000, UpgradeGroup: "pro", Enabled: true,
	}
	require.NoError(t, DB.Create(plan).Error)
	actor := createSubscriptionAdminTestActor(t, "audited-subscription-grant-actor", common.RoleRootUser)

	input := AdminSubscriptionOperationInput{
		UserId: user.Id, PlanId: plan.Id, ActorId: actor.Id, ActorRole: common.RoleRootUser,
		ActorRef: "root-admin", Reason: "verified service recovery", IdempotencyKey: "grant-audited-1",
	}
	message, replayed, err := GrantUserSubscriptionByAdmin(input)
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Contains(t, message, "pro")

	var operations []SubscriptionAdminOperation
	require.NoError(t, DB.Preload("Items").Find(&operations).Error)
	require.Len(t, operations, 1)
	operation := operations[0]
	assert.Equal(t, SubscriptionAdminOperationGrant, operation.Kind)
	assert.Equal(t, input.ActorId, operation.ActorId)
	assert.Equal(t, input.ActorRole, operation.ActorRole)
	assert.Equal(t, input.ActorRef, operation.ActorRef)
	assert.Equal(t, input.UserId, operation.TargetUserId)
	assert.Equal(t, input.Reason, operation.Reason)
	require.Len(t, operation.Items, 1)
	assert.Equal(t, user.Id, operation.Items[0].UserId)
	assert.Zero(t, operation.Items[0].AmountUsedAfter)

	message, replayed, err = GrantUserSubscriptionByAdmin(input)
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Contains(t, message, "pro")
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	assert.Equal(t, int64(1), subscriptionCount)

	conflict := input
	conflict.Reason = "different evidence"
	_, _, err = GrantUserSubscriptionByAdmin(conflict)
	require.ErrorIs(t, err, ErrSubscriptionAdminOperationConflict)
}

func TestGrantUserSubscriptionByAdminRollsBackWhenEvidenceItemFails(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "audited-subscription-grant-rollback", Password: "password123", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{Title: "Rollback grant", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)
	actor := createSubscriptionAdminTestActor(t, "audited-subscription-grant-rollback-actor", common.RoleAdminUser)

	expectedErr := errors.New("subscription operation item failed")
	callbackName := "test:fail_subscription_admin_operation_item"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "subscription_admin_operation_items" {
			tx.AddError(expectedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove(callbackName) })

	_, _, err := GrantUserSubscriptionByAdmin(AdminSubscriptionOperationInput{
		UserId: user.Id, PlanId: plan.Id, ActorId: actor.Id, ActorRole: common.RoleAdminUser,
		Reason: "support correction", IdempotencyKey: "grant-rollback",
	})
	require.ErrorIs(t, err, expectedErr)

	var subscriptionCount, operationCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.NoError(t, DB.Model(&SubscriptionAdminOperation{}).Count(&operationCount).Error)
	assert.Zero(t, subscriptionCount)
	assert.Zero(t, operationCount)
}

func TestInvalidateUserSubscriptionByAdminStoresTransitionAndReplays(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	user := &User{
		Username: "audited-subscription-invalidate", Password: "password123",
		Status: common.UserStatusEnabled, Group: "pro",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Audited invalidate", DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, TotalAmount: 5_000, UpgradeGroup: "pro", Enabled: true,
	}
	require.NoError(t, DB.Create(plan).Error)
	actor := createSubscriptionAdminTestActor(t, "audited-subscription-invalidate-actor", common.RoleRootUser)
	subscription := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: 5_000, AmountUsed: 720, QuotaGeneration: 3,
		StartTime: now - 3600, EndTime: now + 86400, Status: "active", Source: "admin",
		UpgradeGroup: "pro", PrevUserGroup: "default", DowngradeGroup: "default",
	}
	require.NoError(t, DB.Create(subscription).Error)

	input := AdminSubscriptionOperationInput{
		UserSubscriptionId: subscription.Id, ActorId: actor.Id, ActorRole: common.RoleRootUser,
		ActorRef: "root-admin", Reason: "verified entitlement correction", IdempotencyKey: "invalidate-audited-1",
	}
	result, replayed, err := InvalidateUserSubscriptionByAdmin(input)
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "active", result.StatusBefore)
	assert.Equal(t, "cancelled", result.StatusAfter)
	assert.Equal(t, subscription.EndTime, result.EndTimeBefore)
	assert.Less(t, result.EndTimeAfter, result.EndTimeBefore)
	assert.Equal(t, "pro", result.UserGroupBefore)
	assert.Equal(t, "default", result.UserGroupAfter)
	assert.Contains(t, result.Message, "default")

	var operation SubscriptionAdminOperation
	require.NoError(t, DB.Preload("Items").First(&operation).Error)
	assert.Equal(t, SubscriptionAdminOperationInvalidate, operation.Kind)
	assert.Equal(t, input.ActorId, operation.ActorId)
	assert.Equal(t, input.ActorRole, operation.ActorRole)
	assert.Equal(t, input.ActorRef, operation.ActorRef)
	assert.Equal(t, user.Id, operation.TargetUserId)
	assert.Equal(t, plan.Id, operation.PlanId)
	assert.Equal(t, input.Reason, operation.Reason)
	require.Len(t, operation.Items, 1)
	item := operation.Items[0]
	assert.Equal(t, subscription.Id, item.UserSubscriptionId)
	assert.Equal(t, int64(720), item.AmountUsedBefore)
	assert.Equal(t, int64(720), item.AmountUsedAfter)
	assert.Equal(t, int64(3), item.QuotaGenerationBefore)
	assert.Equal(t, int64(3), item.QuotaGenerationAfter)
	assert.Equal(t, "active", item.StatusBefore)
	assert.Equal(t, "cancelled", item.StatusAfter)
	assert.Equal(t, "pro", item.UserGroupBefore)
	assert.Equal(t, "default", item.UserGroupAfter)

	result, replayed, err = InvalidateUserSubscriptionByAdmin(input)
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.True(t, result.Replayed)
	assert.Equal(t, item.EndTimeAfter, result.EndTimeAfter)
	assert.Equal(t, "default", result.UserGroupAfter)
	var operationCount, itemCount int64
	require.NoError(t, DB.Model(&SubscriptionAdminOperation{}).Count(&operationCount).Error)
	require.NoError(t, DB.Model(&SubscriptionAdminOperationItem{}).Count(&itemCount).Error)
	assert.Equal(t, int64(1), operationCount)
	assert.Equal(t, int64(1), itemCount)

	conflict := input
	conflict.Reason = "different correction"
	_, _, err = InvalidateUserSubscriptionByAdmin(conflict)
	require.ErrorIs(t, err, ErrSubscriptionAdminOperationConflict)

	require.NoError(t, DB.Model(actor).Update("role", common.RoleAdminUser).Error)
	roleConflict := input
	roleConflict.ActorRole = common.RoleAdminUser
	_, _, err = InvalidateUserSubscriptionByAdmin(roleConflict)
	require.ErrorIs(t, err, ErrSubscriptionAdminOperationConflict)
	require.NoError(t, DB.Model(actor).Update("role", common.RoleRootUser).Error)

	secondSubscription := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: 5_000,
		StartTime: now - 60, EndTime: now + 7200, Status: "active", Source: "admin",
	}
	require.NoError(t, DB.Create(secondSubscription).Error)
	differentSubscription := input
	differentSubscription.UserSubscriptionId = secondSubscription.Id
	_, _, err = InvalidateUserSubscriptionByAdmin(differentSubscription)
	require.ErrorIs(t, err, ErrSubscriptionAdminOperationConflict)
	require.NoError(t, DB.First(secondSubscription, secondSubscription.Id).Error)
	assert.Equal(t, "active", secondSubscription.Status)

	inactiveNewKey := input
	inactiveNewKey.IdempotencyKey = "invalidate-audited-inactive"
	_, _, err = InvalidateUserSubscriptionByAdmin(inactiveNewKey)
	require.ErrorIs(t, err, ErrSubscriptionAdminEntitlementInactive)
	require.NoError(t, DB.Model(&SubscriptionAdminOperation{}).Count(&operationCount).Error)
	assert.Equal(t, int64(1), operationCount)
}

func TestInvalidateUserSubscriptionByAdminSerializesWithPreConsume(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	user := &User{
		Username: "subscription-invalidate-preconsume", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", AffCode: "subscription-invalidate-preconsume-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	actor := createSubscriptionAdminTestActor(t, "subscription-invalidate-preconsume-actor", common.RoleRootUser)
	plan := &SubscriptionPlan{
		Title: "Invalidate pre-consume", DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, TotalAmount: 1_000, Enabled: true,
	}
	require.NoError(t, DB.Create(plan).Error)
	subscription := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: 1_000,
		StartTime: now - 60, EndTime: now + 86400, Status: "active", Source: "admin",
	}
	require.NoError(t, DB.Create(subscription).Error)

	start := make(chan struct{})
	preConsumeResult := make(chan error, 1)
	invalidateResult := make(chan error, 1)
	go func() {
		<-start
		_, err := PreConsumeUserSubscription("invalidate-preconsume-race", user.Id, "gpt-test", "default", 0, 100)
		preConsumeResult <- err
	}()
	go func() {
		<-start
		_, _, err := InvalidateUserSubscriptionByAdmin(AdminSubscriptionOperationInput{
			UserSubscriptionId: subscription.Id, ActorId: actor.Id, ActorRole: common.RoleRootUser,
			Reason: "verified concurrent correction", IdempotencyKey: "invalidate-preconsume-race",
		})
		invalidateResult <- err
	}()
	close(start)

	preConsumeErr := <-preConsumeResult
	require.NoError(t, <-invalidateResult)
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, "cancelled", subscription.Status)
	var reserveCount int64
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).
		Where("request_id = ?", "invalidate-preconsume-race").Count(&reserveCount).Error)
	if preConsumeErr == nil {
		assert.Equal(t, int64(100), subscription.AmountUsed)
		assert.Equal(t, int64(1), reserveCount)
	} else {
		assert.Contains(t, preConsumeErr.Error(), "no active subscription")
		assert.Zero(t, subscription.AmountUsed)
		assert.Zero(t, reserveCount)
	}
}

func TestInvalidateUserSubscriptionByAdminRollsBackWhenEvidenceItemFails(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	user := &User{
		Username: "audited-subscription-invalidate-rollback", Password: "password123",
		Status: common.UserStatusEnabled, Group: "pro",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Invalidate rollback", DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, TotalAmount: 1_000, Enabled: true,
	}
	require.NoError(t, DB.Create(plan).Error)
	actor := createSubscriptionAdminTestActor(t, "audited-subscription-invalidate-rollback-actor", common.RoleAdminUser)
	subscription := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: 1_000,
		StartTime: now - 3600, EndTime: now + 86400, Status: "active",
		UpgradeGroup: "pro", PrevUserGroup: "default", DowngradeGroup: "default",
	}
	require.NoError(t, DB.Create(subscription).Error)

	expectedErr := errors.New("invalidation evidence failed")
	callbackName := "test:fail_subscription_invalidation_operation_item"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "subscription_admin_operation_items" {
			tx.AddError(expectedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove(callbackName) })

	_, _, err := InvalidateUserSubscriptionByAdmin(AdminSubscriptionOperationInput{
		UserSubscriptionId: subscription.Id, ActorId: actor.Id, ActorRole: common.RoleAdminUser,
		Reason: "support correction", IdempotencyKey: "invalidate-rollback",
	})
	require.ErrorIs(t, err, expectedErr)

	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, "active", subscription.Status)
	assert.Equal(t, now+86400, subscription.EndTime)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, "pro", user.Group)
	var operationCount int64
	require.NoError(t, DB.Model(&SubscriptionAdminOperation{}).Count(&operationCount).Error)
	assert.Zero(t, operationCount)
}

func TestResetUserSubscriptionsByPlanByAdminStoresBeforeAfterAndReplays(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Title: "Audited reset", DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 1_000, QuotaResetPeriod: SubscriptionResetDaily,
	}
	require.NoError(t, DB.Create(plan).Error)
	user := &User{
		Username: "audited-reset-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, DB.Create(user).Error)
	actor := createSubscriptionAdminTestActor(t, "audited-reset-user-actor", common.RoleRootUser)
	subscription := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: 1_000, AmountUsed: 640, QuotaGeneration: 4,
		StartTime: now - 3600, EndTime: now + 86400, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 3600,
	}
	require.NoError(t, DB.Create(subscription).Error)

	input := AdminSubscriptionOperationInput{
		UserId: user.Id, PlanId: plan.Id, AdvanceResetTime: false,
		ActorId: actor.Id, ActorRole: common.RoleRootUser, ActorRef: "root-admin",
		Reason: "customer-approved quota reset", IdempotencyKey: "reset-user-audited-1",
	}
	result, replayed, err := ResetUserSubscriptionsByPlanByAdmin(input)
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, 1, result.ResetCount)

	var operation SubscriptionAdminOperation
	require.NoError(t, DB.Preload("Items").First(&operation).Error)
	require.Len(t, operation.Items, 1)
	item := operation.Items[0]
	assert.Equal(t, int64(640), item.AmountUsedBefore)
	assert.Zero(t, item.AmountUsedAfter)
	assert.Equal(t, int64(4), item.QuotaGenerationBefore)
	assert.Equal(t, int64(5), item.QuotaGenerationAfter)

	result, replayed, err = ResetUserSubscriptionsByPlanByAdmin(input)
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, 1, result.ResetCount)
	var operationCount, itemCount int64
	require.NoError(t, DB.Model(&SubscriptionAdminOperation{}).Count(&operationCount).Error)
	require.NoError(t, DB.Model(&SubscriptionAdminOperationItem{}).Count(&itemCount).Error)
	assert.Equal(t, int64(1), operationCount)
	assert.Equal(t, int64(1), itemCount)

	updated := getSubscriptionResetSub(t, subscription.Id)
	assert.Zero(t, updated.AmountUsed)
	assert.Equal(t, int64(5), updated.QuotaGeneration)
}

func TestResetPlanSubscriptionsByAdminRollsBackUsageWhenEvidenceFails(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{Title: "Reset rollback", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1_000}
	require.NoError(t, DB.Create(plan).Error)
	user := &User{
		Username: "audited-plan-reset-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, DB.Create(user).Error)
	actor := createSubscriptionAdminTestActor(t, "audited-plan-reset-actor", common.RoleRootUser)
	subscription := &UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: 1_000, AmountUsed: 700, QuotaGeneration: 2,
		StartTime: now - 3600, EndTime: now + 86400, Status: "active",
	}
	require.NoError(t, DB.Create(subscription).Error)

	expectedErr := errors.New("reset evidence failed")
	callbackName := "test:fail_subscription_reset_operation_item"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "subscription_admin_operation_items" {
			tx.AddError(expectedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove(callbackName) })

	_, _, err := ResetPlanSubscriptionsByAdmin(AdminSubscriptionOperationInput{
		PlanId: plan.Id, AdvanceResetTime: true, ActorId: actor.Id, ActorRole: common.RoleRootUser,
		Reason: "verified global correction", IdempotencyKey: "reset-plan-rollback",
	})
	require.ErrorIs(t, err, expectedErr)

	updated := getSubscriptionResetSub(t, subscription.Id)
	assert.Equal(t, int64(700), updated.AmountUsed)
	assert.Equal(t, int64(2), updated.QuotaGeneration)
	var operationCount int64
	require.NoError(t, DB.Model(&SubscriptionAdminOperation{}).Count(&operationCount).Error)
	assert.Zero(t, operationCount)
}
