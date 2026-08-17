package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// PrepareBillingForSelectedGroup aligns the frozen request estimate with the
// group chosen for this upstream attempt. This matters for auto-group retries:
// a request first routed through a free group must reserve funds before a paid
// retry is sent, while an existing session only tops up to a higher estimate.
func PrepareBillingForSelectedGroup(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo == nil {
		return nil
	}
	if relayInfo.TieredBillingSnapshot != nil {
		return PrepareTieredBillingForSelectedGroup(c, relayInfo)
	}

	priceData := &relayInfo.PriceData
	targetQuota, err := common.QuotaFromFloatStrict(
		priceData.QuotaToPreConsumeBeforeGroup * priceData.GroupRatioInfo.GroupRatio,
	)
	if err != nil {
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	priceData.QuotaToPreConsume = targetQuota
	if targetQuota <= 0 {
		return nil
	}

	priceData.FreeModel = false
	if relayInfo.Billing == nil {
		return PreConsumeBilling(c, targetQuota, relayInfo)
	}
	if err := relayInfo.Billing.Reserve(targetQuota); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
	return nil
}

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	return preConsumeBilling(c, preConsumedQuota, relayInfo, false)
}

func preConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo, dispatchOccurred bool) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := newBillingSession(c, relayInfo, preConsumedQuota, dispatchOccurred)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// A genuinely free estimate can still acquire a post-response charge, for
	// example a tool-call surcharge. Establish the user's configured funding
	// source when possible so subscription-first users are not silently charged
	// to the wallet merely because the initial estimate was zero. If reservation
	// can no longer succeed after the authorized request completed, the legacy
	// path below still records the amount as wallet arrears.
	if actualQuota > 0 && relayInfo.FinalPreConsumedQuota == 0 && ctx != nil && relayInfo.QuotaClamp == nil {
		// Usage is already authoritative at this point. Create the initial
		// reservation with its dispatch marker confirmed in the same durable
		// insert; otherwise a crash before the recursive Settle call would let
		// recovery refund a charge for usage that already happened.
		if apiErr := preConsumeBilling(ctx, actualQuota, relayInfo, true); apiErr == nil {
			return SettleBilling(ctx, relayInfo, actualQuota)
		} else {
			// A failed compensation attempt is safe to replace with the legacy
			// wallet charge only when every durable post-dispatch intent is known
			// to be canceled (or was never created). If the commit result is
			// unknown, falling through could charge once here and once again when
			// the confirmed journal is recovered.
			for _, source := range []string{BillingSourceSubscription, BillingSourceWallet} {
				operationKey := billingAdjustmentOperationKey(relayInfo.RequestId, "initial:"+source)
				adjustment, loadErr := model.GetBillingAdjustment(operationKey)
				if loadErr != nil {
					return fmt.Errorf("lazy billing intent state is unknown after source selection failed: %w", loadErr)
				}
				if adjustment != nil && adjustment.DispatchConfirmed && adjustment.Status != model.BillingAdjustmentStatusCanceled {
					recordAndQueueBillingRecovery(adjustment.OperationKey, apiErr)
					return apiErr
				}
			}
			logger.LogWarn(ctx, fmt.Sprintf("lazy billing source selection failed; settling authorized wallet charge: %s", apiErr.Error()))
		}
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		// SettleBilling is only reached after the request crossed the spending
		// gate. Preserve that authorization for the legacy no-session fallback,
		// while direct PostConsumeQuota callers remain refund-hold aware.
		_, err := postConsumeQuotaWithResult(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true, true)
		return err
	}
	return nil
}
