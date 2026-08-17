package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func restoreImageBillingRules(t *testing.T, rules map[string]billing_setting.ImageBillingRule) {
	t.Helper()
	data, err := common.Marshal(rules)
	require.NoError(t, err)
	require.NoError(t, billing_setting.UpdateImageBillingRulesByJSONString(string(data)))
}

func testImageBillingContext(body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestApplyImageBillingUsesConfiguredResolutionQualityAndCount(t *testing.T) {
	originalRules := billing_setting.GetImageBillingRulesCopy()
	defer restoreImageBillingRules(t, originalRules)

	require.NoError(t, billing_setting.UpdateImageBillingRulesByJSONString(`{
		"image-test": {
			"enabled": true,
			"size_path": "size",
			"quality_path": "quality",
			"quality_ratios": {
				"standard": 1,
				"high": 1.5
			},
			"resolution_tiers": [
				{"name": "1K", "max_long_edge": 1024, "ratio": 1},
				{"name": "2K", "max_long_edge": 2048, "ratio": 2},
				{"name": "4K", "max_long_edge": 4096, "ratio": 4}
			]
		}
	}`))

	n := uint(2)
	request := &dto.ImageRequest{
		Model:   "image-test",
		Prompt:  "draw",
		N:       &n,
		Size:    "2048x1152",
		Quality: "high",
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "image-test",
		RelayMode:       relayconstant.RelayModeImagesGenerations,
		PriceData: types.PriceData{
			UsePrice:   true,
			ModelPrice: 1000 / common.QuotaPerUnit,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
	c := testImageBillingContext(`{"model":"image-test","prompt":"draw","n":2,"size":"2048x1152","quality":"high"}`)

	require.NoError(t, ApplyImageBilling(c, info, request))
	require.Equal(t, 6000, info.PriceData.QuotaToPreConsume)
	otherRatios := info.PriceData.OtherRatios()
	require.Equal(t, 2.0, otherRatios["n"])
	require.Equal(t, 2.0, otherRatios["image_size"])
	require.Equal(t, 1.5, otherRatios["image_quality"])
}

func TestApplyImageBillingWithoutConfiguredRuleDoesNotTierSizeOrQuality(t *testing.T) {
	originalRules := billing_setting.GetImageBillingRulesCopy()
	defer restoreImageBillingRules(t, originalRules)
	require.NoError(t, billing_setting.UpdateImageBillingRulesByJSONString(`{}`))

	n := uint(1)
	request := &dto.ImageRequest{
		Model:   "plain-image-test",
		Prompt:  "draw",
		N:       &n,
		Size:    "4096x4096",
		Quality: "high",
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "plain-image-test",
		RelayMode:       relayconstant.RelayModeImagesGenerations,
		PriceData: types.PriceData{
			UsePrice:   true,
			ModelPrice: 1000 / common.QuotaPerUnit,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
	c := testImageBillingContext(`{"model":"plain-image-test","prompt":"draw","n":1,"size":"4096x4096","quality":"high"}`)

	require.NoError(t, ApplyImageBilling(c, info, request))
	require.Equal(t, 1000, info.PriceData.QuotaToPreConsume)
	otherRatios := info.PriceData.OtherRatios()
	require.Equal(t, 1.0, otherRatios["n"])
	require.NotContains(t, otherRatios, "image_size")
	require.NotContains(t, otherRatios, "image_quality")
}

func TestApplyImageBillingUsesConfiguredRuleForGeminiNativeImageModel(t *testing.T) {
	originalRules := billing_setting.GetImageBillingRulesCopy()
	defer restoreImageBillingRules(t, originalRules)

	require.NoError(t, billing_setting.UpdateImageBillingRulesByJSONString(`{
		"gemini-3-pro-image-preview": {
			"enabled": true,
			"size_tier_path": "generationConfig.responseFormat.image.imageSize",
			"default_size": "1K",
			"size_ratios": {
				"1K": 1,
				"2K": 1,
				"4K": 1.791045
			}
		}
	}`))

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-pro-image-preview",
		RelayMode:       relayconstant.RelayModeGemini,
		PriceData: types.PriceData{
			UsePrice:   true,
			ModelPrice: 1000 / common.QuotaPerUnit,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
	c := testImageBillingContext(`{
		"contents": [
			{"role": "user", "parts": [{"text": "draw"}]}
		],
		"generationConfig": {
			"responseModalities": ["TEXT", "IMAGE"],
			"responseFormat": {
				"image": {
					"imageSize": "4K"
				}
			}
		}
	}`)

	require.NoError(t, ApplyImageBilling(c, info, nil))
	require.Equal(t, 1791, info.PriceData.QuotaToPreConsume)
	otherRatios := info.PriceData.OtherRatios()
	require.InDelta(t, 1.791045, otherRatios["image_size"], 0.000001)
}
