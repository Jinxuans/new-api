package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const subscriptionAdminTestActorID = 9999

type subscriptionAdminControllerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Replayed bool `json:"replayed"`
	} `json:"data"`
}

func setupSubscriptionAdminControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupManageUserTestDB(t)
	confirmPaymentComplianceForTest(t)
	require.NoError(t, db.Create(&model.User{
		Id: subscriptionAdminTestActorID, Username: "root-subscription-operator",
		Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "root-subscription-operator-aff",
	}).Error)
	return db
}

func createSubscriptionAdminControllerFixture(t *testing.T, db *gorm.DB, suffix string) (*model.User, *model.SubscriptionPlan) {
	t.Helper()
	user := &model.User{
		Username: "subscription-admin-" + suffix, AffCode: "sub-admin-" + suffix,
		Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, db.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Title: "Subscription admin " + suffix, DurationUnit: model.SubscriptionDurationMonth,
		DurationValue: 1, TotalAmount: 10_000, QuotaResetPeriod: model.SubscriptionResetDaily, Enabled: true,
	}
	require.NoError(t, db.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	return user, plan
}

func performSubscriptionAdminControllerRequest(t *testing.T, path string, paramID int, payload map[string]any, handler func(*gin.Context)) subscriptionAdminControllerResponse {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if paramID > 0 {
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(paramID)}}
	}
	c.Set("id", subscriptionAdminTestActorID)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-subscription-operator")
	handler(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response subscriptionAdminControllerResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func countSubscriptionAdminManageLogs(t *testing.T, db *gorm.DB, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.Log{}).
		Where("user_id = ? AND type = ?", userID, model.LogTypeManage).
		Count(&count).Error)
	return count
}

func TestSubscriptionAdminControllersRequireReasonAndIdempotencyKey(t *testing.T) {
	db := setupSubscriptionAdminControllerTestDB(t)
	user, plan := createSubscriptionAdminControllerFixture(t, db, "validation")
	now := common.GetTimestamp()
	subscription := &model.UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount, AmountUsed: 450,
		QuotaGeneration: 3, StartTime: now - 3600, EndTime: now + 86400, Status: "active", Source: "admin",
	}
	require.NoError(t, db.Create(subscription).Error)

	requests := []struct {
		name    string
		path    string
		paramID int
		payload map[string]any
		handler func(*gin.Context)
	}{
		{
			name: "grant missing reason", path: fmt.Sprintf("/api/subscription/admin/users/%d/subscriptions", user.Id),
			payload: map[string]any{"plan_id": plan.Id, "idempotency_key": "grant-validation-reason"}, handler: AdminCreateUserSubscription,
		},
		{
			name: "grant missing idempotency key", path: fmt.Sprintf("/api/subscription/admin/users/%d/subscriptions", user.Id),
			payload: map[string]any{"plan_id": plan.Id, "reason": "verified correction"}, handler: AdminCreateUserSubscription,
		},
		{
			name: "reset missing reason", path: fmt.Sprintf("/api/subscription/admin/users/%d/subscriptions/reset", user.Id),
			payload: map[string]any{"plan_id": plan.Id, "idempotency_key": "reset-validation-reason"}, handler: AdminResetUserSubscriptionsByPlan,
		},
		{
			name: "reset missing idempotency key", path: fmt.Sprintf("/api/subscription/admin/users/%d/subscriptions/reset", user.Id),
			payload: map[string]any{"plan_id": plan.Id, "reason": "verified correction"}, handler: AdminResetUserSubscriptionsByPlan,
		},
		{
			name: "invalidate missing reason", path: fmt.Sprintf("/api/subscription/admin/user_subscriptions/%d/invalidate", subscription.Id),
			paramID: subscription.Id, payload: map[string]any{"idempotency_key": "invalidate-validation-reason"}, handler: AdminInvalidateUserSubscription,
		},
		{
			name: "invalidate missing idempotency key", path: fmt.Sprintf("/api/subscription/admin/user_subscriptions/%d/invalidate", subscription.Id),
			paramID: subscription.Id, payload: map[string]any{"reason": "verified correction"}, handler: AdminInvalidateUserSubscription,
		},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			paramID := request.paramID
			if paramID == 0 {
				paramID = user.Id
			}
			response := performSubscriptionAdminControllerRequest(t, request.path, paramID, request.payload, request.handler)
			assert.False(t, response.Success)
			assert.Equal(t, "参数错误", response.Message)
		})
	}

	var subscriptionCount, operationCount int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.NoError(t, db.Model(&model.SubscriptionAdminOperation{}).Count(&operationCount).Error)
	assert.Equal(t, int64(1), subscriptionCount)
	assert.Zero(t, operationCount)
	require.NoError(t, db.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(450), subscription.AmountUsed)
	assert.Equal(t, int64(3), subscription.QuotaGeneration)
	assert.Zero(t, countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))
	assert.Zero(t, countSubscriptionAdminManageLogs(t, db, user.Id))
}

func TestAdminSubscriptionGrantReplayDoesNotDuplicateEntitlementOrAuditAndConflictFails(t *testing.T) {
	db := setupSubscriptionAdminControllerTestDB(t)
	user, plan := createSubscriptionAdminControllerFixture(t, db, "grant-replay")
	path := fmt.Sprintf("/api/subscription/admin/users/%d/subscriptions", user.Id)
	payload := map[string]any{
		"plan_id": plan.Id, "reason": "verified service recovery", "idempotency_key": "controller-grant-replay",
	}

	first := performSubscriptionAdminControllerRequest(t, path, user.Id, payload, AdminCreateUserSubscription)
	require.True(t, first.Success)
	assert.False(t, first.Data.Replayed)
	second := performSubscriptionAdminControllerRequest(t, path, user.Id, payload, AdminCreateUserSubscription)
	require.True(t, second.Success)
	assert.True(t, second.Data.Replayed)

	var entitlementCount, operationCount, itemCount int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&entitlementCount).Error)
	require.NoError(t, db.Model(&model.SubscriptionAdminOperation{}).Count(&operationCount).Error)
	require.NoError(t, db.Model(&model.SubscriptionAdminOperationItem{}).Count(&itemCount).Error)
	assert.Equal(t, int64(1), entitlementCount)
	assert.Equal(t, int64(1), operationCount)
	assert.Equal(t, int64(1), itemCount)
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))

	conflictPayload := map[string]any{
		"plan_id": plan.Id, "reason": "different correction", "idempotency_key": "controller-grant-replay",
	}
	conflict := performSubscriptionAdminControllerRequest(t, path, user.Id, conflictPayload, AdminCreateUserSubscription)
	assert.False(t, conflict.Success)
	assert.Equal(t, model.ErrSubscriptionAdminOperationConflict.Error(), conflict.Message)
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&entitlementCount).Error)
	assert.Equal(t, int64(1), entitlementCount)
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))
}

func TestAdminSubscriptionInvalidateReplayDoesNotDuplicateEvidenceOrAuditAndConflictFails(t *testing.T) {
	db := setupSubscriptionAdminControllerTestDB(t)
	user, plan := createSubscriptionAdminControllerFixture(t, db, "invalidate-replay")
	now := common.GetTimestamp()
	subscription := &model.UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount, AmountUsed: 340,
		QuotaGeneration: 2, StartTime: now - 3600, EndTime: now + 86400,
		Status: "active", Source: "admin",
	}
	require.NoError(t, db.Create(subscription).Error)
	path := fmt.Sprintf("/api/subscription/admin/user_subscriptions/%d/invalidate", subscription.Id)
	payload := map[string]any{
		"reason": "verified entitlement correction", "idempotency_key": "controller-invalidate-replay",
	}

	first := performSubscriptionAdminControllerRequest(t, path, subscription.Id, payload, AdminInvalidateUserSubscription)
	require.True(t, first.Success)
	assert.False(t, first.Data.Replayed)
	require.NoError(t, db.First(subscription, subscription.Id).Error)
	firstEndTime := subscription.EndTime
	assert.Equal(t, "cancelled", subscription.Status)

	second := performSubscriptionAdminControllerRequest(t, path, subscription.Id, payload, AdminInvalidateUserSubscription)
	require.True(t, second.Success)
	assert.True(t, second.Data.Replayed)
	require.NoError(t, db.First(subscription, subscription.Id).Error)
	assert.Equal(t, firstEndTime, subscription.EndTime)

	var operationCount, itemCount int64
	require.NoError(t, db.Model(&model.SubscriptionAdminOperation{}).Count(&operationCount).Error)
	require.NoError(t, db.Model(&model.SubscriptionAdminOperationItem{}).Count(&itemCount).Error)
	assert.Equal(t, int64(1), operationCount)
	assert.Equal(t, int64(1), itemCount)
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))

	conflictPayload := map[string]any{
		"reason": "different correction", "idempotency_key": "controller-invalidate-replay",
	}
	conflict := performSubscriptionAdminControllerRequest(t, path, subscription.Id, conflictPayload, AdminInvalidateUserSubscription)
	assert.False(t, conflict.Success)
	assert.Equal(t, model.ErrSubscriptionAdminOperationConflict.Error(), conflict.Message)
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))

	inactivePayload := map[string]any{
		"reason": "duplicate attempt", "idempotency_key": "controller-invalidate-inactive",
	}
	inactive := performSubscriptionAdminControllerRequest(t, path, subscription.Id, inactivePayload, AdminInvalidateUserSubscription)
	assert.False(t, inactive.Success)
	assert.Equal(t, model.ErrSubscriptionAdminEntitlementInactive.Error(), inactive.Message)
	require.NoError(t, db.Model(&model.SubscriptionAdminOperation{}).Count(&operationCount).Error)
	assert.Equal(t, int64(1), operationCount)
}

func TestAdminSubscriptionResetReplayDoesNotResetOrAuditTwiceAndConflictFails(t *testing.T) {
	db := setupSubscriptionAdminControllerTestDB(t)
	user, plan := createSubscriptionAdminControllerFixture(t, db, "reset-replay")
	now := common.GetTimestamp()
	subscription := &model.UserSubscription{
		UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount, AmountUsed: 640,
		QuotaGeneration: 4, StartTime: now - 3600, EndTime: now + 86400, Status: "active", Source: "admin",
		LastResetTime: now - 3600, NextResetTime: now + 3600,
	}
	require.NoError(t, db.Create(subscription).Error)
	path := fmt.Sprintf("/api/subscription/admin/users/%d/subscriptions/reset", user.Id)
	payload := map[string]any{
		"plan_id": plan.Id, "advance_reset_time": false,
		"reason": "customer-approved quota reset", "idempotency_key": "controller-reset-replay",
	}

	first := performSubscriptionAdminControllerRequest(t, path, user.Id, payload, AdminResetUserSubscriptionsByPlan)
	require.True(t, first.Success)
	assert.False(t, first.Data.Replayed)
	second := performSubscriptionAdminControllerRequest(t, path, user.Id, payload, AdminResetUserSubscriptionsByPlan)
	require.True(t, second.Success)
	assert.True(t, second.Data.Replayed)

	require.NoError(t, db.First(subscription, subscription.Id).Error)
	assert.Zero(t, subscription.AmountUsed)
	assert.Equal(t, int64(5), subscription.QuotaGeneration)
	var operationCount, itemCount int64
	require.NoError(t, db.Model(&model.SubscriptionAdminOperation{}).Count(&operationCount).Error)
	require.NoError(t, db.Model(&model.SubscriptionAdminOperationItem{}).Count(&itemCount).Error)
	assert.Equal(t, int64(1), operationCount)
	assert.Equal(t, int64(1), itemCount)
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, user.Id))

	require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", subscription.Id).Update("amount_used", 321).Error)
	conflictPayload := map[string]any{
		"plan_id": plan.Id, "advance_reset_time": false,
		"reason": "different reset evidence", "idempotency_key": "controller-reset-replay",
	}
	conflict := performSubscriptionAdminControllerRequest(t, path, user.Id, conflictPayload, AdminResetUserSubscriptionsByPlan)
	assert.False(t, conflict.Success)
	assert.Equal(t, model.ErrSubscriptionAdminOperationConflict.Error(), conflict.Message)
	require.NoError(t, db.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(321), subscription.AmountUsed)
	assert.Equal(t, int64(5), subscription.QuotaGeneration)
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, subscriptionAdminTestActorID))
	assert.Equal(t, int64(1), countSubscriptionAdminManageLogs(t, db, user.Id))
}
