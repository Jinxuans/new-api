package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeBillingReservesCumulativeUsageAndSettlesOnceAfterRefundHold(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	configureRealtimeTestRatios(t)

	const (
		userID   = 705
		tokenID  = 705
		tokenKey = "realtime-regression-token"
	)
	startingQuota := common.GetTrustQuota() + 100
	seedUser(t, userID, startingQuota)
	seedToken(t, tokenID, userID, tokenKey, startingQuota)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", startingQuota)
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        tokenKey,
		OriginModelName: "realtime-regression-model",
		UsingGroup:      "realtime-regression-group",
		UserGroup:       "realtime-regression-user-group",
		RelayFormat:     relaytypes.RelayFormatOpenAIRealtime,
		UserSetting: dto.UserSetting{
			BillingPreference:     "wallet_only",
			QuotaWarningThreshold: 1,
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 5, relayInfo))
	assert.Equal(t, 5, relayInfo.Billing.GetPreConsumedQuota())
	assertRealtimeBalances(t, userID, tokenID, startingQuota-5, startingQuota-5, 5)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("refund_hold", true).Error)

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens: 10,
		InputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 10,
		},
	}))
	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens: 30,
		InputTokens: 30,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 30,
		},
	}))

	assert.Equal(t, 30, relayInfo.Billing.GetPreConsumedQuota())
	assert.Equal(t, 30, relayInfo.FinalPreConsumedQuota)
	assertRealtimeBalances(t, userID, tokenID, startingQuota-30, startingQuota-30, 30)

	require.NoError(t, SettleBilling(ctx, relayInfo, 30))
	assertRealtimeBalances(t, userID, tokenID, startingQuota-30, startingQuota-30, 30)

	require.ErrorIs(t, model.DecreaseUserQuota(userID, 1, false), model.ErrUserRefundHeld)
}

func TestRealtimeBillingStopsWhenCumulativeUsageExceedsWalletQuota(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	configureRealtimeTestRatios(t)

	const (
		userID   = 706
		tokenID  = 706
		tokenKey = "realtime-limited-token"
	)
	seedUser(t, userID, 25)
	seedToken(t, tokenID, userID, tokenKey, 100)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        tokenKey,
		OriginModelName: "realtime-regression-model",
		UsingGroup:      "realtime-regression-group",
		UserGroup:       "realtime-regression-user-group",
		RelayFormat:     relaytypes.RelayFormatOpenAIRealtime,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 5, relayInfo))
	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens: 20,
		InputTokens: 20,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 20,
		},
	}))

	err := PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens: 30,
		InputTokens: 30,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 30,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user quota is not enough")
	assert.Equal(t, 20, relayInfo.Billing.GetPreConsumedQuota())
	assertRealtimeBalances(t, userID, tokenID, 5, 80, 20)
}

func TestTieredRealtimeBillingReservesCumulativeExpressionQuota(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	configureRealtimeTestRatios(t)

	const (
		userID   = 707
		tokenID  = 707
		tokenKey = "tiered-realtime-token"
		expr     = `tier("base", p + c * 2)`
	)
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, tokenKey, 100)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        tokenKey,
		OriginModelName: "tiered-realtime-model",
		UsingGroup:      "realtime-regression-group",
		UserGroup:       "realtime-regression-user-group",
		RelayFormat:     relaytypes.RelayFormatOpenAIRealtime,
		UserSetting: dto.UserSetting{
			BillingPreference:     "wallet_only",
			QuotaWarningThreshold: 1,
		},
		PriceData: hosttypes.PriceData{
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ModelName:                 "tiered-realtime-model",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 5,
			EstimatedQuotaAfterGroup:  5,
			EstimatedTier:             "base",
			QuotaPerUnit:              1_000_000,
			ExprVersion:               billingexpr.ExprVersion(expr),
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 5, relayInfo))

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens:  15,
		InputTokens:  10,
		OutputTokens: 5,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 10,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens: 5,
		},
	}))
	assert.Equal(t, 20, relayInfo.Billing.GetPreConsumedQuota())
	assertRealtimeBalances(t, userID, tokenID, 80, 80, 20)

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens:  30,
		InputTokens:  20,
		OutputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 20,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens: 10,
		},
	}))
	assert.Equal(t, 40, relayInfo.Billing.GetPreConsumedQuota())
	assertRealtimeBalances(t, userID, tokenID, 60, 60, 40)

	require.NoError(t, SettleBilling(ctx, relayInfo, 40))
	assertRealtimeBalances(t, userID, tokenID, 60, 60, 40)
}

func configureRealtimeTestRatios(t *testing.T) {
	t.Helper()
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"realtime-regression-model":1,"tiered-realtime-model":0}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"realtime-regression-group":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
}

func assertRealtimeBalances(t *testing.T, userID, tokenID, userQuota, tokenQuota, tokenUsed int) {
	t.Helper()
	actualUserQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, userQuota, actualUserQuota)

	token, err := model.GetTokenById(tokenID)
	require.NoError(t, err)
	assert.Equal(t, tokenQuota, token.RemainQuota)
	assert.Equal(t, tokenUsed, token.UsedQuota)
}
