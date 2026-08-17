package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetPromotionRefundRecovery(c *gin.Context) {
	recovery, err := service.GetUserPromotionRefundRecovery(
		c.GetInt("id"), common.GetPageQuery(c), c.Query("status"),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, recovery)
}
