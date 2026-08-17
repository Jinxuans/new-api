package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	maxGrowthRewardItemDescriptionLength  = 4000
	maxGrowthRewardItemIntroductionLength = 10000
	maxGrowthRewardItemActionURLLength    = 2048
	maxGrowthRewardItemDailyLimit         = 1000
)

type AdminGrowthRewardItemCapabilities struct {
	RewardOverride bool `json:"reward_override"`
	ActionURL      bool `json:"action_url"`
	Introduction   bool `json:"introduction"`
	ClaimPassword  bool `json:"claim_password"`
	DailyLimit     bool `json:"daily_limit"`
}

type AdminGrowthRewardItem struct {
	Id                      int                               `json:"id"`
	Code                    string                            `json:"code"`
	Title                   string                            `json:"title"`
	Description             string                            `json:"description"`
	Introduction            string                            `json:"introduction,omitempty"`
	RewardQuota             int                               `json:"reward_quota"`
	EffectiveRewardQuotaMin int                               `json:"effective_reward_quota_min"`
	EffectiveRewardQuotaMax int                               `json:"effective_reward_quota_max"`
	RewardSource            string                            `json:"reward_source"`
	ItemType                string                            `json:"item_type"`
	ActionURL               string                            `json:"action_url,omitempty"`
	Enabled                 bool                              `json:"enabled"`
	EffectiveEnabled        bool                              `json:"effective_enabled"`
	OncePerUser             bool                              `json:"once_per_user"`
	DailyLimit              int                               `json:"daily_limit"`
	BuiltIn                 bool                              `json:"built_in"`
	ClaimPasswordConfigured bool                              `json:"claim_password_configured"`
	Capabilities            AdminGrowthRewardItemCapabilities `json:"capabilities"`
}

type AdminGrowthRewardItemCreateRequest struct {
	Code          string  `json:"code"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Introduction  string  `json:"introduction"`
	RewardQuota   int     `json:"reward_quota"`
	ItemType      string  `json:"item_type"`
	ActionURL     string  `json:"action_url"`
	ClaimPassword *string `json:"claim_password"`
	Enabled       bool    `json:"enabled"`
	OncePerUser   bool    `json:"once_per_user"`
	DailyLimit    int     `json:"daily_limit"`
}

type AdminGrowthRewardItemUpdateRequest struct {
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Introduction  string  `json:"introduction"`
	RewardQuota   int     `json:"reward_quota"`
	ActionURL     string  `json:"action_url"`
	ClaimPassword *string `json:"claim_password"`
	Enabled       bool    `json:"enabled"`
	DailyLimit    int     `json:"daily_limit"`
}

type AdminGrowthSubmission struct {
	*model.GrowthSubmission
	ItemTitle      string `json:"item_title"`
	RewardQuotaMin int    `json:"reward_quota_min"`
	RewardQuotaMax int    `json:"reward_quota_max"`
}

func ListAdminGrowthRewardItems() ([]*AdminGrowthRewardItem, error) {
	var items []*model.GrowthRewardItem
	if err := model.DB.Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}

	result := make([]*AdminGrowthRewardItem, 0, len(items))
	for _, item := range items {
		result = append(result, newAdminGrowthRewardItem(item))
	}
	return result, nil
}

func CreateAdminGrowthRewardItem(req AdminGrowthRewardItemCreateRequest) (*AdminGrowthRewardItem, error) {
	item := &model.GrowthRewardItem{
		Code:         strings.TrimSpace(req.Code),
		Title:        strings.TrimSpace(req.Title),
		Description:  strings.TrimSpace(req.Description),
		Introduction: strings.TrimSpace(req.Introduction),
		RewardQuota:  req.RewardQuota,
		ItemType:     strings.TrimSpace(req.ItemType),
		ActionURL:    strings.TrimSpace(req.ActionURL),
		Enabled:      req.Enabled,
		OncePerUser:  req.OncePerUser,
		DailyLimit:   req.DailyLimit,
	}
	if item.ItemType != model.GrowthRewardItemTypeManual && item.ItemType != model.GrowthRewardItemTypeSemiAuto {
		return nil, errors.New("custom reward items must use manual or semi_auto type")
	}
	if req.ClaimPassword != nil {
		item.ClaimPassword = strings.TrimSpace(*req.ClaimPassword)
	}
	capabilities := growthRewardItemCapabilities(item)
	if item.ItemType == model.GrowthRewardItemTypeManual || item.ItemType == model.GrowthRewardItemTypeSemiAuto {
		item.OncePerUser = false
	} else {
		item.DailyLimit = 0
	}
	if !capabilities.ActionURL {
		item.ActionURL = ""
	}
	if !capabilities.Introduction {
		item.Introduction = ""
	}
	if !capabilities.ClaimPassword {
		item.ClaimPassword = ""
	}
	if !capabilities.RewardOverride {
		item.RewardQuota = 0
	}
	if err := validateAdminGrowthRewardItem(item, capabilities); err != nil {
		return nil, err
	}
	if err := model.DB.Create(item).Error; err != nil {
		return nil, err
	}
	return newAdminGrowthRewardItem(item), nil
}

func UpdateAdminGrowthRewardItem(id int, req AdminGrowthRewardItemUpdateRequest) (*AdminGrowthRewardItem, error) {
	if id <= 0 {
		return nil, errors.New("invalid reward item")
	}
	var item model.GrowthRewardItem
	if err := model.DB.Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}

	capabilities := growthRewardItemCapabilities(&item)
	item.Title = strings.TrimSpace(req.Title)
	item.Description = strings.TrimSpace(req.Description)
	item.Enabled = req.Enabled
	if capabilities.RewardOverride {
		item.RewardQuota = req.RewardQuota
	}
	if capabilities.Introduction {
		item.Introduction = strings.TrimSpace(req.Introduction)
	}
	if capabilities.ActionURL {
		item.ActionURL = strings.TrimSpace(req.ActionURL)
	}
	if capabilities.ClaimPassword && req.ClaimPassword != nil {
		item.ClaimPassword = strings.TrimSpace(*req.ClaimPassword)
	}
	if capabilities.DailyLimit {
		item.DailyLimit = req.DailyLimit
	}
	if err := validateAdminGrowthRewardItem(&item, capabilities); err != nil {
		return nil, err
	}
	if err := model.DB.Save(&item).Error; err != nil {
		return nil, err
	}
	return newAdminGrowthRewardItem(&item), nil
}

func ListAdminGrowthSubmissions(pageInfo *common.PageInfo, status string) ([]*AdminGrowthSubmission, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
	status = strings.TrimSpace(status)
	if err := validateGrowthSubmissionStatusFilter(status); err != nil {
		return nil, 0, err
	}

	query := model.DB.Model(&model.GrowthSubmission{})
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var submissions []*model.GrowthSubmission
	if err := query.Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&submissions).Error; err != nil {
		return nil, 0, err
	}

	itemCodes := make([]string, 0, len(submissions))
	for _, submission := range submissions {
		itemCodes = append(itemCodes, submission.ItemCode)
	}
	itemsByCode := make(map[string]*model.GrowthRewardItem, len(itemCodes))
	if len(itemCodes) > 0 {
		var items []*model.GrowthRewardItem
		if err := model.DB.Where("code IN ?", itemCodes).Find(&items).Error; err != nil {
			return nil, 0, err
		}
		for _, item := range items {
			itemsByCode[item.Code] = item
		}
	}

	result := make([]*AdminGrowthSubmission, 0, len(submissions))
	for _, submission := range submissions {
		row := &AdminGrowthSubmission{GrowthSubmission: submission}
		if item := itemsByCode[submission.ItemCode]; item != nil {
			row.ItemTitle = item.Title
			row.RewardQuotaMin, row.RewardQuotaMax = resolveGrowthRewardQuotaRange(item)
		}
		result = append(result, row)
	}
	return result, total, nil
}

func ListAdminPromotionWithdrawals(pageInfo *common.PageInfo, status string) ([]*model.PromotionWithdrawal, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
	status = strings.TrimSpace(status)
	if err := validatePromotionWithdrawalStatusFilter(status); err != nil {
		return nil, 0, err
	}

	return model.ListAdminPromotionWithdrawals(pageInfo, status)
}

func newAdminGrowthRewardItem(item *model.GrowthRewardItem) *AdminGrowthRewardItem {
	if item == nil {
		return nil
	}
	capabilities := growthRewardItemCapabilities(item)
	minRewardQuota, maxRewardQuota := resolveGrowthRewardQuotaRange(item)
	rewardSource := "reward_program"
	if capabilities.RewardOverride && item.RewardQuota > 0 {
		rewardSource = "item_override"
	}
	return &AdminGrowthRewardItem{
		Id:                      item.Id,
		Code:                    item.Code,
		Title:                   item.Title,
		Description:             item.Description,
		Introduction:            item.Introduction,
		RewardQuota:             item.RewardQuota,
		EffectiveRewardQuotaMin: minRewardQuota,
		EffectiveRewardQuotaMax: maxRewardQuota,
		RewardSource:            rewardSource,
		ItemType:                item.ItemType,
		ActionURL:               item.ActionURL,
		Enabled:                 item.Enabled,
		EffectiveEnabled:        !shouldHideGrowthRewardItem(item, operation_setting.GetGrowthSetting()),
		OncePerUser:             item.OncePerUser,
		DailyLimit:              item.DailyLimit,
		BuiltIn:                 isBuiltInGrowthRewardItem(item.Code),
		ClaimPasswordConfigured: item.ClaimPassword != "",
		Capabilities:            capabilities,
	}
}

func growthRewardItemCapabilities(item *model.GrowthRewardItem) AdminGrowthRewardItemCapabilities {
	if item == nil {
		return AdminGrowthRewardItemCapabilities{}
	}
	isSubmissionItem := item.ItemType == model.GrowthRewardItemTypeManual || item.ItemType == model.GrowthRewardItemTypeSemiAuto
	return AdminGrowthRewardItemCapabilities{
		RewardOverride: item.Code != model.GrowthRewardItemDailyCheckin,
		ActionURL:      item.Code == model.GrowthRewardItemJoinCommunity,
		Introduction:   isSubmissionItem,
		ClaimPassword:  item.Code == model.GrowthRewardItemJoinCommunity,
		DailyLimit:     isSubmissionItem,
	}
}

func isBuiltInGrowthRewardItem(code string) bool {
	switch code {
	case model.GrowthRewardItemDailyCheckin,
		model.GrowthRewardItemCreateFirstAPIKey,
		model.GrowthRewardItemFirstAPIRequest,
		model.GrowthRewardItemFirstTopUp,
		model.GrowthRewardItemThreeDayUsage,
		model.GrowthRewardItemMonthlySpendTarget,
		model.GrowthRewardItemJoinCommunity,
		model.GrowthRewardItemContentPublish,
		model.GrowthRewardItemBacklinkSubmission:
		return true
	default:
		return false
	}
}

func validateAdminGrowthRewardItem(item *model.GrowthRewardItem, capabilities AdminGrowthRewardItemCapabilities) error {
	if item == nil {
		return errors.New("reward item is required")
	}
	if item.Code == "" || utf8.RuneCountInString(item.Code) > 64 || strings.IndexFunc(item.Code, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_'
	}) >= 0 {
		return errors.New("reward item code must contain 1 to 64 lowercase letters, numbers, or underscores")
	}
	if item.Title == "" || utf8.RuneCountInString(item.Title) > 128 {
		return errors.New("reward item title must contain 1 to 128 characters")
	}
	if utf8.RuneCountInString(item.Description) > maxGrowthRewardItemDescriptionLength {
		return fmt.Errorf("reward item description cannot exceed %d characters", maxGrowthRewardItemDescriptionLength)
	}
	if capabilities.Introduction && utf8.RuneCountInString(item.Introduction) > maxGrowthRewardItemIntroductionLength {
		return fmt.Errorf("reward item introduction cannot exceed %d characters", maxGrowthRewardItemIntroductionLength)
	}
	if capabilities.RewardOverride && (item.RewardQuota < 0 || item.RewardQuota > common.MaxQuota) {
		return fmt.Errorf("reward quota must be between 0 and %d", common.MaxQuota)
	}
	if capabilities.DailyLimit && (item.DailyLimit < 0 || item.DailyLimit > maxGrowthRewardItemDailyLimit) {
		return fmt.Errorf("daily limit must be between 0 and %d", maxGrowthRewardItemDailyLimit)
	}
	if capabilities.ActionURL && item.ActionURL != "" {
		if utf8.RuneCountInString(item.ActionURL) > maxGrowthRewardItemActionURLLength {
			return fmt.Errorf("action URL cannot exceed %d characters", maxGrowthRewardItemActionURLLength)
		}
		parsedURL, err := url.ParseRequestURI(item.ActionURL)
		if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return errors.New("action URL must be a valid HTTP or HTTPS URL")
		}
	}
	if capabilities.ClaimPassword && utf8.RuneCountInString(item.ClaimPassword) > 128 {
		return errors.New("claim password cannot exceed 128 characters")
	}
	if item.Code == model.GrowthRewardItemJoinCommunity && item.Enabled && strings.TrimSpace(item.ClaimPassword) == "" {
		return errors.New("join community reward requires a claim password before it can be enabled")
	}
	switch item.ItemType {
	case model.GrowthRewardItemTypeAuto,
		model.GrowthRewardItemTypeManual,
		model.GrowthRewardItemTypeSemiAuto,
		model.GrowthRewardItemTypeInvitation:
		return nil
	default:
		return errors.New("invalid reward item type")
	}
}

func validateGrowthSubmissionStatusFilter(status string) error {
	switch status {
	case "", "all", model.GrowthSubmissionStatusPending, model.GrowthSubmissionStatusApproved, model.GrowthSubmissionStatusRejected:
		return nil
	default:
		return errors.New("invalid submission status filter")
	}
}

func validatePromotionWithdrawalStatusFilter(status string) error {
	switch status {
	case "", "all",
		model.PromotionWithdrawalStatusPendingReview,
		model.PromotionWithdrawalStatusApproved,
		model.PromotionWithdrawalStatusProcessing,
		model.PromotionWithdrawalStatusPaid,
		model.PromotionWithdrawalStatusRejected,
		model.PromotionWithdrawalStatusFailed:
		return nil
	default:
		return errors.New("invalid withdrawal status filter")
	}
}
