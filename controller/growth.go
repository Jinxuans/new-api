package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type claimGrowthRewardItemRequest struct {
	Password string `json:"password"`
}

type rejectGrowthSubmissionRequest struct {
	ReviewNote string `json:"review_note"`
}

type promotionWithdrawalRequest struct {
	PayoutMethod            string `json:"payout_method"`
	PayoutAccount           string `json:"payout_account"`
	Remark                  string `json:"remark"`
	ExpectedAmountCents     int64  `json:"expected_amount_cents" binding:"required,min=1"`
	ExpectedQuotaEquivalent int64  `json:"expected_quota_equivalent" binding:"min=1"`
}

type promotionCommissionBalanceRequest struct {
	ExpectedAmountCents     int64 `json:"expected_amount_cents" binding:"required,min=1"`
	ExpectedQuotaEquivalent int64 `json:"expected_quota_equivalent" binding:"required,min=1"`
}

type promotionWithdrawalReviewRequest struct {
	TradeNo    string `json:"trade_no"`
	ReviewNote string `json:"review_note"`
}

type promotionWithdrawalPayoutInitiateRequest struct {
	TradeNo    string `json:"trade_no" binding:"required"`
	ReviewNote string `json:"review_note"`
}

type promotionWithdrawalPaidRequest struct {
	TradeNo    string `json:"trade_no"`
	ReviewNote string `json:"review_note"`
}

type promotionWithdrawalFailedRequest struct {
	TradeNo     string `json:"trade_no" binding:"required"`
	FailureNote string `json:"failure_note" binding:"required"`
}

func AdminGetGrowthConfig(c *gin.Context) {
	common.ApiSuccess(c, service.GetAdminGrowthConfig())
}

func AdminUpdateGrowthConfig(c *gin.Context) {
	var req service.AdminGrowthConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.UpdateAdminGrowthConfig(req); err != nil {
		common.ApiError(c, err)
		return
	}

	sections := make([]string, 0, 2)
	if req.RewardProgram != nil {
		sections = append(sections, "reward_program")
	}
	if req.ReferralProgram != nil {
		sections = append(sections, "referral_program")
	}
	recordManageAudit(c, "growth.config.update", map[string]interface{}{
		"sections": sections,
	})
	common.ApiSuccess(c, service.GetAdminGrowthConfig())
}

func GetGrowthSummary(c *gin.Context) {
	summary, err := service.GetGrowthSummary(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func GetGrowthRewardItems(c *gin.Context) {
	items, err := service.ListGrowthRewardItemsForUser(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.ToUserGrowthRewardItems(items))
}

func ClaimGrowthRewardItem(c *gin.Context) {
	var req claimGrowthRewardItemRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			common.ApiError(c, err)
			return
		}
	}
	reward, err := service.ClaimGrowthRewardItem(c.GetInt("id"), c.Param("code"), req.Password)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.ToUserGrowthReward(reward))
}

func GetGrowthRewards(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rewards, total, err := service.ListUserGrowthRewards(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rewards)
	common.ApiSuccess(c, pageInfo)
}

func GetPromotionEvents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	events, total, err := service.ListPromotionEvents(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

func GetPromotionFundRecords(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	records, total, err := service.ListPromotionFundRecords(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

func CreateGrowthSubmission(c *gin.Context) {
	var req service.GrowthSubmissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	submission, err := service.CreateGrowthSubmission(c.GetInt("id"), req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.ToUserGrowthSubmission(submission))
}

func GetGrowthSubmissions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	submissions, total, err := service.ListUserGrowthSubmissions(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(submissions)
	common.ApiSuccess(c, pageInfo)
}

func GetPromotionCommissionLedgers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	ledgers, total, err := service.ListPromotionCommissionLedgers(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(ledgers)
	common.ApiSuccess(c, pageInfo)
}

func TransferPromotionCommissionsToQuota(c *gin.Context) {
	var req promotionCommissionBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := service.TransferAllSettledPromotionCommissionsToQuota(c.GetInt("id"), service.PromotionCommissionBalanceExpectation{
		AmountCents:     req.ExpectedAmountCents,
		QuotaEquivalent: req.ExpectedQuotaEquivalent,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"quota": quota})
}

func CreatePromotionWithdrawal(c *gin.Context) {
	var req promotionWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.CreatePromotionWithdrawal(c.GetInt("id"), service.PromotionWithdrawalRequest{
		PayoutMethod:            req.PayoutMethod,
		PayoutAccount:           req.PayoutAccount,
		Remark:                  req.Remark,
		ExpectedAmountCents:     req.ExpectedAmountCents,
		ExpectedQuotaEquivalent: req.ExpectedQuotaEquivalent,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.ToUserPromotionWithdrawal(withdrawal))
}

func GetPromotionWithdrawals(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	withdrawals, total, err := service.ListPromotionWithdrawals(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(withdrawals)
	common.ApiSuccess(c, pageInfo)
}

func GetPromotionWithdrawal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.GetPromotionWithdrawal(c.GetInt("id"), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminGetGrowthRewardItems(c *gin.Context) {
	items, err := service.ListAdminGrowthRewardItems()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func AdminCreateGrowthRewardItem(c *gin.Context) {
	var req service.AdminGrowthRewardItemCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.CreateAdminGrowthRewardItem(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func AdminUpdateGrowthRewardItem(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req service.AdminGrowthRewardItemUpdateRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.UpdateAdminGrowthRewardItem(id, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func AdminGetGrowthRewards(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rewards, total, err := service.AdminListGrowthRewards(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rewards)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetGrowthSubmissions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	submissions, total, err := service.ListAdminGrowthSubmissions(pageInfo, c.Query("status"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(submissions)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetPromotionWithdrawals(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	withdrawals, total, err := service.ListAdminPromotionWithdrawals(pageInfo, c.Query("status"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(withdrawals)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetPromotionFundRecords(c *gin.Context) {
	userId, err := strconv.Atoi(c.Query("user_id"))
	if err != nil || userId <= 0 {
		common.ApiError(c, errors.New("valid user_id is required"))
		return
	}
	pageInfo := common.GetPageQuery(c)
	records, total, err := service.ListAdminPromotionFundRecords(userId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetPromotionWithdrawal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.GetAdminPromotionWithdrawal(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminApprovePromotionWithdrawal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req promotionWithdrawalReviewRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.AdminApprovePromotionWithdrawal(id, c.GetInt("id"), service.PromotionWithdrawalReviewRequest{
		TradeNo:    req.TradeNo,
		ReviewNote: req.ReviewNote,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminRejectPromotionWithdrawal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req promotionWithdrawalReviewRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.AdminRejectPromotionWithdrawal(id, c.GetInt("id"), service.PromotionWithdrawalReviewRequest{
		ReviewNote: req.ReviewNote,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminInitiatePromotionWithdrawalPayout(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req promotionWithdrawalPayoutInitiateRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.AdminInitiatePromotionWithdrawalPayout(id, c.GetInt("id"), service.PromotionWithdrawalReviewRequest{
		TradeNo:    req.TradeNo,
		ReviewNote: req.ReviewNote,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminMarkPromotionWithdrawalPaid(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req promotionWithdrawalPaidRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.AdminMarkPromotionWithdrawalPaid(id, c.GetInt("id"), service.PromotionWithdrawalReviewRequest{
		TradeNo:    req.TradeNo,
		ReviewNote: req.ReviewNote,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminMarkPromotionWithdrawalFailed(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req promotionWithdrawalFailedRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	withdrawal, err := service.AdminMarkPromotionWithdrawalFailed(id, c.GetInt("id"), service.PromotionWithdrawalReviewRequest{
		TradeNo:     req.TradeNo,
		FailureNote: req.FailureNote,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

func AdminApproveGrowthSubmission(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req service.GrowthReviewRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	submission, err := service.ApproveGrowthSubmission(id, c.GetInt("id"), req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, submission)
}

func AdminRejectGrowthSubmission(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req rejectGrowthSubmissionRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	submission, err := service.RejectGrowthSubmission(id, c.GetInt("id"), req.ReviewNote)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, submission)
}

func AdminGetGrowthStats(c *gin.Context) {
	stats, err := service.GetGrowthAdminStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}
