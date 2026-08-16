package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type promotionRefundResolutionRequest struct {
	ReviewNote string `json:"review_note" binding:"required"`
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

func AdminResolvePromotionRefundCase(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req promotionRefundResolutionRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	refundCase, err := service.ResolvePromotionRefundCase(id, c.GetInt("id"), req.ReviewNote)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "growth.refund_case.resolve", map[string]interface{}{
		"id":              refundCase.Id,
		"trade_no":        refundCase.TradeNo,
		"refund_trade_no": refundCase.RefundTradeNo,
		"review_note":     refundCase.ReviewNote,
	})
	common.ApiSuccess(c, refundCase)
}
