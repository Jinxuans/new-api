package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---- Shared types ----

type SubscriptionPlanDTO struct {
	Plan model.SubscriptionPlan `json:"plan"`
}

type BillingPreferenceRequest struct {
	BillingPreference string `json:"billing_preference"`
}

type SubscriptionBalancePayRequest struct {
	PlanId int `json:"plan_id"`
}

// ---- User APIs ----

func GetSubscriptionPlans(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiSuccess(c, []SubscriptionPlanDTO{})
		return
	}

	var plans []model.SubscriptionPlan
	if err := model.DB.Where("enabled = ?", true).Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.ApiSuccess(c, result)
}

func GetSubscriptionSelf(c *gin.Context) {
	userId := c.GetInt("id")
	settingMap, _ := model.GetUserSetting(userId, false)
	pref := common.NormalizeBillingPreference(settingMap.BillingPreference)

	// Get all subscriptions (including expired)
	allSubscriptions, err := model.GetAllUserSubscriptions(userId)
	if err != nil {
		allSubscriptions = []model.SubscriptionSummary{}
	}

	// Get active subscriptions for backward compatibility
	activeSubscriptions, err := model.GetAllActiveUserSubscriptions(userId)
	if err != nil {
		activeSubscriptions = []model.SubscriptionSummary{}
	}

	common.ApiSuccess(c, gin.H{
		"billing_preference": pref,
		"subscriptions":      activeSubscriptions, // all active subscriptions
		"all_subscriptions":  allSubscriptions,    // all subscriptions including expired
	})
}

func UpdateSubscriptionPreference(c *gin.Context) {
	userId := c.GetInt("id")
	var req BillingPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	pref := common.NormalizeBillingPreference(req.BillingPreference)

	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	current := user.GetSetting()
	current.BillingPreference = pref
	if err := model.UpdateUserSetting(user.Id, current); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"billing_preference": pref})
}

func SubscriptionRequestBalancePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	userId := c.GetInt("id")
	var req SubscriptionBalancePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	if err := model.PurchaseSubscriptionWithBalance(userId, req.PlanId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- Admin APIs ----

func AdminListSubscriptionPlans(c *gin.Context) {
	var plans []model.SubscriptionPlan
	if err := model.DB.Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.ApiSuccess(c, result)
}

type AdminUpsertSubscriptionPlanRequest struct {
	Plan model.SubscriptionPlan `json:"plan"`
}

func normalizeAndValidateSubscriptionPlanGroups(plan *model.SubscriptionPlan) bool {
	if plan == nil {
		return true
	}
	plan.NormalizeSupportedGroups()
	groupRatio := ratio_setting.GetGroupRatioCopy()
	for _, group := range plan.SupportedGroups {
		if _, ok := groupRatio[group]; !ok {
			return false
		}
	}
	return true
}

func AdminCreateSubscriptionPlan(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req AdminUpsertSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.Plan.Id = 0
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.ApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.ApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.ApiErrorMsg(c, "价格不能超过9999")
		return
	}
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	req.Plan.Currency = "USD"
	if req.Plan.AllowBalancePay == nil {
		req.Plan.AllowBalancePay = common.GetPointer(true)
	}
	if req.Plan.AllowWalletOverflow == nil {
		req.Plan.AllowWalletOverflow = common.GetPointer(true)
	}
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = model.SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != model.SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.ApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	if req.Plan.TotalAmount < 0 {
		common.ApiErrorMsg(c, "总额度不能为负数")
		return
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	if req.Plan.UpgradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.UpgradeGroup]; !ok {
			common.ApiErrorMsg(c, "升级分组不存在")
			return
		}
	}
	if !normalizeAndValidateSubscriptionPlanGroups(&req.Plan) {
		common.ApiErrorMsg(c, "支持分组不存在")
		return
	}
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.DowngradeGroup]; !ok {
			common.ApiErrorMsg(c, "降级分组不存在")
			return
		}
	}
	req.Plan.QuotaResetPeriod = model.NormalizeResetPeriod(req.Plan.QuotaResetPeriod)
	if req.Plan.QuotaResetPeriod == model.SubscriptionResetCustom && req.Plan.QuotaResetCustomSeconds <= 0 {
		common.ApiErrorMsg(c, "自定义重置周期需大于0秒")
		return
	}
	err := model.DB.Create(&req.Plan).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(req.Plan.Id)
	common.ApiSuccess(c, req.Plan)
}

func AdminUpdateSubscriptionPlan(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpsertSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.ApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.ApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.ApiErrorMsg(c, "价格不能超过9999")
		return
	}
	req.Plan.Id = id
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	req.Plan.Currency = "USD"
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = model.SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != model.SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.ApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	if req.Plan.TotalAmount < 0 {
		common.ApiErrorMsg(c, "总额度不能为负数")
		return
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	if req.Plan.UpgradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.UpgradeGroup]; !ok {
			common.ApiErrorMsg(c, "升级分组不存在")
			return
		}
	}
	if !normalizeAndValidateSubscriptionPlanGroups(&req.Plan) {
		common.ApiErrorMsg(c, "支持分组不存在")
		return
	}
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.DowngradeGroup]; !ok {
			common.ApiErrorMsg(c, "降级分组不存在")
			return
		}
	}
	req.Plan.QuotaResetPeriod = model.NormalizeResetPeriod(req.Plan.QuotaResetPeriod)
	if req.Plan.QuotaResetPeriod == model.SubscriptionResetCustom && req.Plan.QuotaResetCustomSeconds <= 0 {
		common.ApiErrorMsg(c, "自定义重置周期需大于0秒")
		return
	}

	err := model.DB.Transaction(func(tx *gorm.DB) error {
		// update plan (allow zero values updates with map)
		updateMap := map[string]interface{}{
			"title":                      req.Plan.Title,
			"subtitle":                   req.Plan.Subtitle,
			"price_amount":               req.Plan.PriceAmount,
			"currency":                   req.Plan.Currency,
			"duration_unit":              req.Plan.DurationUnit,
			"duration_value":             req.Plan.DurationValue,
			"custom_seconds":             req.Plan.CustomSeconds,
			"enabled":                    req.Plan.Enabled,
			"sort_order":                 req.Plan.SortOrder,
			"stripe_price_id":            req.Plan.StripePriceId,
			"creem_product_id":           req.Plan.CreemProductId,
			"waffo_pancake_product_id":   req.Plan.WaffoPancakeProductId,
			"max_purchase_per_user":      req.Plan.MaxPurchasePerUser,
			"total_amount":               req.Plan.TotalAmount,
			"upgrade_group":              req.Plan.UpgradeGroup,
			"supported_groups":           req.Plan.SupportedGroups,
			"downgrade_group":            req.Plan.DowngradeGroup,
			"quota_reset_period":         req.Plan.QuotaResetPeriod,
			"quota_reset_custom_seconds": req.Plan.QuotaResetCustomSeconds,
			"updated_at":                 common.GetTimestamp(),
		}
		if req.Plan.AllowBalancePay != nil {
			updateMap["allow_balance_pay"] = *req.Plan.AllowBalancePay
		}
		if req.Plan.AllowWalletOverflow != nil {
			updateMap["allow_wallet_overflow"] = *req.Plan.AllowWalletOverflow
		}
		if err := tx.Model(&model.SubscriptionPlan{}).Where("id = ?", id).Updates(updateMap).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(id)
	common.ApiSuccess(c, nil)
}

type AdminUpdateSubscriptionPlanStatusRequest struct {
	Enabled *bool `json:"enabled"`
}

func AdminUpdateSubscriptionPlanStatus(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpdateSubscriptionPlanStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := model.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", id).Update("enabled", *req.Enabled).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(id)
	common.ApiSuccess(c, nil)
}

type AdminBindSubscriptionRequest struct {
	UserId         int    `json:"user_id"`
	PlanId         int    `json:"plan_id"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

func AdminBindSubscription(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req AdminBindSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserId <= 0 || req.PlanId <= 0 ||
		strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	msg, replayed, err := model.GrantUserSubscriptionByAdmin(model.AdminSubscriptionOperationInput{
		UserId: req.UserId, PlanId: req.PlanId, ActorId: c.GetInt("id"), ActorRole: c.GetInt("role"),
		ActorRef: c.GetString("username"), Reason: req.Reason, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !replayed {
		recordManageAuditFor(c, req.UserId, "subscription.entitlement_grant", map[string]interface{}{
			"target_user_id": req.UserId,
			"plan_id":        req.PlanId,
			"reason":         strings.TrimSpace(req.Reason),
		})
	} else {
		markAuditLogged(c)
	}
	common.ApiSuccess(c, gin.H{"message": msg, "replayed": replayed})
}

// ---- Admin: user subscription management ----

func AdminListUserSubscriptions(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	subs, err := model.GetAllUserSubscriptions(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, subs)
}

type AdminCreateUserSubscriptionRequest struct {
	PlanId         int    `json:"plan_id"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

type AdminResetSubscriptionRequest struct {
	PlanId           int    `json:"plan_id"`
	AdvanceResetTime *bool  `json:"advance_reset_time"`
	Reason           string `json:"reason"`
	IdempotencyKey   string `json:"idempotency_key"`
}

type AdminInvalidateSubscriptionRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

func resolveAdvanceResetTime(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func recordSubscriptionResetUserLogs(result *model.SubscriptionResetResult, adminInfo map[string]interface{}) {
	if result == nil || result.ResetCount == 0 {
		return
	}
	content := fmt.Sprintf("管理员重置订阅套餐 %s（ID: %d）额度", result.PlanTitle, result.PlanId)
	for _, userId := range result.AffectedUserIds {
		model.RecordLogWithAdminInfo(userId, model.LogTypeManage, content, adminInfo)
	}
}

// AdminCreateUserSubscription creates a new user subscription from a plan (no payment).
func AdminCreateUserSubscription(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req AdminCreateUserSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 ||
		strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	msg, replayed, err := model.GrantUserSubscriptionByAdmin(model.AdminSubscriptionOperationInput{
		UserId: userId, PlanId: req.PlanId, ActorId: c.GetInt("id"), ActorRole: c.GetInt("role"),
		ActorRef: c.GetString("username"), Reason: req.Reason, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !replayed {
		recordManageAuditFor(c, userId, "subscription.entitlement_grant", map[string]interface{}{
			"target_user_id": userId,
			"plan_id":        req.PlanId,
			"reason":         strings.TrimSpace(req.Reason),
		})
	} else {
		markAuditLogged(c)
	}
	common.ApiSuccess(c, gin.H{"message": msg, "replayed": replayed})
}

func AdminResetUserSubscriptionsByPlan(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if req.PlanId <= 0 || strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, replayed, err := model.ResetUserSubscriptionsByPlanByAdmin(model.AdminSubscriptionOperationInput{
		UserId: userId, PlanId: req.PlanId, AdvanceResetTime: advanceResetTime,
		ActorId: c.GetInt("id"), ActorRole: c.GetInt("role"), ActorRef: c.GetString("username"),
		Reason: req.Reason, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result.Replayed = replayed
	if !replayed {
		recordSubscriptionResetUserLogs(result, auditOperatorInfo(c))
		recordManageAuditFor(c, userId, "subscription.user_plan_reset", map[string]interface{}{
			"target_user_id":     userId,
			"plan_id":            result.PlanId,
			"plan_title":         result.PlanTitle,
			"reset_count":        result.ResetCount,
			"user_count":         result.UserCount,
			"advance_reset_time": result.AdvanceResetTime,
			"reason":             strings.TrimSpace(req.Reason),
		})
	} else {
		markAuditLogged(c)
	}
	common.ApiSuccess(c, result)
}

func AdminResetPlanSubscriptions(c *gin.Context) {
	planId, _ := strconv.Atoi(c.Param("id"))
	if planId <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" ||
		strings.TrimSpace(req.IdempotencyKey) == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, replayed, err := model.ResetPlanSubscriptionsByAdmin(model.AdminSubscriptionOperationInput{
		PlanId: planId, AdvanceResetTime: advanceResetTime,
		ActorId: c.GetInt("id"), ActorRole: c.GetInt("role"), ActorRef: c.GetString("username"),
		Reason: req.Reason, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result.Replayed = replayed
	if !replayed {
		recordSubscriptionResetUserLogs(result, auditOperatorInfo(c))
		common.SysLog(fmt.Sprintf("admin reset subscription plan %d quota: reset_count=%d user_count=%d advance_reset_time=%t",
			result.PlanId, result.ResetCount, result.UserCount, result.AdvanceResetTime))
		recordManageAudit(c, "subscription.plan_reset", map[string]interface{}{
			"plan_id":            result.PlanId,
			"plan_title":         result.PlanTitle,
			"reset_count":        result.ResetCount,
			"user_count":         result.UserCount,
			"advance_reset_time": result.AdvanceResetTime,
			"reason":             strings.TrimSpace(req.Reason),
		})
	} else {
		markAuditLogged(c)
	}
	common.ApiSuccess(c, result)
}

// AdminInvalidateUserSubscription cancels a user subscription immediately.
func AdminInvalidateUserSubscription(c *gin.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}
	var req AdminInvalidateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" ||
		strings.TrimSpace(req.IdempotencyKey) == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	result, replayed, err := model.InvalidateUserSubscriptionByAdmin(model.AdminSubscriptionOperationInput{
		UserSubscriptionId: subId, ActorId: c.GetInt("id"), ActorRole: c.GetInt("role"),
		ActorRef: c.GetString("username"), Reason: req.Reason, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !replayed {
		recordManageAuditFor(c, result.UserId, "subscription.entitlement_invalidate", map[string]interface{}{
			"subscription_id":   result.UserSubscriptionId,
			"plan_id":           result.PlanId,
			"previous_status":   result.StatusBefore,
			"current_status":    result.StatusAfter,
			"previous_end_time": result.EndTimeBefore,
			"current_end_time":  result.EndTimeAfter,
			"previous_group":    result.UserGroupBefore,
			"current_group":     result.UserGroupAfter,
			"reason":            strings.TrimSpace(req.Reason),
			"request_method":    c.Request.Method,
		})
	} else {
		markAuditLogged(c)
	}
	common.ApiSuccess(c, result)
}

// AdminDeleteUserSubscription preserves the legacy route while invalidating
// the entitlement so payment and refund evidence remains queryable.
func AdminDeleteUserSubscription(c *gin.Context) {
	AdminInvalidateUserSubscription(c)
}
