package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const billingAdjustmentRecoveryBatchSize = 200

type billingAdjustmentRecoveryHandler struct{}

func (billingAdjustmentRecoveryHandler) Type() string {
	return model.SystemTaskTypeBillingAdjustmentRecovery
}

func (billingAdjustmentRecoveryHandler) Enabled() bool { return true }

func (billingAdjustmentRecoveryHandler) Interval() time.Duration { return time.Minute }

func (billingAdjustmentRecoveryHandler) NewPayload() any { return nil }

func (billingAdjustmentRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	result, runErr := RecoverPendingBillingAdjustments(ctx, billingAdjustmentRecoveryBatchSize)
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if runErr != nil {
		status = model.SystemTaskStatusFailed
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		common.SysError(fmt.Sprintf("billing adjustment recovery task %s failed to persist result: %v", task.TaskID, err))
	}
}

func init() {
	RegisterSystemTaskHandler(billingAdjustmentRecoveryHandler{})
}

type BillingAdjustmentRecoveryResult struct {
	Processed int `json:"processed"`
	Completed int `json:"completed"`
	Canceled  int `json:"canceled"`
	Pending   int `json:"pending"`
}

func ensureBillingRequestId(relayInfo *relaycommon.RelayInfo) string {
	if relayInfo == nil {
		return ""
	}
	relayInfo.RequestId = strings.TrimSpace(relayInfo.RequestId)
	if relayInfo.RequestId == "" {
		relayInfo.RequestId = common.NewRequestId()
	}
	return relayInfo.RequestId
}

func billingAdjustmentOperationKey(requestId string, suffix string) string {
	requestId = strings.TrimSpace(requestId)
	suffix = strings.TrimSpace(suffix)
	key := requestId + ":" + suffix
	if len(key) <= 191 {
		return key
	}
	digest := sha256.Sum256([]byte(requestId))
	return "billing:" + hex.EncodeToString(digest[:]) + ":" + suffix
}

func createBillingAdjustment(relayInfo *relaycommon.RelayInfo, funding FundingSource, kind string, suffix string,
	fundingDelta int, fundingTarget int, fundingRequired bool, tokenDelta int, tokenRequired bool, tokenEnforceBalance bool,
	dispatchConfirmed bool,
) (*model.BillingAdjustmentJournal, error) {
	if relayInfo == nil || funding == nil {
		return nil, errors.New("billing adjustment requires relay and funding information")
	}
	requestId := ensureBillingRequestId(relayInfo)
	return model.CreateBillingAdjustment(model.BillingAdjustmentInput{
		OperationKey:        billingAdjustmentOperationKey(requestId, suffix),
		RequestId:           requestId,
		Kind:                kind,
		FundingSource:       funding.Source(),
		UserId:              relayInfo.UserId,
		TokenId:             relayInfo.TokenId,
		ModelName:           relayInfo.OriginModelName,
		UsingGroup:          relayInfo.UsingGroup,
		FundingDelta:        int64(fundingDelta),
		FundingTarget:       int64(fundingTarget),
		TokenDelta:          int64(tokenDelta),
		FundingRequired:     fundingRequired,
		TokenRequired:       tokenRequired,
		TokenEnforceBalance: tokenEnforceBalance,
		TokenUnlimited:      relayInfo.TokenUnlimited,
		DispatchRequired:    kind == model.BillingAdjustmentKindInitialReserve || kind == model.BillingAdjustmentKindReserve,
		DispatchConfirmed:   dispatchConfirmed,
	})
}

func applyBillingFunding(adjustment *model.BillingAdjustmentJournal, funding FundingSource) (*model.SubscriptionPreConsumeResult, error) {
	if adjustment == nil || funding == nil {
		return nil, errors.New("billing funding adjustment is missing")
	}
	switch typed := funding.(type) {
	case *WalletFunding:
		return nil, model.ApplyWalletBillingAdjustment(adjustment.OperationKey)
	case *SubscriptionFunding:
		result, err := model.ApplySubscriptionBillingAdjustment(adjustment.OperationKey)
		if err == nil && result != nil {
			typed.applyPreConsumeResult(result)
			if adjustment.Kind == model.BillingAdjustmentKindInitialReserve {
				if planInfo, planErr := model.GetSubscriptionPlanInfoByUserSubscriptionId(result.UserSubscriptionId); planErr == nil && planInfo != nil {
					typed.PlanId = planInfo.PlanId
					typed.PlanTitle = planInfo.PlanTitle
				}
			}
		}
		return result, err
	default:
		// Third-party/test FundingSource implementations retain compatibility.
		// Production wallet and subscription sources always use the atomic paths
		// above.
		var err error
		switch adjustment.Kind {
		case model.BillingAdjustmentKindInitialReserve:
			err = funding.PreConsume(int(adjustment.FundingTarget))
		case model.BillingAdjustmentKindReserve, model.BillingAdjustmentKindUsageReserve:
			err = funding.PreConsume(int(adjustment.FundingDelta))
		case model.BillingAdjustmentKindSettle:
			err = funding.Settle(int(adjustment.FundingDelta))
		case model.BillingAdjustmentKindRefund:
			err = funding.Refund()
		default:
			err = fmt.Errorf("unsupported custom funding adjustment kind: %s", adjustment.Kind)
		}
		if err != nil {
			return nil, err
		}
		return nil, model.MarkBillingAdjustmentFundingApplied(adjustment.OperationKey)
	}
}

func applyBillingToken(adjustment *model.BillingAdjustmentJournal) error {
	if adjustment == nil {
		return errors.New("billing token adjustment is missing")
	}
	return model.ApplyTokenBillingAdjustment(adjustment.OperationKey)
}

func recordAndQueueBillingRecovery(operationKey string, adjustmentErr error) {
	if adjustmentErr != nil {
		if err := model.RecordBillingAdjustmentError(operationKey, adjustmentErr); err != nil {
			common.SysError("failed to persist billing adjustment recovery error: " + err.Error())
		}
	}
	if _, _, err := EnqueueSystemTask(model.SystemTaskTypeBillingAdjustmentRecovery, nil); err != nil {
		common.SysError("failed to enqueue billing adjustment recovery: " + err.Error())
	}
}

func cancelBillingReservation(adjustment *model.BillingAdjustmentJournal, cause error) (bool, error) {
	if adjustment == nil {
		return false, errors.New("billing reservation adjustment is missing")
	}
	rollbackKey := billingAdjustmentOperationKey(adjustment.RequestId, "rollback:"+strconv.FormatInt(adjustment.ID, 10))
	rollback, err := model.CancelBillingAdjustmentWithRollback(adjustment.OperationKey, rollbackKey, cause)
	if errors.Is(err, model.ErrBillingAdjustmentAlreadyCompleted) {
		return false, nil
	}
	if err != nil {
		recordAndQueueBillingRecovery(adjustment.OperationKey, err)
		return true, err
	}
	if rollback == nil || rollback.TokenApplied {
		return true, nil
	}
	if err := applyBillingToken(rollback); err != nil {
		recordAndQueueBillingRecovery(rollback.OperationKey, err)
		return true, err
	}
	return true, nil
}

// refundUndispatchedBillingReservation closes a provisional authorization and
// applies the exact durable reversal for whichever sides were already
// pre-applied. A false canceled result means dispatch confirmation won the
// race, so the caller must treat the reservation as authorized instead.
func refundUndispatchedBillingReservation(adjustment *model.BillingAdjustmentJournal, funding FundingSource, cause error) (canceled bool, refund *model.BillingAdjustmentJournal, err error) {
	if adjustment == nil {
		return false, nil, errors.New("billing reservation adjustment is missing")
	}
	refundKey := billingAdjustmentOperationKey(adjustment.RequestId, "undispatched_refund:"+strconv.FormatInt(adjustment.ID, 10))
	refund, err = model.CancelUndispatchedBillingReservationWithRefund(adjustment.OperationKey, refundKey, cause)
	if errors.Is(err, model.ErrBillingAdjustmentDispatchConflict) {
		stored, loadErr := model.GetBillingAdjustment(adjustment.OperationKey)
		if loadErr != nil {
			return false, nil, loadErr
		}
		if stored != nil && stored.DispatchConfirmed && stored.Status != model.BillingAdjustmentStatusCanceled {
			return false, nil, nil
		}
	}
	if err != nil {
		recordAndQueueBillingRecovery(adjustment.OperationKey, err)
		return false, nil, err
	}
	if refund == nil {
		return true, nil, nil
	}
	if !refund.FundingApplied {
		if funding != nil {
			_, err = applyBillingFunding(refund, funding)
		} else {
			err = applyPersistedBillingFunding(refund)
		}
		if err != nil {
			recordAndQueueBillingRecovery(refund.OperationKey, err)
			return true, refund, err
		}
	}
	if !refund.TokenApplied {
		if err = applyBillingToken(refund); err != nil {
			recordAndQueueBillingRecovery(refund.OperationKey, err)
			return true, refund, err
		}
	}
	return true, refund, nil
}

func applyPersistedBillingFunding(adjustment *model.BillingAdjustmentJournal) error {
	switch adjustment.FundingSource {
	case BillingSourceWallet:
		return model.ApplyWalletBillingAdjustment(adjustment.OperationKey)
	case BillingSourceSubscription:
		_, err := model.ApplySubscriptionBillingAdjustment(adjustment.OperationKey)
		return err
	default:
		return fmt.Errorf("unsupported persisted billing source: %s", adjustment.FundingSource)
	}
}

func applyTaskBillingProjections(operationKey string) error {
	adjustment, err := model.GetBillingAdjustment(operationKey)
	if err != nil || adjustment == nil {
		return err
	}
	if adjustment.Kind != model.BillingAdjustmentKindTaskProjection {
		return errors.New("billing adjustment is not a task projection")
	}
	if !adjustment.TokenApplied {
		if err := applyBillingToken(adjustment); err != nil {
			return err
		}
	}
	if !adjustment.UsageApplied {
		if err := model.ApplyBillingUsageProjection(operationKey); err != nil {
			return err
		}
	}
	if !adjustment.LogApplied {
		claimedBy := fmt.Sprintf("%s-%s", common.NodeName, common.GetRandomString(12))
		claimed, err := model.ClaimBillingAdjustmentLogProjection(operationKey, claimedBy, common.GetTimestamp()+60)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		if err := model.EnsureTaskBillingProjectionLog(adjustment); err != nil {
			_ = model.ReleaseBillingAdjustmentLogProjection(operationKey, claimedBy)
			return err
		}
		if err := model.CompleteBillingAdjustmentLogProjection(operationKey, claimedBy); err != nil {
			return err
		}
	}
	return nil
}

func recoverBillingAdjustment(adjustment *model.BillingAdjustmentJournal) (canceled bool, err error) {
	if adjustment == nil {
		return false, nil
	}
	if adjustment.Kind == model.BillingAdjustmentKindTaskProjection {
		return false, applyTaskBillingProjections(adjustment.OperationKey)
	}
	if adjustment.Kind == model.BillingAdjustmentKindInitialReserve {
		if adjustment.DispatchRequired && !adjustment.DispatchConfirmed {
			canceled, _, err := refundUndispatchedBillingReservation(
				adjustment,
				nil,
				errors.New("initial billing reservation was interrupted before upstream dispatch authorization"),
			)
			if err != nil || canceled {
				return canceled, err
			}
			stored, loadErr := model.GetBillingAdjustment(adjustment.OperationKey)
			if loadErr != nil {
				return false, loadErr
			}
			if stored == nil {
				return false, errors.New("confirmed billing reservation is missing")
			}
			adjustment = stored
		}
		if adjustment.DispatchRequired {
			if !adjustment.TokenApplied {
				if err := applyBillingToken(adjustment); err != nil {
					return false, err
				}
			}
			if !adjustment.FundingApplied {
				if err := applyPersistedBillingFunding(adjustment); err != nil {
					return false, err
				}
			}
			return false, nil
		}

		// Legacy initial reservations predate the dispatch marker. A partial
		// token-only reservation can still be safely unwound, while a completed
		// legacy row retains its original pre-consume semantics.
		if adjustment.FundingApplied {
			if adjustment.TokenApplied {
				return false, nil
			}
			return false, errors.New("initial billing reservation has funding without its token prerequisite")
		}
		return cancelBillingReservation(adjustment, errors.New("initial billing reservation was interrupted before funding completed"))
	}

	if adjustment.Kind == model.BillingAdjustmentKindReserve {
		if adjustment.DispatchRequired && !adjustment.DispatchConfirmed {
			canceled, _, err := refundUndispatchedBillingReservation(
				adjustment,
				nil,
				errors.New("billing reservation was interrupted before upstream dispatch authorization"),
			)
			return canceled, err
		}
		if !adjustment.TokenApplied {
			if err := applyBillingToken(adjustment); err != nil {
				if errors.Is(err, model.ErrBillingAdjustmentTokenInsufficient) {
					return cancelBillingReservation(adjustment, err)
				}
				return false, err
			}
		}
		if !adjustment.FundingApplied {
			if err := applyPersistedBillingFunding(adjustment); err != nil {
				if errors.Is(err, model.ErrBillingAdjustmentFundingInsufficient) ||
					errors.Is(err, model.ErrUserRefundHeld) ||
					strings.Contains(err.Error(), "quota insufficient") ||
					strings.Contains(err.Error(), "group unsupported") {
					return cancelBillingReservation(adjustment, err)
				}
				return false, err
			}
		}
		return false, nil
	}

	if adjustment.Kind == model.BillingAdjustmentKindUsageReserve {
		if !adjustment.TokenApplied {
			if err := applyBillingToken(adjustment); err != nil {
				if errors.Is(err, model.ErrBillingAdjustmentTokenInsufficient) {
					return cancelBillingReservation(adjustment, err)
				}
				return false, err
			}
		}
		if !adjustment.FundingApplied {
			if err := applyPersistedBillingFunding(adjustment); err != nil {
				if errors.Is(err, model.ErrBillingAdjustmentFundingInsufficient) ||
					errors.Is(err, model.ErrUserRefundHeld) ||
					strings.Contains(err.Error(), "quota insufficient") ||
					strings.Contains(err.Error(), "group unsupported") {
					return cancelBillingReservation(adjustment, err)
				}
				return false, err
			}
		}
		return false, nil
	}

	// Settlement and refund establish their final funding intent before token
	// accounting. Rollbacks have no funding side and naturally skip it.
	if !adjustment.FundingApplied {
		if err := applyPersistedBillingFunding(adjustment); err != nil {
			return false, err
		}
	}
	if !adjustment.TokenApplied {
		if err := applyBillingToken(adjustment); err != nil {
			return false, err
		}
	}
	return false, nil
}

// RecoverPendingBillingAdjustments replays durable, due journal rows. Every
// side is itself idempotent, so repeating a whole task after lease loss or an
// unknown commit result cannot duplicate a wallet refund or token adjustment.
func RecoverPendingBillingAdjustments(ctx context.Context, limit int) (BillingAdjustmentRecoveryResult, error) {
	if limit <= 0 {
		limit = billingAdjustmentRecoveryBatchSize
	}
	rows, err := model.ListPendingBillingAdjustments(limit)
	if err != nil {
		return BillingAdjustmentRecoveryResult{}, err
	}
	result := BillingAdjustmentRecoveryResult{}
	var recoveryErrors []error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(recoveryErrors, err)...)
		}
		result.Processed++
		canceled, recoverErr := recoverBillingAdjustment(row)
		if recoverErr != nil {
			result.Pending++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("%s: %w", row.OperationKey, recoverErr))
			_ = model.RecordBillingAdjustmentError(row.OperationKey, recoverErr)
			continue
		}
		if canceled {
			result.Canceled++
		} else {
			result.Completed++
		}
	}
	return result, errors.Join(recoveryErrors...)
}
