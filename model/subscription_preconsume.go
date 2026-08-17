package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	subscriptionPreConsumeRequestIdMaxLength = 64

	SubscriptionPreConsumeStatusReserved = "reserved"
	SubscriptionPreConsumeStatusSettled  = "settled"
	SubscriptionPreConsumeStatusRefunded = "refunded"

	// consumed was the pre-lifecycle status used by older installations. It is
	// treated as reserved until the request is settled or refunded.
	subscriptionPreConsumeStatusLegacyConsumed = "consumed"
)

func normalizeSubscriptionPreConsumeRequestId(requestId string) (string, error) {
	requestId = strings.TrimSpace(requestId)
	if requestId == "" {
		return "", errors.New("requestId is empty")
	}
	if len(requestId) > subscriptionPreConsumeRequestIdMaxLength {
		return "", errors.New("requestId is too long")
	}
	return requestId, nil
}

var (
	ErrSubscriptionPreConsumeConflict          = errors.New("subscription pre-consume request conflicts with its persisted lifecycle")
	ErrSubscriptionPreConsumeFinalized         = errors.New("subscription pre-consume request is already finalized")
	ErrSubscriptionPreConsumeGenerationChanged = errors.New("subscription quota generation changed during the request")
	ErrSubscriptionUsageUnderflow              = errors.New("subscription used quota would become negative")
)

func subscriptionPreConsumeIsReserved(status string) bool {
	return status == SubscriptionPreConsumeStatusReserved || status == subscriptionPreConsumeStatusLegacyConsumed
}

func advanceSubscriptionQuotaGeneration(sub *UserSubscription) error {
	if sub == nil || sub.QuotaGeneration == math.MaxInt64 {
		return errors.New("subscription quota generation overflow")
	}
	sub.QuotaGeneration++
	return nil
}

func applySubscriptionUsageDeltaTx(tx *gorm.DB, sub *UserSubscription, delta int64) error {
	if tx == nil || sub == nil || sub.Id <= 0 {
		return errors.New("invalid subscription usage update")
	}
	if sub.AmountUsed < 0 || sub.AmountTotal < 0 {
		return errors.New("subscription contains invalid quota values")
	}
	if delta == 0 {
		return nil
	}
	if delta > 0 && sub.AmountUsed > math.MaxInt64-delta {
		return errors.New("subscription used quota overflow")
	}
	if delta < 0 && delta < -sub.AmountUsed {
		return fmt.Errorf("%w: used=%d delta=%d", ErrSubscriptionUsageUnderflow, sub.AmountUsed, delta)
	}
	newUsed := sub.AmountUsed + delta
	if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
		return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
	}
	sub.AmountUsed = newUsed
	return tx.Save(sub).Error
}

func lockSubscriptionPreConsumeRecordTx(tx *gorm.DB, requestId string) (*SubscriptionPreConsumeRecord, error) {
	var record SubscriptionPreConsumeRecord
	if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func lockSubscriptionForPreConsumeRecordTx(tx *gorm.DB, record *SubscriptionPreConsumeRecord) (*UserSubscription, error) {
	if record == nil || record.UserSubscriptionId <= 0 || record.UserId <= 0 {
		return nil, ErrSubscriptionPreConsumeConflict
	}
	var sub UserSubscription
	if err := lockForUpdate(tx).
		Where("id = ? AND user_id = ?", record.UserSubscriptionId, record.UserId).
		First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func buildSubscriptionPreConsumeResult(record *SubscriptionPreConsumeRecord, sub *UserSubscription, usedBefore int64) *SubscriptionPreConsumeResult {
	return &SubscriptionPreConsumeResult{
		UserSubscriptionId: sub.Id,
		PreConsumed:        record.PreConsumed,
		FinalConsumed:      record.FinalConsumed,
		QuotaGeneration:    record.QuotaGeneration,
		Status:             record.Status,
		GenerationMatched:  record.QuotaGeneration == sub.QuotaGeneration,
		AmountTotal:        sub.AmountTotal,
		AmountUsedBefore:   usedBefore,
		AmountUsedAfter:    sub.AmountUsed,
	}
}

func loadSubscriptionPreConsumeResultTx(tx *gorm.DB, record *SubscriptionPreConsumeRecord) (*SubscriptionPreConsumeResult, error) {
	sub, err := lockSubscriptionForPreConsumeRecordTx(tx, record)
	if err != nil {
		return nil, err
	}
	return buildSubscriptionPreConsumeResult(record, sub, sub.AmountUsed), nil
}

// createSubscriptionPreConsumeRecordTx inserts without turning a duplicate-key
// race into an aborted PostgreSQL transaction. The caller has not changed the
// subscription yet, so losing the race remains a clean idempotent replay.
func createSubscriptionPreConsumeRecordTx(tx *gorm.DB, record *SubscriptionPreConsumeRecord) (bool, *SubscriptionPreConsumeRecord, error) {
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "request_id"}},
		DoNothing: true,
	}).Create(record)
	if result.Error != nil {
		return false, nil, result.Error
	}
	if result.RowsAffected == 1 {
		return true, record, nil
	}
	existing, err := lockSubscriptionPreConsumeRecordTx(tx, record.RequestId)
	if err != nil {
		return false, nil, err
	}
	return false, existing, nil
}

// ReserveSubscriptionPreConsume raises a request's total reservation to
// targetReserved. The cumulative target makes retries idempotent while still
// allowing long-lived requests to extend their reservation.
func ReserveSubscriptionPreConsume(requestId string, targetReserved int64) (*SubscriptionPreConsumeResult, error) {
	var reserveResult *SubscriptionPreConsumeResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		reserveResult, err = ReserveSubscriptionPreConsumeTx(tx, requestId, targetReserved)
		return err
	})
	if err != nil {
		return nil, err
	}
	return reserveResult, nil
}

// ReserveSubscriptionPreConsumeTx is the caller-owned transaction variant
// used when reservation state must commit atomically with another durable row.
func ReserveSubscriptionPreConsumeTx(tx *gorm.DB, requestId string, targetReserved int64) (*SubscriptionPreConsumeResult, error) {
	if tx == nil {
		return nil, errors.New("database transaction is required")
	}
	requestId, err := normalizeSubscriptionPreConsumeRequestId(requestId)
	if err != nil {
		return nil, err
	}
	if targetReserved <= 0 {
		return nil, errors.New("targetReserved must be > 0")
	}

	record, err := lockSubscriptionPreConsumeRecordTx(tx, requestId)
	if err != nil {
		return nil, err
	}
	if !subscriptionPreConsumeIsReserved(record.Status) {
		return nil, ErrSubscriptionPreConsumeFinalized
	}
	if targetReserved < record.PreConsumed {
		return nil, ErrSubscriptionPreConsumeConflict
	}
	sub, err := lockSubscriptionForPreConsumeRecordTx(tx, record)
	if err != nil {
		return nil, err
	}
	if record.QuotaGeneration != sub.QuotaGeneration {
		return nil, ErrSubscriptionPreConsumeGenerationChanged
	}
	usedBefore := sub.AmountUsed
	if targetReserved > record.PreConsumed {
		delta := targetReserved - record.PreConsumed
		if err := applySubscriptionUsageDeltaTx(tx, sub, delta); err != nil {
			return nil, err
		}
		record.PreConsumed = targetReserved
		record.Status = SubscriptionPreConsumeStatusReserved
		if err := tx.Save(record).Error; err != nil {
			return nil, err
		}
	}
	return buildSubscriptionPreConsumeResult(record, sub, usedBefore), nil
}

// SettleSubscriptionPreConsume reconciles the request to an exact usage target.
// Repeating a target is idempotent, while a later asynchronous result may
// revise an earlier estimate. A reset generation change makes the old request
// economically obsolete, so only its persisted lifecycle is updated.
func SettleSubscriptionPreConsume(requestId string, finalConsumed int64) (*SubscriptionPreConsumeResult, error) {
	var settleResult *SubscriptionPreConsumeResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		settleResult, err = SettleSubscriptionPreConsumeTx(tx, requestId, finalConsumed)
		return err
	})
	if err != nil {
		return nil, err
	}
	return settleResult, nil
}

// SettleSubscriptionPreConsumeTx atomically reconciles the amount currently
// applied for requestId to finalConsumed. A settled estimate may be revised by
// an asynchronous terminal result; repeating the same target is a no-op.
func SettleSubscriptionPreConsumeTx(tx *gorm.DB, requestId string, finalConsumed int64) (*SubscriptionPreConsumeResult, error) {
	if tx == nil {
		return nil, errors.New("database transaction is required")
	}
	requestId, err := normalizeSubscriptionPreConsumeRequestId(requestId)
	if err != nil {
		return nil, err
	}
	if finalConsumed < 0 {
		return nil, errors.New("finalConsumed must not be negative")
	}

	record, err := lockSubscriptionPreConsumeRecordTx(tx, requestId)
	if err != nil {
		return nil, err
	}
	var appliedAmount int64
	switch record.Status {
	case SubscriptionPreConsumeStatusSettled:
		if record.FinalConsumed < 0 {
			return nil, ErrSubscriptionPreConsumeConflict
		}
		appliedAmount = record.FinalConsumed
	case SubscriptionPreConsumeStatusRefunded:
		return nil, ErrSubscriptionPreConsumeFinalized
	default:
		if !subscriptionPreConsumeIsReserved(record.Status) || record.PreConsumed < 0 {
			return nil, ErrSubscriptionPreConsumeConflict
		}
		appliedAmount = record.PreConsumed
	}

	sub, err := lockSubscriptionForPreConsumeRecordTx(tx, record)
	if err != nil {
		return nil, err
	}
	usedBefore := sub.AmountUsed
	if record.QuotaGeneration == sub.QuotaGeneration {
		if err := applySubscriptionUsageDeltaTx(tx, sub, finalConsumed-appliedAmount); err != nil {
			return nil, err
		}
	}
	if record.Status != SubscriptionPreConsumeStatusSettled || record.FinalConsumed != finalConsumed {
		record.FinalConsumed = finalConsumed
		record.Status = SubscriptionPreConsumeStatusSettled
		if err := tx.Save(record).Error; err != nil {
			return nil, err
		}
	}
	return buildSubscriptionPreConsumeResult(record, sub, usedBefore), nil
}

// RefundSubscriptionPreConsume refunds the entire persisted reservation once.
// The record and subscription are locked and changed in one transaction; if a
// reset advanced the generation, only the lifecycle is finalized.
func RefundSubscriptionPreConsume(requestId string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return RefundSubscriptionPreConsumeTx(tx, requestId)
	})
}

// RefundSubscriptionPreConsumeTx refunds the currently applied amount through
// the caller's transaction. It supports both an open reservation and a settled
// task estimate, then makes subsequent retries no-ops.
func RefundSubscriptionPreConsumeTx(tx *gorm.DB, requestId string) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	requestId, err := normalizeSubscriptionPreConsumeRequestId(requestId)
	if err != nil {
		return err
	}
	record, err := lockSubscriptionPreConsumeRecordTx(tx, requestId)
	if err != nil {
		return err
	}
	var appliedAmount int64
	switch record.Status {
	case SubscriptionPreConsumeStatusRefunded:
		return nil
	case SubscriptionPreConsumeStatusSettled:
		appliedAmount = record.FinalConsumed
	default:
		if !subscriptionPreConsumeIsReserved(record.Status) {
			return ErrSubscriptionPreConsumeConflict
		}
		appliedAmount = record.PreConsumed
	}
	if appliedAmount < 0 {
		return ErrSubscriptionPreConsumeConflict
	}

	sub, err := lockSubscriptionForPreConsumeRecordTx(tx, record)
	if err != nil {
		return err
	}
	if record.QuotaGeneration == sub.QuotaGeneration && appliedAmount > 0 {
		if err := applySubscriptionUsageDeltaTx(tx, sub, -appliedAmount); err != nil {
			return err
		}
	}
	record.FinalConsumed = 0
	record.Status = SubscriptionPreConsumeStatusRefunded
	return tx.Save(record).Error
}

// PostConsumeUserSubscriptionDelta remains for legacy callers that do not own
// a request lifecycle. Negative deltas are strict: corrupt or duplicate
// refunds fail instead of consuming another request's usage through clamping.
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) error {
	if userSubscriptionId <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		return applySubscriptionUsageDeltaTx(tx, &sub, delta)
	})
}

// CleanupSubscriptionPreConsumeRecords removes only refunded lifecycles.
// Reserved rows are active state, and settled rows remain revisable evidence
// for asynchronous task reconciliation, so neither can be age-deleted safely.
func CleanupSubscriptionPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		olderThanSeconds = 7 * 24 * 3600
	}
	cutoff := GetDBTimestamp() - olderThanSeconds
	res := DB.Where("updated_at < ? AND status = ?", cutoff, SubscriptionPreConsumeStatusRefunded).
		Delete(&SubscriptionPreConsumeRecord{})
	return res.RowsAffected, res.Error
}
