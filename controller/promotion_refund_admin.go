package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func AdminCreatePromotionRefundCase(c *gin.Context) {
	var req service.AdminPromotionRefundCaseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	auditParams := map[string]interface{}{
		"trade_no":                    req.TradeNo,
		"external_ref":                req.ExternalReference,
		"intake_source":               req.IntakeSource,
		"kind":                        req.Kind,
		"refunded_amount_minor":       req.RefundedAmountMinor,
		"currency":                    req.Currency,
		"amount_is_cumulative":        req.AmountIsCumulative,
		"idempotency_key_fingerprint": common.Sha1([]byte(req.IdempotencyKey)),
	}
	refundCase, err := service.CreateAdminPromotionRefundCase(c.GetInt("id"), c.GetInt("role"), req)
	if err != nil {
		auditParams["accepted"] = false
		auditParams["error"] = err.Error()
		recordManageAudit(c, "growth.refund_case.create", auditParams)
		common.ApiError(c, err)
		return
	}
	auditParams["accepted"] = true
	auditParams["case_id"] = refundCase.Id
	recordManageAudit(c, "growth.refund_case.create", auditParams)
	common.ApiSuccess(c, refundCase)
}

func AdminGetPromotionRefundCases(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	refundCases, total, err := service.ListAdminPromotionRefundCases(pageInfo, c.Query("status"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(refundCases)
	common.ApiSuccess(c, pageInfo)
}

func AdminApplyPromotionRefundAction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req service.AdminPromotionRefundActionRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	auditParams := map[string]interface{}{
		"id":                                  id,
		"action":                              req.Action,
		"idempotency_key_fingerprint":         common.Sha1([]byte(req.IdempotencyKey)),
		"obligation_id":                       req.ObligationId,
		"user_id":                             req.UserId,
		"top_up_id":                           req.TopUpId,
		"asset":                               req.Asset,
		"currency":                            req.Currency,
		"amount":                              req.Amount,
		"external_ref":                        req.ExternalRef,
		"remark":                              req.Remark,
		"commission_ledger_id":                req.CommissionLedgerId,
		"commission_ledger_status":            req.CommissionLedgerStatus,
		"expected_responsibility_fingerprint": req.ExpectedResponsibilityFingerprint,
		"user_subscription_id":                req.UserSubscriptionId,
	}
	refundCase, err := service.ApplyAdminPromotionRefundAction(id, c.GetInt("id"), c.GetInt("role"), req)
	if err != nil {
		auditParams["accepted"] = false
		auditParams["error"] = err.Error()
		recordManageAudit(c, "growth.refund_case.action", auditParams)
		common.ApiError(c, err)
		return
	}
	auditParams["accepted"] = true
	auditParams["trade_no"] = refundCase.TradeNo
	auditParams["refund_trade_no"] = refundCase.RefundTradeNo
	auditParams["resulting_responsibility_fingerprint"] = refundCase.ResponsibilityFingerprint
	recordManageAudit(c, "growth.refund_case.action", auditParams)
	common.ApiSuccess(c, refundCase)
}
