package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareBillingForSelectedGroupReservesStandardEstimate(t *testing.T) {
	billing := &recordingBillingSettler{preConsumedQuota: 50}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50,
		PriceData: types.PriceData{
			QuotaToPreConsume:            50,
			QuotaToPreConsumeBeforeGroup: 500,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 0.2,
			},
		},
	}

	require.Nil(t, PrepareBillingForSelectedGroup(nil, relayInfo))
	assert.Equal(t, []int{100}, billing.reserveTargets)
	assert.Equal(t, 100, relayInfo.PriceData.QuotaToPreConsume)
	assert.Equal(t, 100, relayInfo.FinalPreConsumedQuota)
}

func TestPrepareBillingForSelectedGroupStartsStandardBillingAfterFreeGroup(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	const userID = 705
	seedUser(t, userID, 1_000)
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		OriginModelName: "standard-test",
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
		PriceData: types.PriceData{
			FreeModel:                    true,
			QuotaToPreConsumeBeforeGroup: 100,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 2,
			},
		},
	}
	ctx, _ := gin.CreateTestContext(nil)

	require.Nil(t, PrepareBillingForSelectedGroup(ctx, relayInfo))
	require.NotNil(t, relayInfo.Billing)
	assert.False(t, relayInfo.PriceData.FreeModel)
	assert.Equal(t, 200, relayInfo.FinalPreConsumedQuota)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, 800, userQuota)
}

func TestSettleBillingLazyChargeUsesSubscriptionPreference(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	const (
		userID         = 706
		planID         = 1706
		subscriptionID = 2706
	)
	seedUser(t, userID, 1_000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          planID,
		Title:       "settlement plan",
		TotalAmount: 1_000,
		Enabled:     true,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:          subscriptionID,
		UserId:      userID,
		PlanId:      planID,
		AmountTotal: 1_000,
		Status:      "active",
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
	}).Error)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "lazy-subscription-charge",
		UserId:          userID,
		UserQuota:       1_000,
		IsPlayground:    true,
		OriginModelName: "free-with-tool-surcharge",
		UsingGroup:      "default",
		UserSetting: dto.UserSetting{
			BillingPreference: "subscription_first",
		},
	}
	ctx, _ := gin.CreateTestContext(nil)

	require.NoError(t, SettleBilling(ctx, relayInfo, 100))
	assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
	assert.Equal(t, subscriptionID, relayInfo.SubscriptionId)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, 1_000, userQuota)
	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	assert.Equal(t, int64(100), subscription.AmountUsed)
}
