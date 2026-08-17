package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingAdjustmentKindInitialReserve = "initial_reserve"
	BillingAdjustmentKindReserve        = "reserve"
	BillingAdjustmentKindUsageReserve   = "usage_reserve"
	BillingAdjustmentKindSettle         = "settle"
	BillingAdjustmentKindRefund         = "refund"
	BillingAdjustmentKindRollback       = "rollback"
	BillingAdjustmentKindTaskProjection = "task_projection"

	BillingAdjustmentStatusPending   = "pending"
	BillingAdjustmentStatusCompleted = "completed"
	BillingAdjustmentStatusCanceled  = "canceled"

	billingAdjustmentSourceWallet       = "wallet"
	billingAdjustmentSourceSubscription = "subscription"
)

var (
	ErrBillingAdjustmentConflict            = errors.New("billing adjustment operation conflicts with its persisted target")
	ErrBillingAdjustmentCanceled            = errors.New("billing adjustment operation is canceled")
	ErrBillingAdjustmentAlreadyCompleted    = errors.New("billing adjustment operation is already completed")
	ErrBillingAdjustmentFundingInsufficient = errors.New("billing adjustment funding quota is insufficient")
	ErrBillingAdjustmentTokenInsufficient   = errors.New("billing adjustment token quota is insufficient")
	ErrBillingAdjustmentQuotaOutOfRange     = errors.New("billing adjustment result is outside the persistent quota range")
	ErrBillingAdjustmentDispatchConflict    = errors.New("billing adjustment dispatch state conflicts with the requested operation")
)

const (
	billingAdjustmentRetryBaseSeconds int64 = 15
	billingAdjustmentRetryMaxSeconds  int64 = 60 * 60
)

// BillingAdjustmentJournal is the durable hand-off between a request's
// funding source and its secondary token accounting. Each side's mutation and
// applied marker commit in one database transaction, so a retry after an
// unknown commit result cannot apply the same wallet or token delta twice.
type BillingAdjustmentJournal struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	OperationKey         string `json:"operation_key" gorm:"type:varchar(191);uniqueIndex"`
	RequestId            string `json:"request_id" gorm:"type:varchar(128);index"`
	Kind                 string `json:"kind" gorm:"type:varchar(32);index"`
	FundingSource        string `json:"funding_source" gorm:"type:varchar(32);index"`
	UserId               int    `json:"user_id" gorm:"index"`
	TokenId              int    `json:"token_id" gorm:"index"`
	ModelName            string `json:"model_name" gorm:"type:varchar(255)"`
	UsingGroup           string `json:"using_group" gorm:"type:varchar(64)"`
	FundingDelta         int64  `json:"funding_delta"`
	FundingTarget        int64  `json:"funding_target"`
	TokenDelta           int64  `json:"token_delta"`
	FundingRequired      bool   `json:"funding_required"`
	TokenRequired        bool   `json:"token_required"`
	TokenEnforceBalance  bool   `json:"token_enforce_balance"`
	TokenUnlimited       bool   `json:"token_unlimited"`
	FundingApplied       bool   `json:"funding_applied" gorm:"index"`
	TokenApplied         bool   `json:"token_applied" gorm:"index"`
	DispatchRequired     bool   `json:"dispatch_required"`
	DispatchConfirmed    bool   `json:"dispatch_confirmed" gorm:"index"`
	TaskId               int64  `json:"task_id" gorm:"index"`
	ChannelId            int    `json:"channel_id" gorm:"index"`
	UsageDelta           int64  `json:"usage_delta"`
	UsageRequired        bool   `json:"usage_required"`
	UsageApplied         bool   `json:"usage_applied" gorm:"index"`
	LogRequired          bool   `json:"log_required"`
	LogApplied           bool   `json:"log_applied" gorm:"index"`
	LogClaimedBy         string `json:"log_claimed_by" gorm:"type:varchar(128);index"`
	LogClaimedUntil      int64  `json:"log_claimed_until" gorm:"bigint;index"`
	ProjectionLogType    int    `json:"projection_log_type"`
	ProjectionLogQuota   int    `json:"projection_log_quota"`
	ProjectionLogContent string `json:"projection_log_content" gorm:"type:text"`
	ProjectionLogOther   string `json:"projection_log_other" gorm:"type:text"`
	ProjectionNodeName   string `json:"projection_node_name" gorm:"type:varchar(128)"`
	Status               string `json:"status" gorm:"type:varchar(32);index"`
	LastError            string `json:"last_error" gorm:"type:text"`
	AttemptCount         int    `json:"attempt_count" gorm:"not null;default:0"`
	RecoverAfter         int64  `json:"recover_after" gorm:"bigint;index"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint;index"`
}

type BillingAdjustmentInput struct {
	OperationKey         string
	RequestId            string
	Kind                 string
	FundingSource        string
	UserId               int
	TokenId              int
	ModelName            string
	UsingGroup           string
	FundingDelta         int64
	FundingTarget        int64
	TokenDelta           int64
	FundingRequired      bool
	TokenRequired        bool
	TokenEnforceBalance  bool
	TokenUnlimited       bool
	DispatchRequired     bool
	DispatchConfirmed    bool
	TaskId               int64
	ChannelId            int
	UsageDelta           int64
	UsageRequired        bool
	LogRequired          bool
	ProjectionLogType    int
	ProjectionLogQuota   int
	ProjectionLogContent string
	ProjectionLogOther   string
	ProjectionNodeName   string
}

func (j *BillingAdjustmentJournal) BeforeCreate(tx *gorm.DB) error {
	now := getDBTimestamp(tx)
	if j.CreatedAt == 0 {
		j.CreatedAt = now
	}
	if j.UpdatedAt == 0 {
		j.UpdatedAt = now
	}
	return nil
}

func validateBillingAdjustmentInput(input BillingAdjustmentInput) error {
	input.OperationKey = strings.TrimSpace(input.OperationKey)
	input.RequestId = strings.TrimSpace(input.RequestId)
	if input.OperationKey == "" || len(input.OperationKey) > 191 {
		return errors.New("invalid billing adjustment operation key")
	}
	if input.RequestId == "" || len(input.RequestId) > 128 {
		return errors.New("invalid billing adjustment request id")
	}
	switch input.Kind {
	case BillingAdjustmentKindInitialReserve, BillingAdjustmentKindReserve, BillingAdjustmentKindUsageReserve,
		BillingAdjustmentKindSettle, BillingAdjustmentKindRefund, BillingAdjustmentKindRollback,
		BillingAdjustmentKindTaskProjection:
	default:
		return errors.New("invalid billing adjustment kind")
	}
	if input.DispatchRequired && input.Kind != BillingAdjustmentKindInitialReserve && input.Kind != BillingAdjustmentKindReserve {
		return errors.New("only an initial or extended pre-dispatch reserve can require dispatch confirmation")
	}
	if input.DispatchConfirmed && !input.DispatchRequired {
		return errors.New("only a dispatch-gated billing adjustment can be pre-confirmed")
	}
	if input.FundingRequired {
		if input.UserId <= 0 {
			return errors.New("billing adjustment funding user is required")
		}
		if input.FundingSource != billingAdjustmentSourceWallet && input.FundingSource != billingAdjustmentSourceSubscription {
			return errors.New("invalid billing adjustment funding source")
		}
	}
	if input.TokenRequired {
		if input.TokenId <= 0 || input.TokenDelta == 0 {
			return errors.New("invalid billing adjustment token target")
		}
	}
	if input.UsageRequired && input.UserId <= 0 && input.ChannelId <= 0 {
		return errors.New("billing adjustment usage target is missing")
	}
	if input.LogRequired {
		if input.TaskId <= 0 || (input.ProjectionLogType != LogTypeConsume && input.ProjectionLogType != LogTypeRefund) || input.ProjectionLogQuota <= 0 {
			return errors.New("invalid billing adjustment log projection")
		}
	}
	if input.FundingDelta > math.MaxInt32 || input.FundingDelta < math.MinInt32 ||
		input.FundingTarget > math.MaxInt32 || input.FundingTarget < 0 ||
		input.TokenDelta > math.MaxInt32 || input.TokenDelta < math.MinInt32 ||
		input.UsageDelta > math.MaxInt32 || input.UsageDelta < math.MinInt32 {
		return errors.New("billing adjustment target exceeds quota bounds")
	}
	return nil
}

func billingAdjustmentFromInput(input BillingAdjustmentInput) *BillingAdjustmentJournal {
	fundingApplied := !input.FundingRequired
	tokenApplied := !input.TokenRequired
	usageApplied := !input.UsageRequired
	logApplied := !input.LogRequired
	dispatchConfirmed := !input.DispatchRequired || input.DispatchConfirmed
	status := BillingAdjustmentStatusPending
	if fundingApplied && tokenApplied && usageApplied && logApplied && dispatchConfirmed {
		status = BillingAdjustmentStatusCompleted
	}
	return &BillingAdjustmentJournal{
		OperationKey:         strings.TrimSpace(input.OperationKey),
		RequestId:            strings.TrimSpace(input.RequestId),
		Kind:                 input.Kind,
		FundingSource:        input.FundingSource,
		UserId:               input.UserId,
		TokenId:              input.TokenId,
		ModelName:            input.ModelName,
		UsingGroup:           input.UsingGroup,
		FundingDelta:         input.FundingDelta,
		FundingTarget:        input.FundingTarget,
		TokenDelta:           input.TokenDelta,
		FundingRequired:      input.FundingRequired,
		TokenRequired:        input.TokenRequired,
		TokenEnforceBalance:  input.TokenEnforceBalance,
		TokenUnlimited:       input.TokenUnlimited,
		FundingApplied:       fundingApplied,
		TokenApplied:         tokenApplied,
		DispatchRequired:     input.DispatchRequired,
		DispatchConfirmed:    dispatchConfirmed,
		TaskId:               input.TaskId,
		ChannelId:            input.ChannelId,
		UsageDelta:           input.UsageDelta,
		UsageRequired:        input.UsageRequired,
		UsageApplied:         usageApplied,
		LogRequired:          input.LogRequired,
		LogApplied:           logApplied,
		ProjectionLogType:    input.ProjectionLogType,
		ProjectionLogQuota:   input.ProjectionLogQuota,
		ProjectionLogContent: input.ProjectionLogContent,
		ProjectionLogOther:   input.ProjectionLogOther,
		ProjectionNodeName:   input.ProjectionNodeName,
		Status:               status,
		RecoverAfter:         common.GetTimestamp() + 120,
	}
}

func billingAdjustmentMatchesInput(row *BillingAdjustmentJournal, input BillingAdjustmentInput) bool {
	return row != nil &&
		row.OperationKey == strings.TrimSpace(input.OperationKey) &&
		row.RequestId == strings.TrimSpace(input.RequestId) &&
		row.Kind == input.Kind &&
		row.FundingSource == input.FundingSource &&
		row.UserId == input.UserId &&
		row.TokenId == input.TokenId &&
		row.ModelName == input.ModelName &&
		row.UsingGroup == input.UsingGroup &&
		row.FundingDelta == input.FundingDelta &&
		row.FundingTarget == input.FundingTarget &&
		row.TokenDelta == input.TokenDelta &&
		row.FundingRequired == input.FundingRequired &&
		row.TokenRequired == input.TokenRequired &&
		row.TokenEnforceBalance == input.TokenEnforceBalance &&
		row.TokenUnlimited == input.TokenUnlimited &&
		row.DispatchRequired == input.DispatchRequired &&
		row.DispatchConfirmed == (!input.DispatchRequired || input.DispatchConfirmed) &&
		row.TaskId == input.TaskId &&
		row.ChannelId == input.ChannelId &&
		row.UsageDelta == input.UsageDelta &&
		row.UsageRequired == input.UsageRequired &&
		row.LogRequired == input.LogRequired &&
		row.ProjectionLogType == input.ProjectionLogType &&
		row.ProjectionLogQuota == input.ProjectionLogQuota &&
		row.ProjectionLogContent == input.ProjectionLogContent &&
		row.ProjectionLogOther == input.ProjectionLogOther &&
		row.ProjectionNodeName == input.ProjectionNodeName
}

func createBillingAdjustmentTx(tx *gorm.DB, input BillingAdjustmentInput) (*BillingAdjustmentJournal, error) {
	if err := validateBillingAdjustmentInput(input); err != nil {
		return nil, err
	}
	row := billingAdjustmentFromInput(input)
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "operation_key"}},
		DoNothing: true,
	}).Create(row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 1 {
		return row, nil
	}
	var existing BillingAdjustmentJournal
	if err := lockForUpdate(tx).Where("operation_key = ?", row.OperationKey).First(&existing).Error; err != nil {
		return nil, err
	}
	if !billingAdjustmentMatchesInput(&existing, input) {
		return nil, ErrBillingAdjustmentConflict
	}
	return &existing, nil
}

func CreateBillingAdjustment(input BillingAdjustmentInput) (*BillingAdjustmentJournal, error) {
	var row *BillingAdjustmentJournal
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		row, err = createBillingAdjustmentTx(tx, input)
		return err
	})
	return row, err
}

func GetBillingAdjustment(operationKey string) (*BillingAdjustmentJournal, error) {
	var row BillingAdjustmentJournal
	err := DB.Where("operation_key = ?", strings.TrimSpace(operationKey)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func ListPendingBillingAdjustments(limit int) ([]*BillingAdjustmentJournal, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var rows []*BillingAdjustmentJournal
	err := DB.Where("status = ? AND recover_after <= ?", BillingAdjustmentStatusPending, GetDBTimestamp()).
		// Fresh work is ordered ahead of repeatedly failing rows. Combined with
		// per-row backoff, an old poison batch cannot monopolize every recovery
		// run while newly due financial writes wait behind it.
		Order("attempt_count asc").
		Order("recover_after asc").
		Order("id asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func lockBillingAdjustmentTx(tx *gorm.DB, operationKey string) (*BillingAdjustmentJournal, error) {
	var row BillingAdjustmentJournal
	if err := lockForUpdate(tx).Where("operation_key = ?", strings.TrimSpace(operationKey)).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func billingAdjustmentStatusAfterApply(row *BillingAdjustmentJournal) string {
	if row.Status == BillingAdjustmentStatusCanceled {
		return BillingAdjustmentStatusCanceled
	}
	if row.FundingApplied && row.TokenApplied && row.UsageApplied && row.LogApplied &&
		(!row.DispatchRequired || row.DispatchConfirmed) {
		return BillingAdjustmentStatusCompleted
	}
	return BillingAdjustmentStatusPending
}

func billingAdjustmentBlocksApplyBeforeDispatch(row *BillingAdjustmentJournal) bool {
	return row != nil && row.DispatchRequired && !row.DispatchConfirmed &&
		row.Kind != BillingAdjustmentKindInitialReserve && row.Kind != BillingAdjustmentKindReserve
}

func markBillingAdjustmentFundingAppliedTx(tx *gorm.DB, row *BillingAdjustmentJournal) error {
	if row.FundingApplied {
		return nil
	}
	row.FundingApplied = true
	status := billingAdjustmentStatusAfterApply(row)
	if err := tx.Model(&BillingAdjustmentJournal{}).
		Where("id = ? AND funding_applied = ?", row.ID, false).
		Updates(map[string]any{
			"funding_applied": true,
			"status":          status,
			"last_error":      "",
			"updated_at":      getDBTimestamp(tx),
		}).Error; err != nil {
		return err
	}
	row.Status = status
	return nil
}

func markBillingAdjustmentTokenAppliedTx(tx *gorm.DB, row *BillingAdjustmentJournal) error {
	if row.TokenApplied {
		return nil
	}
	row.TokenApplied = true
	status := billingAdjustmentStatusAfterApply(row)
	if err := tx.Model(&BillingAdjustmentJournal{}).
		Where("id = ? AND token_applied = ?", row.ID, false).
		Updates(map[string]any{
			"token_applied": true,
			"status":        status,
			"last_error":    "",
			"updated_at":    getDBTimestamp(tx),
		}).Error; err != nil {
		return err
	}
	row.Status = status
	return nil
}

func MarkBillingAdjustmentFundingApplied(operationKey string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if billingAdjustmentBlocksApplyBeforeDispatch(row) {
			return ErrBillingAdjustmentDispatchConflict
		}
		return markBillingAdjustmentFundingAppliedTx(tx, row)
	})
}

// MarkBillingAdjustmentDispatchConfirmed records that the authorized request
// crossed the upstream dispatch boundary. Reservations that require this
// marker remain recoverable (rather than completed) until it is persisted.
func MarkBillingAdjustmentDispatchConfirmed(operationKey string) error {
	return MarkBillingAdjustmentsDispatchConfirmed([]string{operationKey})
}

// MarkBillingAdjustmentsDispatchConfirmed commits one request's dispatch
// authorization as a unit. This prevents an initial reservation and a later
// group-specific top-up from ending in different authorization states if the
// process stops between row updates.
func MarkBillingAdjustmentsDispatchConfirmed(operationKeys []string) error {
	keys := make([]string, 0, len(operationKeys))
	seen := make(map[string]struct{}, len(operationKeys))
	for _, operationKey := range operationKeys {
		operationKey = strings.TrimSpace(operationKey)
		if operationKey == "" {
			return ErrBillingAdjustmentDispatchConflict
		}
		if _, exists := seen[operationKey]; exists {
			continue
		}
		seen[operationKey] = struct{}{}
		keys = append(keys, operationKey)
	}
	if len(keys) == 0 {
		return nil
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var rows []*BillingAdjustmentJournal
		if err := lockForUpdate(tx).
			Where("operation_key IN ?", keys).
			Order("id asc").
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(keys) {
			return gorm.ErrRecordNotFound
		}
		requestId := rows[0].RequestId
		for _, row := range rows {
			if row.RequestId != requestId || !row.DispatchRequired {
				return ErrBillingAdjustmentDispatchConflict
			}
			if row.Status == BillingAdjustmentStatusCanceled {
				return ErrBillingAdjustmentCanceled
			}
		}
		for _, row := range rows {
			if row.DispatchConfirmed {
				continue
			}
			row.DispatchConfirmed = true
			status := billingAdjustmentStatusAfterApply(row)
			result := tx.Model(&BillingAdjustmentJournal{}).
				Where("id = ? AND dispatch_confirmed = ?", row.ID, false).
				Updates(map[string]any{
					"dispatch_confirmed": true,
					"status":             status,
					"last_error":         "",
					"updated_at":         getDBTimestamp(tx),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingAdjustmentConflict
			}
		}
		return nil
	})
}

// ApplyWalletBillingAdjustment applies the wallet delta and its journal marker
// atomically. A positive FundingDelta charges the wallet; a negative delta
// credits it. Positive reservations enforce the balance and refund-hold gate;
// later settlement adjustments may debit already-authorized usage into
// arrears, while realtime usage reserves fail instead so callers can stop the
// stream.
func ApplyWalletBillingAdjustment(operationKey string) error {
	var userId int
	var attempted bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if row.FundingApplied {
			return nil
		}
		if billingAdjustmentBlocksApplyBeforeDispatch(row) {
			return ErrBillingAdjustmentDispatchConflict
		}
		if !row.FundingRequired {
			return markBillingAdjustmentFundingAppliedTx(tx, row)
		}
		if row.FundingSource != billingAdjustmentSourceWallet || row.UserId <= 0 {
			return errors.New("billing adjustment is not a wallet operation")
		}
		userId = row.UserId
		attempted = true
		var user User
		if err := lockForUpdate(tx).
			Select("id", "quota", "refund_hold").
			Where("id = ?", row.UserId).
			First(&user).Error; err != nil {
			return err
		}
		currentQuota := int64(user.Quota)
		if currentQuota < int64(common.MinQuota) || currentQuota > int64(common.MaxQuota) {
			return ErrBillingAdjustmentQuotaOutOfRange
		}
		isPositiveReservation := row.FundingDelta > 0 &&
			(row.Kind == BillingAdjustmentKindInitialReserve ||
				row.Kind == BillingAdjustmentKindReserve ||
				row.Kind == BillingAdjustmentKindUsageReserve)
		if isPositiveReservation {
			// A refund hold blocks new spending authorization. UsageReserve is
			// different: the long-lived request already produced authoritative
			// usage, so it may consume available balance while the hold prevents
			// the stream from starting any new request.
			if user.RefundHold && row.Kind != BillingAdjustmentKindUsageReserve {
				return ErrUserRefundHeld
			}
			if currentQuota < row.FundingDelta {
				return ErrBillingAdjustmentFundingInsufficient
			}
		}
		nextQuota := currentQuota - row.FundingDelta
		if nextQuota < int64(common.MinQuota) || nextQuota > int64(common.MaxQuota) {
			return ErrBillingAdjustmentQuotaOutOfRange
		}
		if nextQuota != currentQuota {
			result := tx.Model(&User{}).
				Where("id = ? AND quota = ?", row.UserId, user.Quota).
				Update("quota", int(nextQuota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingAdjustmentConflict
			}
		}
		return markBillingAdjustmentFundingAppliedTx(tx, row)
	})
	if attempted || userId > 0 {
		invalidateUserQuotaCacheAfterDBWrite(userId, "billing adjustment")
	}
	return err
}

// ApplyTokenBillingAdjustment atomically changes token remain/used quota and
// marks that side applied. Positive TokenDelta is a charge; negative is a
// refund. TokenEnforceBalance is used only for pre-reservation operations.
func ApplyTokenBillingAdjustment(operationKey string) error {
	row, err := GetBillingAdjustment(operationKey)
	if err != nil {
		return err
	}
	if row == nil {
		return gorm.ErrRecordNotFound
	}
	if billingAdjustmentBlocksApplyBeforeDispatch(row) {
		return ErrBillingAdjustmentDispatchConflict
	}
	if row.TokenApplied || !row.TokenRequired {
		return DB.Transaction(func(tx *gorm.DB) error {
			locked, lockErr := lockBillingAdjustmentTx(tx, operationKey)
			if lockErr != nil {
				return lockErr
			}
			if locked.Status == BillingAdjustmentStatusCanceled {
				return ErrBillingAdjustmentCanceled
			}
			return markBillingAdjustmentTokenAppliedTx(tx, locked)
		})
	}

	var token Token
	if err := DB.Unscoped().
		Select("id", "user_id", "key", "deleted_at").
		Where("id = ?", row.TokenId).
		First(&token).Error; err != nil {
		return err
	}
	if row.UserId <= 0 || token.UserId != row.UserId {
		return fmt.Errorf("%w: token %d owner does not match user %d", ErrBillingAdjustmentConflict, row.TokenId, row.UserId)
	}
	// Fence the cache before the database mutation. The fence intentionally
	// survives the transaction, covering both a successful commit and an
	// unknown commit result. It is also safe for a deleted token and covers a
	// concurrent restore before the locked database check below.
	if err := invalidateTokenCacheForMutation(token.Key); err != nil {
		common.SysError("failed to fence token cache for billing adjustment: " + err.Error())
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if locked.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if locked.TokenApplied {
			return nil
		}
		if billingAdjustmentBlocksApplyBeforeDispatch(locked) {
			return ErrBillingAdjustmentDispatchConflict
		}
		if !locked.TokenRequired {
			return markBillingAdjustmentTokenAppliedTx(tx, locked)
		}

		var lockedToken Token
		if err := lockForUpdate(tx.Unscoped()).
			Select("id", "user_id", "remain_quota", "used_quota", "deleted_at").
			Where("id = ?", locked.TokenId).
			First(&lockedToken).Error; err != nil {
			return err
		}
		if locked.UserId <= 0 || lockedToken.UserId != locked.UserId {
			return fmt.Errorf("%w: token %d owner does not match user %d", ErrBillingAdjustmentConflict, locked.TokenId, locked.UserId)
		}
		if lockedToken.DeletedAt.Valid {
			switch locked.Kind {
			case BillingAdjustmentKindSettle, BillingAdjustmentKindRefund,
				BillingAdjustmentKindRollback, BillingAdjustmentKindTaskProjection:
				// Once a token is soft-deleted there is no active token balance to
				// project. The durable funding intent still stands, so recording the
				// token marker is the only replay-safe terminal state.
				return markBillingAdjustmentTokenAppliedTx(tx, locked)
			default:
				return gorm.ErrRecordNotFound
			}
		}

		currentRemain := int64(lockedToken.RemainQuota)
		currentUsed := int64(lockedToken.UsedQuota)
		if currentRemain < int64(common.MinQuota) || currentRemain > int64(common.MaxQuota) ||
			currentUsed < int64(common.MinQuota) || currentUsed > int64(common.MaxQuota) {
			return ErrBillingAdjustmentQuotaOutOfRange
		}
		isPositiveReservation := locked.TokenDelta > 0 &&
			(locked.Kind == BillingAdjustmentKindInitialReserve ||
				locked.Kind == BillingAdjustmentKindReserve ||
				locked.Kind == BillingAdjustmentKindUsageReserve)
		enforceBalance := locked.TokenEnforceBalance || isPositiveReservation
		if locked.TokenDelta > 0 && enforceBalance && !locked.TokenUnlimited && currentRemain < locked.TokenDelta {
			return ErrBillingAdjustmentTokenInsufficient
		}
		nextRemain := currentRemain - locked.TokenDelta
		nextUsed := currentUsed + locked.TokenDelta
		if nextRemain < int64(common.MinQuota) || nextRemain > int64(common.MaxQuota) ||
			nextUsed < int64(common.MinQuota) || nextUsed > int64(common.MaxQuota) {
			return ErrBillingAdjustmentQuotaOutOfRange
		}
		result := tx.Model(&Token{}).
			Where("id = ? AND remain_quota = ? AND used_quota = ?", locked.TokenId, lockedToken.RemainQuota, lockedToken.UsedQuota).
			Updates(map[string]any{
				"remain_quota":  int(nextRemain),
				"used_quota":    int(nextUsed),
				"accessed_time": getDBTimestamp(tx),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingAdjustmentConflict
		}
		return markBillingAdjustmentTokenAppliedTx(tx, locked)
	})
}

// ApplySubscriptionBillingAdjustment runs the request-scoped subscription
// lifecycle transition and journal marker in the same transaction.
func ApplySubscriptionBillingAdjustment(operationKey string) (*SubscriptionPreConsumeResult, error) {
	var result *SubscriptionPreConsumeResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if billingAdjustmentBlocksApplyBeforeDispatch(row) {
			return ErrBillingAdjustmentDispatchConflict
		}
		if !row.FundingRequired {
			return markBillingAdjustmentFundingAppliedTx(tx, row)
		}
		if row.FundingSource != billingAdjustmentSourceSubscription {
			return errors.New("billing adjustment is not a subscription operation")
		}

		if row.FundingApplied {
			if row.Kind == BillingAdjustmentKindRefund {
				return nil
			}
			record, loadErr := lockSubscriptionPreConsumeRecordTx(tx, row.RequestId)
			if loadErr != nil {
				return loadErr
			}
			result, loadErr = loadSubscriptionPreConsumeResultTx(tx, record)
			return loadErr
		}

		switch row.Kind {
		case BillingAdjustmentKindInitialReserve:
			result, err = PreConsumeUserSubscriptionTx(tx, row.RequestId, row.UserId, row.ModelName, row.UsingGroup, 0, row.FundingTarget)
		case BillingAdjustmentKindReserve, BillingAdjustmentKindUsageReserve:
			result, err = ReserveSubscriptionPreConsumeTx(tx, row.RequestId, row.FundingTarget)
		case BillingAdjustmentKindSettle:
			result, err = SettleSubscriptionPreConsumeTx(tx, row.RequestId, row.FundingTarget)
		case BillingAdjustmentKindRefund:
			err = RefundSubscriptionPreConsumeTx(tx, row.RequestId)
		default:
			err = fmt.Errorf("unsupported subscription billing adjustment kind: %s", row.Kind)
		}
		if err != nil {
			return err
		}
		return markBillingAdjustmentFundingAppliedTx(tx, row)
	})
	return result, err
}

func RecordBillingAdjustmentError(operationKey string, adjustmentErr error) error {
	if adjustmentErr == nil {
		return nil
	}
	message := adjustmentErr.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.Status != BillingAdjustmentStatusPending {
			return nil
		}

		attemptCount := row.AttemptCount + 1
		if row.AttemptCount < 0 {
			attemptCount = 1
		} else if row.AttemptCount >= common.MaxQuota {
			attemptCount = common.MaxQuota
		}
		delay := billingAdjustmentRetryBaseSeconds
		// Shifting only up to the point where the configured cap is reached
		// avoids integer overflow even for a corrupted legacy attempt count.
		for retry := 1; retry < attemptCount && delay < billingAdjustmentRetryMaxSeconds; retry++ {
			delay *= 2
			if delay > billingAdjustmentRetryMaxSeconds {
				delay = billingAdjustmentRetryMaxSeconds
			}
		}
		now := getDBTimestamp(tx)
		return tx.Model(&BillingAdjustmentJournal{}).
			Where("id = ? AND status = ?", row.ID, BillingAdjustmentStatusPending).
			Updates(map[string]any{
				"last_error":    message,
				"attempt_count": attemptCount,
				"recover_after": now + delay,
				"updated_at":    now,
			}).Error
	})
}

// CancelBillingReservationAfterFinalIntent closes an undispatched
// pre-authorization after a durable settlement/refund intent for the same
// request exists. The final journal owns any necessary compensation, so this
// path deliberately does not create the token-only rollback used for an
// authorization failure.
func CancelBillingReservationAfterFinalIntent(operationKey string, finalOperationKey string) error {
	operationKey = strings.TrimSpace(operationKey)
	finalOperationKey = strings.TrimSpace(finalOperationKey)
	if operationKey == "" || finalOperationKey == "" || operationKey == finalOperationKey {
		return ErrBillingAdjustmentDispatchConflict
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var rows []*BillingAdjustmentJournal
		if err := lockForUpdate(tx).
			Where("operation_key IN ?", []string{operationKey, finalOperationKey}).
			Order("id asc").
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 2 {
			return gorm.ErrRecordNotFound
		}
		rowsByKey := make(map[string]*BillingAdjustmentJournal, len(rows))
		for _, row := range rows {
			rowsByKey[row.OperationKey] = row
		}
		reservation := rowsByKey[operationKey]
		finalIntent := rowsByKey[finalOperationKey]
		if reservation == nil || finalIntent == nil {
			return gorm.ErrRecordNotFound
		}
		if reservation.Kind != BillingAdjustmentKindInitialReserve && reservation.Kind != BillingAdjustmentKindReserve {
			return ErrBillingAdjustmentDispatchConflict
		}
		if finalIntent.RequestId != reservation.RequestId ||
			(finalIntent.Kind != BillingAdjustmentKindSettle && finalIntent.Kind != BillingAdjustmentKindRefund) ||
			finalIntent.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentDispatchConflict
		}
		if reservation.Status == BillingAdjustmentStatusCanceled {
			return nil
		}
		if !reservation.DispatchRequired || reservation.DispatchConfirmed {
			return ErrBillingAdjustmentDispatchConflict
		}

		result := tx.Model(&BillingAdjustmentJournal{}).
			Where("id = ? AND status <> ? AND dispatch_confirmed = ?", reservation.ID, BillingAdjustmentStatusCanceled, false).
			Updates(map[string]any{
				"status":     BillingAdjustmentStatusCanceled,
				"last_error": "superseded by final billing intent " + finalOperationKey,
				"updated_at": getDBTimestamp(tx),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingAdjustmentConflict
		}
		return nil
	})
}

// CancelUndispatchedBillingReservationWithRefund atomically closes an
// authorization that never crossed the dispatch boundary and creates the
// exact reversal for every side that was already pre-applied. The returned
// refund can be replayed independently after a process or database failure.
func CancelUndispatchedBillingReservationWithRefund(operationKey string, refundOperationKey string, reason error) (*BillingAdjustmentJournal, error) {
	operationKey = strings.TrimSpace(operationKey)
	refundOperationKey = strings.TrimSpace(refundOperationKey)
	if operationKey == "" || refundOperationKey == "" || operationKey == refundOperationKey {
		return nil, ErrBillingAdjustmentDispatchConflict
	}

	var refund *BillingAdjustmentJournal
	err := DB.Transaction(func(tx *gorm.DB) error {
		reservation, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if reservation.Kind != BillingAdjustmentKindInitialReserve && reservation.Kind != BillingAdjustmentKindReserve {
			return ErrBillingAdjustmentDispatchConflict
		}
		if !reservation.DispatchRequired || reservation.DispatchConfirmed {
			return ErrBillingAdjustmentDispatchConflict
		}
		if reservation.Status == BillingAdjustmentStatusCanceled {
			var existing BillingAdjustmentJournal
			err := tx.Where("operation_key = ?", refundOperationKey).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			refund = &existing
			return nil
		}

		fundingDelta := int64(0)
		fundingRequired := reservation.FundingRequired && reservation.FundingApplied && reservation.FundingDelta != 0
		if fundingRequired {
			fundingDelta = -reservation.FundingDelta
		}
		tokenDelta := int64(0)
		tokenRequired := reservation.TokenRequired && reservation.TokenApplied && reservation.TokenDelta != 0
		if tokenRequired {
			tokenDelta = -reservation.TokenDelta
		}
		refund, err = createBillingAdjustmentTx(tx, BillingAdjustmentInput{
			OperationKey:     refundOperationKey,
			RequestId:        reservation.RequestId,
			Kind:             BillingAdjustmentKindRefund,
			FundingSource:    reservation.FundingSource,
			UserId:           reservation.UserId,
			TokenId:          reservation.TokenId,
			ModelName:        reservation.ModelName,
			UsingGroup:       reservation.UsingGroup,
			FundingDelta:     fundingDelta,
			TokenDelta:       tokenDelta,
			FundingRequired:  fundingRequired,
			TokenRequired:    tokenRequired,
			TokenUnlimited:   reservation.TokenUnlimited,
			DispatchRequired: false,
		})
		if err != nil {
			return err
		}

		message := "request ended before upstream dispatch authorization"
		if reason != nil {
			message = reason.Error()
			if len(message) > 2000 {
				message = message[:2000]
			}
		}
		result := tx.Model(&BillingAdjustmentJournal{}).
			Where("id = ? AND status <> ? AND dispatch_confirmed = ?", reservation.ID, BillingAdjustmentStatusCanceled, false).
			Updates(map[string]any{
				"status":     BillingAdjustmentStatusCanceled,
				"last_error": message,
				"updated_at": getDBTimestamp(tx),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingAdjustmentConflict
		}
		return nil
	})
	return refund, err
}

// CancelBillingAdjustmentWithRollback closes a reservation that never reached
// its funding side and creates one idempotent token compensation when needed.
// If the funding side committed despite an unknown result, cancellation is
// rejected so the caller can replay the now-completed operation instead.
func CancelBillingAdjustmentWithRollback(operationKey string, rollbackOperationKey string, reason error) (*BillingAdjustmentJournal, error) {
	var rollback *BillingAdjustmentJournal
	err := DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.FundingApplied {
			if row.TokenApplied {
				row.FundingApplied = true
				row.TokenApplied = true
				status := billingAdjustmentStatusAfterApply(row)
				if row.Status != status {
					if err := tx.Model(&BillingAdjustmentJournal{}).Where("id = ?", row.ID).Updates(map[string]any{
						"status":     status,
						"updated_at": getDBTimestamp(tx),
					}).Error; err != nil {
						return err
					}
				}
			}
			return ErrBillingAdjustmentAlreadyCompleted
		}
		if row.Kind != BillingAdjustmentKindInitialReserve &&
			row.Kind != BillingAdjustmentKindReserve &&
			row.Kind != BillingAdjustmentKindUsageReserve {
			return errors.New("only reservation adjustments can be canceled with rollback")
		}

		message := ""
		if reason != nil {
			message = reason.Error()
			if len(message) > 2000 {
				message = message[:2000]
			}
		}
		if row.Status != BillingAdjustmentStatusCanceled {
			if err := tx.Model(&BillingAdjustmentJournal{}).Where("id = ?", row.ID).Updates(map[string]any{
				"status":     BillingAdjustmentStatusCanceled,
				"last_error": message,
				"updated_at": getDBTimestamp(tx),
			}).Error; err != nil {
				return err
			}
		}
		if !row.TokenApplied || row.TokenDelta == 0 {
			return nil
		}
		rollback, err = createBillingAdjustmentTx(tx, BillingAdjustmentInput{
			OperationKey:    rollbackOperationKey,
			RequestId:       row.RequestId,
			Kind:            BillingAdjustmentKindRollback,
			FundingSource:   row.FundingSource,
			UserId:          row.UserId,
			TokenId:         row.TokenId,
			TokenDelta:      -row.TokenDelta,
			FundingRequired: false,
			TokenRequired:   true,
		})
		return err
	})
	return rollback, err
}
