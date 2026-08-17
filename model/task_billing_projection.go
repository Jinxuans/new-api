package model

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskBillingProjectionInput struct {
	ModelName string
	Group     string
	Content   string
	Other     string
	NodeName  string
}

func TaskBillingAdjustmentOperationKey(taskId int64, expectedQuota int, targetQuota int) string {
	return fmt.Sprintf("taskadj:%d:%d:%d", taskId, expectedQuota, targetQuota)
}

// taskUsageTarget applies a task usage delta inside the persistent int32
// accounting range. Usage is secondary reporting data, so saturating an
// anomalous historical value is safer than overflowing it or leaving the
// financial journal permanently pending.
func taskUsageTarget(current int64, delta int64) (int64, bool) {
	const minQuota = int64(math.MinInt32)
	const maxQuota = int64(math.MaxInt32)

	if delta > 0 && current > math.MaxInt64-delta {
		return maxQuota, true
	}
	if delta < 0 && current < math.MinInt64-delta {
		return minQuota, true
	}
	next := current + delta
	if next > maxQuota {
		return maxQuota, true
	}
	if next < minQuota {
		return minQuota, true
	}
	return next, false
}

func createTaskBillingAdjustmentTx(tx *gorm.DB, task *Task, expectedQuota int, targetQuota int, projection TaskBillingProjectionInput) error {
	if tx == nil || task == nil {
		return errors.New("task billing adjustment requires a transaction and task")
	}
	delta := targetQuota - expectedQuota
	if delta == 0 {
		return nil
	}
	source := strings.TrimSpace(task.PrivateData.BillingSource)
	if source == "" {
		source = taskBillingSourceWallet
	}
	logType := LogTypeConsume
	logQuota := delta
	if delta < 0 {
		logType = LogTypeRefund
		logQuota = -delta
	}
	tokenRequired := false
	tokenUnlimited := false
	if task.PrivateData.TokenId > 0 {
		var token Token
		err := tx.Select("id", "unlimited_quota").Where("id = ?", task.PrivateData.TokenId).First(&token).Error
		switch {
		case err == nil:
			// Unlimited tokens still maintain remain/used accounting; they only
			// bypass the balance check when quota is reserved.
			tokenRequired = true
			tokenUnlimited = token.UnlimitedQuota
		case errors.Is(err, gorm.ErrRecordNotFound):
			// An async task can outlive the token that submitted it. Preserve the
			// historical behavior: settle the durable funding and usage without
			// leaving an unrecoverable token projection behind.
		default:
			return fmt.Errorf("load task billing token %d: %w", task.PrivateData.TokenId, err)
		}
	}
	operationKey := TaskBillingAdjustmentOperationKey(task.ID, expectedQuota, targetQuota)
	_, err := createBillingAdjustmentTx(tx, BillingAdjustmentInput{
		OperationKey:         operationKey,
		RequestId:            operationKey,
		Kind:                 BillingAdjustmentKindTaskProjection,
		FundingSource:        source,
		UserId:               task.UserId,
		TokenId:              task.PrivateData.TokenId,
		ModelName:            projection.ModelName,
		UsingGroup:           projection.Group,
		FundingDelta:         int64(delta),
		FundingTarget:        int64(targetQuota),
		TokenDelta:           int64(delta),
		FundingRequired:      false,
		TokenRequired:        tokenRequired,
		TokenUnlimited:       tokenUnlimited,
		TaskId:               task.ID,
		ChannelId:            task.ChannelId,
		UsageDelta:           int64(delta),
		UsageRequired:        task.UserId > 0 || task.ChannelId > 0,
		LogRequired:          true,
		ProjectionLogType:    logType,
		ProjectionLogQuota:   logQuota,
		ProjectionLogContent: projection.Content,
		ProjectionLogOther:   projection.Other,
		ProjectionNodeName:   projection.NodeName,
	})
	return err
}

func markBillingAdjustmentUsageAppliedTx(tx *gorm.DB, row *BillingAdjustmentJournal) error {
	if row.UsageApplied {
		return nil
	}
	row.UsageApplied = true
	status := billingAdjustmentStatusAfterApply(row)
	if err := tx.Model(&BillingAdjustmentJournal{}).
		Where("id = ? AND usage_applied = ?", row.ID, false).
		Updates(map[string]any{
			"usage_applied": true,
			"status":        status,
			"last_error":    "",
			"updated_at":    getDBTimestamp(tx),
		}).Error; err != nil {
		return err
	}
	row.Status = status
	return nil
}

func ApplyBillingUsageProjection(operationKey string) error {
	var saturationMessages []string
	err := DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if row.UsageApplied || !row.UsageRequired {
			return markBillingAdjustmentUsageAppliedTx(tx, row)
		}
		if row.Kind != BillingAdjustmentKindTaskProjection || row.UsageDelta == 0 {
			return errors.New("billing adjustment is not a task usage projection")
		}
		delta := row.UsageDelta
		if row.UserId > 0 {
			var user User
			err := lockForUpdate(tx.Unscoped()).
				Select("id", "used_quota").
				Where("id = ?", row.UserId).
				First(&user).Error
			switch {
			case err == nil:
				target, saturated := taskUsageTarget(int64(user.UsedQuota), delta)
				if saturated {
					saturationMessages = append(saturationMessages,
						fmt.Sprintf("task billing user usage saturated: operation=%s user=%d current=%d delta=%d target=%d", operationKey, row.UserId, user.UsedQuota, delta, target))
				}
				if err := tx.Unscoped().Model(&User{}).
					Where("id = ?", row.UserId).
					Update("used_quota", target).Error; err != nil {
					return err
				}
			case errors.Is(err, gorm.ErrRecordNotFound):
				// A task can finish after its owner was permanently deleted. The
				// remaining channel projection is still valid and must not roll back.
			default:
				return err
			}
		}
		if row.ChannelId > 0 {
			var channel Channel
			err := lockForUpdate(tx).
				Select("id", "used_quota").
				Where("id = ?", row.ChannelId).
				First(&channel).Error
			switch {
			case err == nil:
				target, saturated := taskUsageTarget(channel.UsedQuota, delta)
				if saturated {
					saturationMessages = append(saturationMessages,
						fmt.Sprintf("task billing channel usage saturated: operation=%s channel=%d current=%d delta=%d target=%d", operationKey, row.ChannelId, channel.UsedQuota, delta, target))
				}
				if err := tx.Model(&Channel{}).
					Where("id = ?", row.ChannelId).
					Update("used_quota", target).Error; err != nil {
					return err
				}
			case errors.Is(err, gorm.ErrRecordNotFound):
				// Channels are hard-deleted. Keep the user side and complete the
				// journal rather than retrying an impossible projection forever.
			default:
				return err
			}
		}
		return markBillingAdjustmentUsageAppliedTx(tx, row)
	})
	if err == nil {
		for _, message := range saturationMessages {
			common.SysError(message)
		}
	}
	return err
}

// RecordTaskInitialUsage persists the baseline that later task adjustment
// journals reconcile against. It deliberately bypasses the process-local
// batch updater: otherwise a restart could lose the initial quota while a
// durable settlement/refund delta survives and produces an invalid total.
func RecordTaskInitialUsage(userId int, channelId int, quota int) error {
	if userId <= 0 {
		return errors.New("task usage user is required")
	}
	if quota < 0 || quota > common.MaxQuota {
		return errors.New("task usage quota exceeds the persistent quota range")
	}

	var pendingReward *InvitationReward
	var saturationMessages []string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		pendingReward, err = QueueInvitationFirstRequestRewardTx(tx, userId)
		if err != nil {
			return err
		}

		var user User
		if err := lockForUpdate(tx).
			Select("id", "used_quota", "request_count").
			Where("id = ?", userId).
			First(&user).Error; err != nil {
			return err
		}
		usedQuota, usageSaturated := taskUsageTarget(int64(user.UsedQuota), int64(quota))
		requestCount, countSaturated := taskUsageTarget(int64(user.RequestCount), 1)
		if usageSaturated {
			saturationMessages = append(saturationMessages,
				fmt.Sprintf("initial task user usage saturated: user=%d current=%d delta=%d target=%d", userId, user.UsedQuota, quota, usedQuota))
		}
		if countSaturated {
			saturationMessages = append(saturationMessages,
				fmt.Sprintf("initial task request count saturated: user=%d current=%d target=%d", userId, user.RequestCount, requestCount))
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]any{
			"used_quota":    usedQuota,
			"request_count": requestCount,
		}).Error; err != nil {
			return err
		}

		if channelId <= 0 {
			return nil
		}
		var channel Channel
		err = lockForUpdate(tx).
			Select("id", "used_quota").
			Where("id = ?", channelId).
			First(&channel).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		usedChannelQuota, saturated := taskUsageTarget(channel.UsedQuota, int64(quota))
		if saturated {
			saturationMessages = append(saturationMessages,
				fmt.Sprintf("initial task channel usage saturated: channel=%d current=%d delta=%d target=%d", channelId, channel.UsedQuota, quota, usedChannelQuota))
		}
		return tx.Model(&Channel{}).
			Where("id = ?", channelId).
			Update("used_quota", usedChannelQuota).Error
	})
	if err != nil {
		return err
	}
	for _, message := range saturationMessages {
		common.SysError(message)
	}
	if pendingReward != nil && pendingReward.Status == InvitationRewardStatusPending {
		SettleInvitationFirstRequestReward(userId)
	}
	return nil
}

func ClaimBillingAdjustmentLogProjection(operationKey string, claimedBy string, lockUntil int64) (bool, error) {
	claimedBy = strings.TrimSpace(claimedBy)
	if claimedBy == "" || lockUntil <= GetDBTimestamp() {
		return false, errors.New("invalid billing log projection lease")
	}
	claimed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if row.LogApplied || !row.LogRequired {
			return nil
		}
		now := getDBTimestamp(tx)
		if row.LogClaimedBy != "" && row.LogClaimedUntil >= now {
			return nil
		}
		result := tx.Model(&BillingAdjustmentJournal{}).
			Where("id = ? AND log_applied = ? AND (log_claimed_until < ? OR log_claimed_by = ?)", row.ID, false, now, "").
			Updates(map[string]any{
				"log_claimed_by":    claimedBy,
				"log_claimed_until": lockUntil,
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		claimed = result.RowsAffected == 1
		return nil
	})
	return claimed, err
}

func CompleteBillingAdjustmentLogProjection(operationKey string, claimedBy string) error {
	claimedBy = strings.TrimSpace(claimedBy)
	snapshot, err := GetBillingAdjustment(operationKey)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return gorm.ErrRecordNotFound
	}
	if snapshot.Status == BillingAdjustmentStatusCanceled {
		return ErrBillingAdjustmentCanceled
	}
	if snapshot.LogApplied {
		return nil
	}
	if snapshot.LogClaimedBy != claimedBy {
		return errors.New("billing log projection lease was lost")
	}

	var persistedLog *Log
	if snapshot.ProjectionLogType != LogTypeConsume || common.LogConsumeEnabled {
		persistedLog, err = findTaskBillingProjectionLog(LOG_DB, snapshot, false)
		if err != nil {
			return fmt.Errorf("load persisted task billing log: %w", err)
		}
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockBillingAdjustmentTx(tx, operationKey)
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentStatusCanceled {
			return ErrBillingAdjustmentCanceled
		}
		if row.LogApplied {
			return nil
		}
		if row.LogClaimedBy != claimedBy {
			return errors.New("billing log projection lease was lost")
		}
		// The dashboard aggregate and the main journal marker share one
		// transaction. A crash or unknown commit can therefore replay both
		// without either dropping or double-counting this task projection.
		if row.ProjectionLogType == LogTypeConsume && common.LogConsumeEnabled && common.DataExportEnabled {
			if persistedLog == nil {
				return errors.New("task billing dashboard projection has no persisted log")
			}
			if err := applyTaskBillingQuotaDataTx(tx, row, persistedLog); err != nil {
				return err
			}
		}
		row.LogApplied = true
		status := billingAdjustmentStatusAfterApply(row)
		if err := tx.Model(&BillingAdjustmentJournal{}).
			Where("id = ? AND log_applied = ?", row.ID, false).
			Updates(map[string]any{
				"log_applied":       true,
				"log_claimed_by":    "",
				"log_claimed_until": 0,
				"status":            status,
				"last_error":        "",
				"updated_at":        getDBTimestamp(tx),
			}).Error; err != nil {
			return err
		}
		return nil
	})
}

func ReleaseBillingAdjustmentLogProjection(operationKey string, claimedBy string) error {
	return DB.Model(&BillingAdjustmentJournal{}).
		Where("operation_key = ? AND log_applied = ? AND log_claimed_by = ?", operationKey, false, strings.TrimSpace(claimedBy)).
		Updates(map[string]any{
			"log_claimed_by":    "",
			"log_claimed_until": 0,
			"updated_at":        GetDBTimestamp(),
		}).Error
}

func findTaskBillingProjectionLog(db *gorm.DB, row *BillingAdjustmentJournal, forUpdate bool) (*Log, error) {
	if db == nil || row == nil {
		return nil, errors.New("task billing log query requires a database and journal")
	}
	query := db.Where(map[string]any{
		"request_id": row.OperationKey,
		"user_id":    row.UserId,
		"type":       row.ProjectionLogType,
		"quota":      row.ProjectionLogQuota,
		"channel_id": row.ChannelId,
		"token_id":   row.TokenId,
		"model_name": row.ModelName,
		"group":      row.UsingGroup,
	})
	if forUpdate && (common.UsingLogDatabase(common.DatabaseTypeMySQL) || common.UsingLogDatabase(common.DatabaseTypePostgreSQL)) {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var log Log
	if err := query.Order("created_at ASC").Order("id ASC").Take(&log).Error; err != nil {
		return nil, err
	}
	return &log, nil
}

func applyTaskBillingQuotaDataTx(tx *gorm.DB, row *BillingAdjustmentJournal, log *Log) error {
	if tx == nil || row == nil || log == nil {
		return errors.New("task billing dashboard projection requires a transaction, journal and log")
	}
	if log.CreatedAt <= 0 {
		return errors.New("task billing log has no stable creation time")
	}
	createdAt := log.CreatedAt - (log.CreatedAt % 3600)
	nodeName := row.ProjectionNodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	where := "user_id = ? AND username = ? AND model_name = ? AND created_at = ? AND use_group = ? AND token_id = ? AND channel_id = ? AND node_name = ?"
	args := []any{row.UserId, log.Username, row.ModelName, createdAt, row.UsingGroup, row.TokenId, row.ChannelId, nodeName}

	var existing QuotaData
	err := lockForUpdate(tx).
		Where(where, args...).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&QuotaData{
			UserID:    row.UserId,
			Username:  log.Username,
			ModelName: row.ModelName,
			CreatedAt: createdAt,
			UseGroup:  row.UsingGroup,
			TokenID:   row.TokenId,
			ChannelID: row.ChannelId,
			NodeName:  nodeName,
			Count:     1,
			Quota:     row.ProjectionLogQuota,
		}).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&QuotaData{}).
		Where("id = ?", existing.Id).
		Updates(map[string]any{
			"count": gorm.Expr("count + ?", 1),
			"quota": gorm.Expr("quota + ?", row.ProjectionLogQuota),
		}).Error
}

// EnsureTaskBillingProjectionLog writes the externally stored log with the
// stable task adjustment identity as request_id. Relational log databases use
// a serializable predicate transaction, so even if the 60-second main-journal
// lease expires during a slow insert, concurrent workers cannot both commit a
// matching log. This avoids imposing global uniqueness on ordinary request IDs.
func EnsureTaskBillingProjectionLog(row *BillingAdjustmentJournal) error {
	if row == nil || !row.LogRequired || row.LogApplied {
		return nil
	}
	if row.Kind != BillingAdjustmentKindTaskProjection {
		return errors.New("billing adjustment is not a task log projection")
	}
	if row.ProjectionLogType == LogTypeConsume && !common.LogConsumeEnabled {
		return nil
	}

	username, _ := GetUsernameById(row.UserId, false)
	tokenName := ""
	if row.TokenId > 0 {
		if token, err := GetTokenById(row.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:    row.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      row.ProjectionLogType,
		Content:   row.ProjectionLogContent,
		TokenName: tokenName,
		ModelName: row.ModelName,
		Quota:     row.ProjectionLogQuota,
		ChannelId: row.ChannelId,
		TokenId:   row.TokenId,
		Group:     row.UsingGroup,
		RequestId: row.OperationKey,
		Other:     row.ProjectionLogOther,
	}
	insert := func(tx *gorm.DB, forUpdate bool) error {
		_, err := findTaskBillingProjectionLog(tx, row, forUpdate)
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(log).Error
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse does not provide the relational transaction/locking
		// contract. Preserve compatibility; the durable main marker and stable
		// projection identity still make crash recovery observable.
		return insert(LOG_DB, false)
	}
	return LOG_DB.Transaction(func(tx *gorm.DB) error {
		return insert(tx, true)
	}, &sql.TxOptions{Isolation: sql.LevelSerializable})
}
