package helper

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func ApplyImageBilling(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info == nil || !info.PriceData.UsePrice {
		return nil
	}

	rule, _, ok := billing_setting.GetImageBillingRule(info.OriginModelName)
	if !isImageRelayMode(info.RelayMode) && !ok {
		return nil
	}

	addImageCountRatioFromRequest(info, request)

	if ok {
		body, err := resolveImageBillingBody(c, request)
		if err != nil {
			return err
		}
		if err := applyConfiguredImageBillingRule(info, rule, body); err != nil {
			return err
		}
	}

	return applyOtherRatiosToImagePreConsume(info)
}

func isImageRelayMode(relayMode int) bool {
	return relayMode == relayconstant.RelayModeImagesGenerations ||
		relayMode == relayconstant.RelayModeImagesEdits
}

func addImageCountRatioFromRequest(info *relaycommon.RelayInfo, request dto.Request) {
	imageRequest, ok := request.(*dto.ImageRequest)
	if !ok || imageRequest == nil || imageRequest.N == nil || *imageRequest.N == 0 {
		return
	}
	info.PriceData.AddOtherRatio("n", float64(*imageRequest.N))
}

func resolveImageBillingBody(c *gin.Context, request dto.Request) ([]byte, error) {
	if c != nil && c.Request != nil && isJSONContentType(c.Request.Header.Get("Content-Type")) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, err
		}
		body, err := storage.Bytes()
		if err != nil {
			return nil, err
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return nil, seekErr
		}
		if gjson.ValidBytes(body) {
			return body, nil
		}
	}
	if request == nil {
		return nil, nil
	}
	body, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func applyConfiguredImageBillingRule(info *relaycommon.RelayInfo, rule billing_setting.ImageBillingRule, body []byte) error {
	if ratio, ok, err := resolveImageSizeRatio(rule, body); err != nil {
		return err
	} else if ok {
		info.PriceData.AddOtherRatio("image_size", ratio)
	}

	if ratio, ok, err := resolveNamedImageRatio(
		"quality",
		bodyStringAtPath(body, rule.QualityPath),
		rule.DefaultQuality,
		rule.QualityRatios,
		nil,
		rule.UnknownPolicy,
	); err != nil {
		return err
	} else if ok {
		info.PriceData.AddOtherRatio("image_quality", ratio)
	}

	return nil
}

func resolveImageSizeRatio(rule billing_setting.ImageBillingRule, body []byte) (float64, bool, error) {
	tierValue := bodyStringAtPath(body, rule.SizeTierPath)
	if tierValue != "" {
		return resolveNamedImageRatio(
			"size",
			tierValue,
			rule.DefaultSize,
			rule.SizeRatios,
			rule.ResolutionTiers,
			rule.UnknownPolicy,
		)
	}

	sizeValue := bodyStringAtPath(body, rule.SizePath)
	if sizeValue != "" {
		if ratio, ok := lookupImageRatio(rule.SizeRatios, sizeValue); ok {
			return ratio, true, nil
		}
		if ratio, ok := matchResolutionTierRatio(rule.ResolutionTiers, sizeValue); ok {
			return ratio, true, nil
		}
		return resolveNamedImageRatio(
			"size",
			sizeValue,
			rule.DefaultSize,
			rule.SizeRatios,
			rule.ResolutionTiers,
			rule.UnknownPolicy,
		)
	}

	return resolveNamedImageRatio(
		"size",
		"",
		rule.DefaultSize,
		rule.SizeRatios,
		rule.ResolutionTiers,
		rule.UnknownPolicy,
	)
}

func resolveNamedImageRatio(
	label string,
	value string,
	defaultValue string,
	ratios map[string]float64,
	tiers []billing_setting.ImageBillingResolutionTier,
	unknownPolicy string,
) (float64, bool, error) {
	value = strings.TrimSpace(value)
	defaultValue = strings.TrimSpace(defaultValue)

	if value != "" {
		if ratio, ok := lookupImageRatio(ratios, value); ok {
			return ratio, true, nil
		}
		if ratio, ok := lookupResolutionTierByName(tiers, value); ok {
			return ratio, true, nil
		}
	}

	switch normalizeImageBillingUnknownPolicy(unknownPolicy) {
	case billing_setting.ImageBillingUnknownReject:
		if value == "" {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("image billing %s value %s is not configured", label, value)
	case billing_setting.ImageBillingUnknownHighest:
		return highestImageRatio(ratios, tiers)
	case billing_setting.ImageBillingUnknownBase:
		return 0, false, nil
	default:
		if defaultValue == "" {
			return 0, false, nil
		}
		if ratio, ok := lookupImageRatio(ratios, defaultValue); ok {
			return ratio, true, nil
		}
		if ratio, ok := lookupResolutionTierByName(tiers, defaultValue); ok {
			return ratio, true, nil
		}
		if value != "" {
			return 0, false, fmt.Errorf("image billing %s default %s is not configured", label, defaultValue)
		}
		return 0, false, nil
	}
}

func bodyStringAtPath(body []byte, path string) string {
	path = strings.TrimSpace(path)
	if len(body) == 0 || path == "" {
		return ""
	}
	result := gjson.GetBytes(body, path)
	if !result.Exists() || result.Type == gjson.Null {
		return ""
	}
	return strings.TrimSpace(result.String())
}

func lookupImageRatio(ratios map[string]float64, key string) (float64, bool) {
	if len(ratios) == 0 {
		return 0, false
	}
	key = strings.TrimSpace(key)
	if ratio, ok := ratios[key]; ok && ratio > 0 {
		return ratio, true
	}
	return 0, false
}

func matchResolutionTierRatio(tiers []billing_setting.ImageBillingResolutionTier, sizeValue string) (float64, bool) {
	width, height, ok := parseImageSize(sizeValue)
	if !ok {
		return 0, false
	}
	longEdge := width
	if height > longEdge {
		longEdge = height
	}
	pixels := width * height
	for _, tier := range tiers {
		if tier.Ratio <= 0 {
			continue
		}
		if tier.MaxLongEdge > 0 && longEdge > tier.MaxLongEdge {
			continue
		}
		if tier.MaxPixels > 0 && pixels > tier.MaxPixels {
			continue
		}
		return tier.Ratio, true
	}
	return 0, false
}

func parseImageSize(sizeValue string) (int, int, bool) {
	normalized := strings.ToLower(strings.TrimSpace(sizeValue))
	normalized = strings.NewReplacer(" ", "", "*", "x").Replace(normalized)
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func lookupResolutionTierByName(tiers []billing_setting.ImageBillingResolutionTier, name string) (float64, bool) {
	name = strings.TrimSpace(name)
	for _, tier := range tiers {
		if tier.Name == name && tier.Ratio > 0 {
			return tier.Ratio, true
		}
	}
	return 0, false
}

func highestImageRatio(ratios map[string]float64, tiers []billing_setting.ImageBillingResolutionTier) (float64, bool, error) {
	highest := 0.0
	for _, ratio := range ratios {
		if ratio > highest {
			highest = ratio
		}
	}
	for _, tier := range tiers {
		if tier.Ratio > highest {
			highest = tier.Ratio
		}
	}
	if highest <= 0 {
		return 0, false, nil
	}
	return highest, true, nil
}

func normalizeImageBillingUnknownPolicy(policy string) string {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		return billing_setting.ImageBillingUnknownDefault
	}
	return policy
}

func applyOtherRatiosToImagePreConsume(info *relaycommon.RelayInfo) error {
	baseQuota := info.PriceData.ApplyOtherRatiosToFloat(info.PriceData.ModelPrice * common.QuotaPerUnit)
	quota, err := common.QuotaFromFloatStrict(baseQuota * info.PriceData.GroupRatioInfo.GroupRatio)
	if err != nil {
		return err
	}
	info.PriceData.QuotaToPreConsumeBeforeGroup = baseQuota
	info.PriceData.QuotaToPreConsume = quota
	return nil
}
