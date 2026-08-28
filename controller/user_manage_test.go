package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupManageUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.UserSession{}, &model.Log{}, &model.CasbinRule{}, &model.AuthzRole{},
		&model.SubscriptionPlan{}, &model.UserSubscription{},
		&model.SubscriptionAdminOperation{}, &model.SubscriptionAdminOperationItem{},
		&model.TopUp{}, &model.InvitationRebate{}, &model.InvitationReward{},
		&model.PromotionCommissionLedger{}, &model.PromotionRefundCase{}, &model.PromotionRefundCaseUser{},
		&model.PromotionRefundObligation{},
		&model.PromotionFundTransaction{}, &model.PromotionFundTransactionLeg{},
	))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestAdminDeleteUserSubscriptionRetainsEvidenceAndRecordsAudit(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 9999, Username: "root-operator", Password: "password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "root-operator-aff",
	}).Error)
	user := &model.User{
		Username: "subscription-delete-audit-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	subscription := &model.UserSubscription{
		UserId: user.Id, PlanId: 91, AmountTotal: 1000,
		StartTime: common.GetTimestamp() - 60, EndTime: common.GetTimestamp() + 3600,
		Status: "active", Source: "admin",
	}
	require.NoError(t, db.Create(subscription).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body, err := common.Marshal(map[string]interface{}{
		"reason": "verified entitlement correction", "idempotency_key": "legacy-delete-invalidate",
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/subscription/admin/user_subscriptions/%d", subscription.Id), bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(subscription.Id)}}
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")
	AdminDeleteUserSubscription(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	var retained model.UserSubscription
	require.NoError(t, db.First(&retained, subscription.Id).Error)
	assert.Equal(t, "cancelled", retained.Status)
	assert.LessOrEqual(t, retained.EndTime, subscription.EndTime)

	var auditLog model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", 9999, model.LogTypeManage).
		Order("id DESC").First(&auditLog).Error)
	assert.Contains(t, auditLog.Content, fmt.Sprintf("subscription entitlement %d", subscription.Id))
	var other map[string]interface{}
	require.NoError(t, common.Unmarshal([]byte(auditLog.Other), &other))
	op, ok := other["op"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "subscription.entitlement_invalidate", op["action"])
	params, ok := op["params"].(map[string]interface{})
	require.True(t, ok)
	assert.EqualValues(t, user.Id, params["target_user_id"])
	assert.Equal(t, http.MethodDelete, params["request_method"])
	assert.Equal(t, "verified entitlement correction", params["reason"])

	var operation model.SubscriptionAdminOperation
	require.NoError(t, db.Preload("Items").First(&operation).Error)
	assert.Equal(t, model.SubscriptionAdminOperationInvalidate, operation.Kind)
	assert.Equal(t, "verified entitlement correction", operation.Reason)
	require.Len(t, operation.Items, 1)
	assert.Equal(t, "active", operation.Items[0].StatusBefore)
	assert.Equal(t, "cancelled", operation.Items[0].StatusAfter)
}

func performManageUserRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")
	ManageUser(c)
	return recorder
}

func TestManageUserDisableAdvancesAuthVersionOnceAndRevokesSession(t *testing.T) {
	db := setupManageUserTestDB(t)
	now := time.Now().Unix()
	user := model.User{
		Username: "managed-disable-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.UserSession{
		SID: "managed-disable-session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: model.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"disable"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, updated.Status)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var session model.UserSession
	require.NoError(t, db.First(&session, "sid = ?", "managed-disable-session").Error)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
}

func TestManageUserDemoteAdvancesAuthVersionAndRevokesSessionsOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	require.NoError(t, authz.Init(db))

	now := time.Now().Unix()
	user := model.User{
		Username: "managed-demote-user", Password: "password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	for _, sid := range []string{"managed-demote-session-one", "managed-demote-session-two"} {
		require.NoError(t, db.Create(&model.UserSession{
			SID: sid, UserID: user.Id, Version: 1, UserAuthVersion: 1,
			Status: model.UserSessionStatusActive, RefreshHash: "refresh-" + sid, LoginMethod: "password",
			LastActiveAt: now, ExpiresAt: now + 3600,
		}).Error)
	}

	sessionUpdateCount := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:count_demote_session_updates", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "user_sessions" {
			sessionUpdateCount++
		}
	}))

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"demote"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.RoleCommonUser, updated.Role)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var sessions []model.UserSession
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("sid asc").Find(&sessions).Error)
	require.Len(t, sessions, 2)
	for _, session := range sessions {
		assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
		assert.Equal(t, "admin_demote", session.RevokedReason)
	}
	assert.Equal(t, 1, sessionUpdateCount)
}

func TestManageUserDeleteReturnsImmediatelyAndUnknownActionFails(t *testing.T) {
	db := setupManageUserTestDB(t)
	deleted := model.User{
		Username: "managed-delete-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "delete-aff",
	}
	require.NoError(t, db.Create(&deleted).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"delete"}`, deleted.Id))
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	var deletedCount int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("id = ? AND deleted_at IS NOT NULL", deleted.Id).Count(&deletedCount).Error)
	assert.EqualValues(t, 1, deletedCount)

	unchanged := model.User{
		Username: "managed-unknown-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "unknown-aff",
	}
	require.NoError(t, db.Create(&unchanged).Error)
	recorder = performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"unknown"}`, unchanged.Id))
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	require.NoError(t, db.First(&unchanged, unchanged.Id).Error)
	assert.EqualValues(t, 1, unchanged.AuthVersion)
	assert.Equal(t, common.UserStatusEnabled, unchanged.Status)
}

func TestManageUserQuotaReplayDoesNotDuplicateManageAudit(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 9999, Username: "root-operator", Password: "password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "root-operator-quota-aff",
	}).Error)
	user := model.User{
		Username: "managed-quota-replay-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 100,
	}
	require.NoError(t, db.Create(&user).Error)
	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":25,"remark":"verified correction","idempotency_key":"manage-quota-replay"}`, user.Id)

	first := performManageUserRequest(t, body)
	assert.Equal(t, http.StatusOK, first.Code)
	assert.Contains(t, first.Body.String(), `"success":true`)
	assert.Contains(t, first.Body.String(), `"replayed":false`)

	second := performManageUserRequest(t, body)
	assert.Equal(t, http.StatusOK, second.Code)
	assert.Contains(t, second.Body.String(), `"success":true`)
	assert.Contains(t, second.Body.String(), `"replayed":true`)

	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 125, user.Quota)
	var fundCount int64
	require.NoError(t, db.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", "admin_quota:manage-quota-replay").
		Count(&fundCount).Error)
	assert.Equal(t, int64(1), fundCount)
	var auditLogs []model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", 9999, model.LogTypeManage).Find(&auditLogs).Error)
	require.Len(t, auditLogs, 1)
	var other map[string]interface{}
	require.NoError(t, common.Unmarshal([]byte(auditLogs[0].Other), &other))
	op, ok := other["op"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "user.quota_add", op["action"])
}

func TestManageUserQuotaRequiresIdempotencyKey(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-missing-key", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 100,
	}
	require.NoError(t, db.Create(&user).Error)
	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":25,"remark":"verified correction"}`, user.Id)

	response := performManageUserRequest(t, body)
	assert.Contains(t, response.Body.String(), `"success":false`)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	var fundCount int64
	require.NoError(t, db.Model(&model.PromotionFundTransaction{}).Count(&fundCount).Error)
	assert.Zero(t, fundCount)
}

func TestManageUserQuotaRespectsWalletCeiling(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: common.MaxQuota - 1,
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":2,"remark":"ceiling probe","idempotency_key":"quota-ceiling-add"}`, user.Id))
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.MaxQuota-1, updated.Quota)

	recorder = performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"override","value":%d,"remark":"ceiling probe","idempotency_key":"quota-ceiling-override"}`, user.Id, common.MaxQuota+1))
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.MaxQuota-1, updated.Quota)
}
