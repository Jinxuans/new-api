package service

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViolationFeeChargesAuthorizedRequestAfterRefundHold(t *testing.T) {
	truncate(t)

	settings := model_setting.GetGrokSettings()
	previousEnabled := settings.ViolationDeductionEnabled
	previousAmount := settings.ViolationDeductionAmount
	settings.ViolationDeductionEnabled = true
	settings.ViolationDeductionAmount = 0.01
	t.Cleanup(func() {
		settings.ViolationDeductionEnabled = previousEnabled
		settings.ViolationDeductionAmount = previousAmount
	})

	const userID = 705
	const initialQuota = 1_000_000
	seedUser(t, userID, initialQuota)
	seedChannel(t, userID)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("refund_hold", true).Error)
	relayInfo := &relaycommon.RelayInfo{
		UserId:       userID,
		UserQuota:    initialQuota,
		IsPlayground: true,
		StartTime:    time.Now(),
		ChannelMeta:  &relaycommon.ChannelMeta{ChannelId: userID},
		PriceData: hosttypes.PriceData{
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	apiErr := relaytypes.NewError(errors.New(CSAMViolationMarker), relaytypes.ErrorCodeViolationFeeGrokCSAM)
	ctx, _ := gin.CreateTestContext(nil)
	expectedFee, clamp := calcViolationFeeQuota(settings.ViolationDeductionAmount, 1)
	require.Nil(t, clamp)

	assert.True(t, ChargeViolationFeeIfNeeded(ctx, relayInfo, apiErr))
	assert.Equal(t, initialQuota-expectedFee, getUserQuota(t, userID))
	require.ErrorIs(t, model.DecreaseUserQuota(userID, 1, false), model.ErrUserRefundHeld)
}

func TestCalcViolationFeeQuotaSaturatesOversizedConfiguration(t *testing.T) {
	quota, clamp := calcViolationFeeQuota(math.MaxFloat64, math.MaxFloat64)

	assert.Equal(t, common.MaxQuota, quota)
	require.NotNil(t, clamp)
	assert.Equal(t, common.QuotaClampOverflow, clamp.Kind)
}
