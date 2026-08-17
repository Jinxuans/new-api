package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	taskBillingSourceWallet       = "wallet"
	taskBillingSourceSubscription = "subscription"
)

var (
	// ErrTaskQuotaTransitionConflict means the caller's task snapshot is stale.
	// The persisted quota remains the idempotency marker for async settlement,
	// so a caller must reload it instead of applying a delta from stale state.
	ErrTaskQuotaTransitionConflict = errors.New("task quota transition conflicts with persisted state")
	// ErrTaskBillingEvidenceMissing prevents a subscription task from changing
	// a possibly newer quota period when its request-scoped lifecycle is absent.
	ErrTaskBillingEvidenceMissing = errors.New("task billing evidence is missing")
)

// ApplyTaskQuotaTransition atomically reconciles a task's durable funding
// source and quota marker from expectedQuota to targetQuota. It returns true
// only when this call applied the transition. A repeated target is a successful
// no-op (false, nil), which makes polling and refund retries idempotent.
//
// Wallet tasks use the task quota CAS as their durable idempotency boundary.
// Subscription tasks additionally require BillingRequestId so settlement can
// use the request lifecycle and cannot debit or refund a newer reset period.
func ApplyTaskQuotaTransition(taskId int64, expectedQuota int, targetQuota int) (bool, error) {
	return ApplyTaskQuotaTransitionWithProjection(taskId, expectedQuota, targetQuota, TaskBillingProjectionInput{})
}

// ApplyTaskQuotaTransitionWithProjection commits the funding/task quota CAS
// together with a durable event for token, usage and log projections. A CAS
// winner can therefore crash immediately after commit without losing the
// secondary accounting work.
func ApplyTaskQuotaTransitionWithProjection(taskId int64, expectedQuota int, targetQuota int, projection TaskBillingProjectionInput) (bool, error) {
	if taskId <= 0 {
		return false, errors.New("invalid task id")
	}
	if expectedQuota < 0 || targetQuota < 0 {
		return false, errors.New("task quota must not be negative")
	}
	if expectedQuota > common.MaxQuota || targetQuota > common.MaxQuota {
		return false, errors.New("task quota exceeds the persistent quota range")
	}

	applied := false
	walletUserId := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).Where("id = ?", taskId).First(&task).Error; err != nil {
			return err
		}
		// Fence the cache for both a fresh wallet transition and an idempotent
		// replay. In particular, a database driver may report an unknown commit
		// result after the wallet mutation committed; the next replay must still
		// remove any stale pre-commit quota snapshot.
		if task.UserId > 0 && (task.PrivateData.BillingSource == taskBillingSourceWallet ||
			(task.PrivateData.BillingSource == "" && task.PrivateData.SubscriptionId == 0 && strings.TrimSpace(task.PrivateData.BillingRequestId) == "")) {
			walletUserId = task.UserId
		}

		if task.Quota == targetQuota {
			return nil
		}
		if task.Quota != expectedQuota {
			return fmt.Errorf("%w: task=%d expected=%d current=%d target=%d",
				ErrTaskQuotaTransitionConflict, taskId, expectedQuota, task.Quota, targetQuota)
		}

		delta := targetQuota - expectedQuota
		switch task.PrivateData.BillingSource {
		case taskBillingSourceSubscription:
			requestId := strings.TrimSpace(task.PrivateData.BillingRequestId)
			if requestId == "" || task.PrivateData.SubscriptionId <= 0 {
				return fmt.Errorf("%w: subscription task %d has no request lifecycle", ErrTaskBillingEvidenceMissing, taskId)
			}

			record, err := lockSubscriptionPreConsumeRecordTx(tx, requestId)
			if err != nil {
				return fmt.Errorf("%w: subscription task %d: %v", ErrTaskBillingEvidenceMissing, taskId, err)
			}
			if record.UserId != task.UserId || record.UserSubscriptionId != task.PrivateData.SubscriptionId {
				return fmt.Errorf("%w: subscription task %d lifecycle owner mismatch", ErrTaskBillingEvidenceMissing, taskId)
			}

			if targetQuota == 0 {
				if err := RefundSubscriptionPreConsumeTx(tx, requestId); err != nil {
					return err
				}
			} else {
				result, err := SettleSubscriptionPreConsumeTx(tx, requestId, int64(targetQuota))
				if err != nil {
					return err
				}
				if result == nil || result.UserSubscriptionId != task.PrivateData.SubscriptionId {
					return fmt.Errorf("%w: subscription task %d lifecycle changed", ErrTaskBillingEvidenceMissing, taskId)
				}
			}

		case "":
			// Tasks from before funding-source persistence were wallet billed.
			// Only use that legacy fallback when no newer subscription/request
			// markers are present; mixed evidence is economically ambiguous.
			if task.PrivateData.SubscriptionId > 0 || strings.TrimSpace(task.PrivateData.BillingRequestId) != "" {
				return fmt.Errorf("%w: legacy task %d has ambiguous funding markers", ErrTaskBillingEvidenceMissing, taskId)
			}
			fallthrough

		case taskBillingSourceWallet:
			if task.UserId <= 0 {
				return fmt.Errorf("%w: wallet task %d has no user", ErrTaskBillingEvidenceMissing, taskId)
			}
			if task.PrivateData.SubscriptionId > 0 {
				return fmt.Errorf("%w: wallet task %d references a subscription", ErrTaskBillingEvidenceMissing, taskId)
			}
			if delta != 0 {
				query := tx.Model(&User{}).Where("id = ?", task.UserId)
				var result *gorm.DB
				if delta > 0 {
					// Authorized async settlement may create arrears, but it must
					// not overflow the signed quota range.
					query = query.Where("quota >= ?", -common.MaxQuota+delta)
					result = query.Update("quota", gorm.Expr("quota - ?", delta))
				} else {
					credit := -delta
					query = query.Where("quota <= ?", common.MaxQuota-credit)
					result = query.Update("quota", gorm.Expr("quota + ?", credit))
				}
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("wallet task %d quota transition was rejected", taskId)
				}
			}

		default:
			return fmt.Errorf("%w: task %d has unsupported billing source %q",
				ErrTaskBillingEvidenceMissing, taskId, task.PrivateData.BillingSource)
		}

		result := tx.Model(&Task{}).
			Where("id = ? AND quota = ?", taskId, expectedQuota).
			Update("quota", targetQuota)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: task=%d expected=%d target=%d",
				ErrTaskQuotaTransitionConflict, taskId, expectedQuota, targetQuota)
		}
		if projection.ModelName == "" {
			if task.PrivateData.BillingContext != nil {
				projection.ModelName = task.PrivateData.BillingContext.OriginModelName
			}
			if projection.ModelName == "" {
				projection.ModelName = task.Properties.OriginModelName
			}
		}
		if projection.Group == "" {
			projection.Group = task.Group
		}
		if err := createTaskBillingAdjustmentTx(tx, &task, expectedQuota, targetQuota, projection); err != nil {
			return err
		}
		applied = true
		return nil
	})
	// Invalidating on an error is intentional: it covers both a normal
	// rollback (where deletion is harmless) and a commit-unknown result.
	if walletUserId > 0 {
		invalidateUserQuotaCacheAfterDBWrite(walletUserId, "async task quota transition")
	}
	if err != nil {
		return false, err
	}
	return applied, nil
}
