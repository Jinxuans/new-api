package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRegisterRequestInvitationCodeAcceptsAff(t *testing.T) {
	var request RegisterRequest
	require.NoError(t, common.Unmarshal([]byte(`{"aff":" bY5w "}`), &request))

	require.Equal(t, "bY5w", request.InvitationCode())
}

func TestRegisterRequestInvitationCodeFallsBackToAffCode(t *testing.T) {
	var request RegisterRequest
	require.NoError(t, common.Unmarshal([]byte(`{"aff_code":"legacy"}`), &request))

	require.Equal(t, "legacy", request.InvitationCode())
}

func TestRegisterBindsInviterFromAffPayload(t *testing.T) {
	setupRegisterControllerTestDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Username:    "inviter",
		Password:    "hashed-password",
		DisplayName: "inviter",
		AffCode:     "bY5w",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}).Error)

	body := bytes.NewBufferString(`{"username":"invitee","password":"password123","aff":"bY5w"}`)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", body)

	Register(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)

	var invitee model.User
	require.NoError(t, model.DB.Where("username = ?", "invitee").First(&invitee).Error)
	require.Equal(t, 1, invitee.InviterId)
}

func setupRegisterControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalGenerateDefaultToken := constant.GenerateDefaultToken

	common.RedisEnabled = false
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	constant.GenerateDefaultToken = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Log{},
		&model.InvitationReward{},
		&model.PromotionEvent{},
		&model.PromotionFundTransaction{},
		&model.PromotionFundTransactionLeg{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
		constant.GenerateDefaultToken = originalGenerateDefaultToken
	})

	return db
}
