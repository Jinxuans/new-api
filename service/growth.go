package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

type GrowthSummary struct {
	// Legacy aggregate fields retained for API compatibility. They mix task
	// rewards and referral credits; new clients should use the split fields.
	AvailableRewardQuota         int64                      `json:"available_reward_quota"`
	PendingRewardQuota           int64                      `json:"pending_reward_quota"`
	TotalRewardQuota             int64                      `json:"total_reward_quota"`
	TaskRewardEarnedQuota        int64                      `json:"task_reward_earned_quota"`
	TaskRewardPendingQuota       int64                      `json:"task_reward_pending_quota"`
	ReferralCreditAvailableQuota int64                      `json:"referral_credit_available_quota"`
	ReferralCreditTotalQuota     int64                      `json:"referral_credit_total_quota"`
	InviteCount                  int                        `json:"invite_count"`
	MonthlyRebateQuota           int64                      `json:"monthly_rebate_quota"`
	TotalRebateQuota             int                        `json:"total_rebate_quota"`
	AffCode                      string                     `json:"aff_code"`
	InviteRebatePercent          float64                    `json:"invite_rebate_percent"`
	InvitationChainRewardQuota   int                        `json:"invitation_chain_reward_quota"`
	CashCommission               PromotionCommissionSummary `json:"cash_commission"`
}

type PromotionCommissionSummary struct {
	Currency                 string `json:"currency"`
	AvailableAmountCents     int64  `json:"available_amount_cents"`
	PendingAmountCents       int64  `json:"pending_amount_cents"`
	WithdrawingAmountCents   int64  `json:"withdrawing_amount_cents"`
	WithdrawnAmountCents     int64  `json:"withdrawn_amount_cents"`
	TransferredAmountCents   int64  `json:"transferred_amount_cents"`
	AvailableQuotaEquivalent int64  `json:"available_quota_equivalent"`
}

type PromotionWithdrawalRequest struct {
	PayoutMethod  string `json:"payout_method"`
	PayoutAccount string `json:"payout_account"`
	Remark        string `json:"remark"`
}

type PromotionWithdrawalReviewRequest struct {
	TradeNo     string `json:"trade_no"`
	ReviewNote  string `json:"review_note"`
	FailureNote string `json:"failure_note"`
}

type GrowthRewardItemStatus struct {
	*model.GrowthRewardItem
	RewardQuota          int    `json:"reward_quota"`
	RewardQuotaMin       int    `json:"reward_quota_min"`
	RewardQuotaMax       int    `json:"reward_quota_max"`
	ProgressCurrentQuota int64  `json:"progress_current_quota,omitempty"`
	ProgressTargetQuota  int64  `json:"progress_target_quota,omitempty"`
	Status               string `json:"status"`
	Claimable            bool   `json:"claimable"`
	Reason               string `json:"reason,omitempty"`
}

type GrowthSubmissionRequest struct {
	ItemCode       string `json:"item_code"`
	LegacyTaskCode string `json:"task_code"`
	Platform       string `json:"platform"`
	Url            string `json:"url" binding:"required"`
	Remark         string `json:"remark"`
}

type GrowthReviewRequest struct {
	RewardQuota int    `json:"reward_quota"`
	ReviewNote  string `json:"review_note"`
}

func GetGrowthSummary(userId int) (*GrowthSummary, error) {
	user, err := model.GetUserById(userId, true)
	if err != nil {
		return nil, err
	}
	rewardSummary, err := model.GetGrowthRewardSummary(userId)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
	var monthlyRebate int64
	if err := model.DB.Model(&model.InvitationRebate{}).
		Where("inviter_id = ? AND status = ? AND settled_at >= ?", userId, model.InvitationRebateStatusSettled, monthStart).
		Select("COALESCE(SUM(rebate_quota), 0)").
		Scan(&monthlyRebate).Error; err != nil {
		return nil, err
	}
	var monthlyInvitationReward int64
	if err := model.DB.Model(&model.InvitationReward{}).
		Where("inviter_id = ? AND status = ? AND settled_at >= ?", userId, model.InvitationRewardStatusSettled, monthStart).
		Select("COALESCE(SUM(reward_quota), 0)").
		Scan(&monthlyInvitationReward).Error; err != nil {
		return nil, err
	}
	var pendingInvitationRebate int64
	if err := model.DB.Model(&model.InvitationRebate{}).
		Where("inviter_id = ? AND status = ?", userId, model.InvitationRebateStatusPending).
		Select("COALESCE(SUM(rebate_quota), 0)").
		Scan(&pendingInvitationRebate).Error; err != nil {
		return nil, err
	}
	cashSummary, err := GetPromotionCommissionSummary(userId)
	if err != nil {
		return nil, err
	}

	return &GrowthSummary{
		AvailableRewardQuota:         rewardSummary.AvailableRewardQuota,
		PendingRewardQuota:           rewardSummary.PendingRewardQuota + pendingInvitationRebate,
		TotalRewardQuota:             rewardSummary.TotalRewardQuota,
		TaskRewardEarnedQuota:        rewardSummary.TaskRewardEarnedQuota,
		TaskRewardPendingQuota:       rewardSummary.TaskRewardPendingQuota,
		ReferralCreditAvailableQuota: rewardSummary.ReferralCreditAvailableQuota,
		ReferralCreditTotalQuota:     rewardSummary.ReferralCreditTotalQuota,
		InviteCount:                  user.AffCount,
		MonthlyRebateQuota:           monthlyRebate + monthlyInvitationReward,
		TotalRebateQuota:             user.AffHistoryQuota,
		AffCode:                      user.AffCode,
		InviteRebatePercent:          operation_setting.GetInviteRebatePercentage(),
		InvitationChainRewardQuota:   invitationChainRewardQuota(),
		CashCommission:               *cashSummary,
	}, nil
}

func invitationChainRewardQuota() int {
	growthSetting := operation_setting.GetGrowthSetting()
	total := common.QuotaForInviter
	if growthSetting.InviteFirstRequestRewardQuota > 0 {
		total += growthSetting.InviteFirstRequestRewardQuota
	}
	if growthSetting.InviteFirstTopUpRewardQuota > 0 {
		total += growthSetting.InviteFirstTopUpRewardQuota
	}
	return total
}

func GetPromotionCommissionSummary(userId int) (*PromotionCommissionSummary, error) {
	summary := &PromotionCommissionSummary{Currency: "CNY"}
	type statusAmountRow struct {
		Status string
		Amount int64
		Quota  int64
	}
	var rows []statusAmountRow
	if err := model.DB.Model(&model.PromotionCommissionLedger{}).
		Select("status, COALESCE(SUM(net_amount_cents), 0) AS amount, COALESCE(SUM(quota_equivalent), 0) AS quota").
		Where("user_id = ? AND cashable = ? AND currency = ?", userId, true, "CNY").
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		switch row.Status {
		case model.PromotionCommissionStatusSettled:
			summary.AvailableAmountCents = row.Amount
			summary.AvailableQuotaEquivalent = row.Quota
		case model.PromotionCommissionStatusPending:
			summary.PendingAmountCents = row.Amount
		case model.PromotionCommissionStatusWithdrawing:
			summary.WithdrawingAmountCents = row.Amount
		case model.PromotionCommissionStatusWithdrawn:
			summary.WithdrawnAmountCents = row.Amount
		case model.PromotionCommissionStatusTransferred:
			summary.TransferredAmountCents = row.Amount
		}
	}
	return summary, nil
}

func TransferAllSettledPromotionCommissionsToQuota(userId int) (int, error) {
	if userId <= 0 {
		return 0, errors.New("invalid user")
	}
	if err := model.SettleDueInvitationRebatesForInviter(userId); err != nil {
		return 0, err
	}
	var transferredQuota int
	var transferredAmountCents int64
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		ledgers, err := model.LockSettledPromotionCommissionLedgersTx(tx, userId)
		if err != nil {
			return err
		}
		if len(ledgers) == 0 {
			return errors.New("no settled cash commission available")
		}
		ledgerIds := make([]int, 0, len(ledgers))
		var transferredQuotaTotal int64
		for _, ledger := range ledgers {
			var err error
			transferredAmountCents, transferredQuotaTotal, err = addPromotionCommissionLedgerTotals(transferredAmountCents, transferredQuotaTotal, ledger)
			if err != nil {
				return err
			}
			ledgerIds = append(ledgerIds, ledger.Id)
		}
		transferredQuota = int(transferredQuotaTotal)
		now := common.GetTimestamp()
		res := tx.Model(&model.PromotionCommissionLedger{}).
			Where("id IN ? AND user_id = ? AND status = ? AND cashable = ? AND currency = ?", ledgerIds, userId, model.PromotionCommissionStatusSettled, true, "CNY").
			Updates(map[string]interface{}{
				"status":         model.PromotionCommissionStatusTransferred,
				"transferred_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(ledgerIds)) {
			return errors.New("commission status changed, please retry")
		}
		maxCurrentQuota := common.MaxQuota - 1 - transferredQuota
		walletUpdate := tx.Model(&model.User{}).
			Where("id = ? AND quota <= ?", userId, maxCurrentQuota).
			Update("quota", gorm.Expr("quota + ?", transferredQuota))
		if walletUpdate.Error != nil {
			return walletUpdate.Error
		}
		if walletUpdate.RowsAffected != 1 {
			return model.ErrTopUpQuotaLimitExceeded
		}
		return model.CreatePromotionEventTx(tx, &model.PromotionEvent{
			EventKey:        fmt.Sprintf("%s:%s:%d:%d", model.PromotionEventTypeCommissionTransferred, model.PromotionEventSourceCommissionTransfer, userId, now),
			UserId:          userId,
			EventType:       model.PromotionEventTypeCommissionTransferred,
			SourceTable:     model.PromotionEventSourceCommissionTransfer,
			SourceId:        int(now),
			Direction:       model.PromotionEventDirectionIncome,
			QuotaDelta:      transferredQuota,
			CashAmountCents: transferredAmountCents,
			Currency:        "CNY",
			Status:          model.PromotionCommissionStatusTransferred,
			Title:           "Cash commission transferred to balance",
			CreatedAt:       now,
		})
	})
	if err != nil {
		return 0, err
	}
	if err := model.InvalidateUserCache(userId); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate user cache after commission transfer: user_id=%d error=%v", userId, err))
	}
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("Promotion cash commission transferred to balance: %s", logger.LogQuota(transferredQuota)))
	return transferredQuota, nil
}

func CreatePromotionWithdrawal(userId int, req PromotionWithdrawalRequest) (*model.PromotionWithdrawal, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user")
	}
	var err error
	req, err = normalizePromotionWithdrawalRequest(req)
	if err != nil {
		return nil, err
	}
	if err := model.SettleDueInvitationRebatesForInviter(userId); err != nil {
		return nil, err
	}
	var withdrawal *model.PromotionWithdrawal
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		ledgers, err := model.LockSettledPromotionCommissionLedgersTx(tx, userId)
		if err != nil {
			return err
		}
		if len(ledgers) == 0 {
			return errors.New("no settled cash commission available")
		}

		ledgerIds := make([]int, 0, len(ledgers))
		var grossAmountCents int64
		var quotaEquivalentTotal int64
		for _, ledger := range ledgers {
			var err error
			grossAmountCents, quotaEquivalentTotal, err = addPromotionCommissionLedgerTotals(grossAmountCents, quotaEquivalentTotal, ledger)
			if err != nil {
				return err
			}
			ledgerIds = append(ledgerIds, ledger.Id)
		}
		quotaEquivalent := int(quotaEquivalentTotal)
		accountSnapshot, err := common.Marshal(map[string]interface{}{
			"payout_method":  req.PayoutMethod,
			"payout_account": req.PayoutAccount,
			"remark":         req.Remark,
		})
		if err != nil {
			return err
		}
		nextWithdrawal := &model.PromotionWithdrawal{
			UserId:                userId,
			Currency:              "CNY",
			GrossAmountCents:      grossAmountCents,
			NetAmountCents:        grossAmountCents,
			Status:                model.PromotionWithdrawalStatusPendingReview,
			PayoutMethod:          req.PayoutMethod,
			PayoutAccountSnapshot: string(accountSnapshot),
		}
		if err := tx.Create(nextWithdrawal).Error; err != nil {
			return err
		}
		items := make([]*model.PromotionWithdrawalItem, 0, len(ledgers))
		for _, ledger := range ledgers {
			items = append(items, &model.PromotionWithdrawalItem{
				WithdrawalId: nextWithdrawal.Id,
				LedgerId:     ledger.Id,
				AmountCents:  ledger.NetAmountCents,
			})
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		res := tx.Model(&model.PromotionCommissionLedger{}).
			Where("id IN ? AND user_id = ? AND status = ? AND cashable = ? AND currency = ?", ledgerIds, userId, model.PromotionCommissionStatusSettled, true, "CNY").
			Updates(map[string]interface{}{
				"status": model.PromotionCommissionStatusWithdrawing,
				"remark": fmt.Sprintf("withdrawal #%d, quota_equivalent=%d", nextWithdrawal.Id, quotaEquivalent),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(ledgerIds)) {
			return errors.New("commission status changed, please retry")
		}
		if err := model.CreatePromotionEventTx(tx, &model.PromotionEvent{
			UserId:          userId,
			EventType:       model.PromotionEventTypeCommissionWithdrawSubmitted,
			SourceTable:     model.PromotionEventSourceWithdrawal,
			SourceId:        nextWithdrawal.Id,
			Direction:       model.PromotionEventDirectionStatus,
			QuotaDelta:      -quotaEquivalent,
			CashAmountCents: grossAmountCents,
			Currency:        nextWithdrawal.Currency,
			Status:          nextWithdrawal.Status,
			Title:           "Cash withdrawal request submitted",
			Remark:          req.Remark,
			CreatedAt:       nextWithdrawal.AppliedAt,
		}); err != nil {
			return err
		}
		withdrawal = nextWithdrawal
		return nil
	})
	if err != nil {
		return nil, err
	}
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("Promotion cash withdrawal submitted: %.2f CNY", float64(withdrawal.NetAmountCents)/100))
	return withdrawal, nil
}

func ListPromotionCommissionLedgers(userId int, pageInfo *common.PageInfo) ([]*model.PromotionCommissionLedger, int64, error) {
	return model.ListPromotionCommissionLedgers(userId, pageInfo)
}

func ListPromotionWithdrawals(userId int, pageInfo *common.PageInfo) ([]*model.PromotionWithdrawal, int64, error) {
	return model.ListPromotionWithdrawals(userId, pageInfo)
}

func GetCheckinStats(userId int, month string) (map[string]interface{}, error) {
	return model.GetUserCheckinStats(userId, month)
}

func AdminApprovePromotionWithdrawal(id int, reviewerId int, req PromotionWithdrawalReviewRequest) (*model.PromotionWithdrawal, error) {
	var withdrawal *model.PromotionWithdrawal
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		updatedWithdrawal, err := updatePromotionWithdrawalReviewTx(tx, id, reviewerId, model.PromotionWithdrawalStatusApproved, req.TradeNo, req.ReviewNote, []string{
			model.PromotionWithdrawalStatusPendingReview,
		})
		if err != nil {
			return err
		}
		if err := model.ValidatePromotionWithdrawalLedgersPayableTx(tx, updatedWithdrawal.Id); err != nil {
			return err
		}
		withdrawal = updatedWithdrawal
		return model.CreatePromotionEventTx(tx, &model.PromotionEvent{
			UserId:          withdrawal.UserId,
			EventType:       model.PromotionEventTypeCommissionWithdrawApproved,
			SourceTable:     model.PromotionEventSourceWithdrawal,
			SourceId:        withdrawal.Id,
			Direction:       model.PromotionEventDirectionStatus,
			CashAmountCents: withdrawal.NetAmountCents,
			Currency:        withdrawal.Currency,
			Status:          withdrawal.Status,
			Title:           "Cash withdrawal request approved",
			Remark:          req.ReviewNote,
			CreatedAt:       withdrawal.ReviewedAt,
		})
	})
	return withdrawal, err
}

func AdminRejectPromotionWithdrawal(id int, reviewerId int, req PromotionWithdrawalReviewRequest) (*model.PromotionWithdrawal, error) {
	var withdrawal *model.PromotionWithdrawal
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		updatedWithdrawal, err := updatePromotionWithdrawalReviewTx(tx, id, reviewerId, model.PromotionWithdrawalStatusRejected, "", req.ReviewNote, []string{
			model.PromotionWithdrawalStatusPendingReview,
			model.PromotionWithdrawalStatusApproved,
		})
		if err != nil {
			return err
		}
		withdrawal = updatedWithdrawal
		if err := releasePromotionWithdrawalLedgersTx(tx, withdrawal.Id, model.PromotionCommissionStatusSettled); err != nil {
			return err
		}
		return model.CreatePromotionEventTx(tx, &model.PromotionEvent{
			UserId:          withdrawal.UserId,
			EventType:       model.PromotionEventTypeCommissionWithdrawRejected,
			SourceTable:     model.PromotionEventSourceWithdrawal,
			SourceId:        withdrawal.Id,
			Direction:       model.PromotionEventDirectionStatus,
			CashAmountCents: withdrawal.NetAmountCents,
			Currency:        withdrawal.Currency,
			Status:          withdrawal.Status,
			Title:           "Cash withdrawal request rejected",
			Remark:          req.ReviewNote,
			CreatedAt:       withdrawal.ReviewedAt,
		})
	})
	return withdrawal, err
}

func AdminMarkPromotionWithdrawalPaid(id int, reviewerId int, req PromotionWithdrawalReviewRequest) (*model.PromotionWithdrawal, error) {
	var withdrawal *model.PromotionWithdrawal
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		updatedWithdrawal, err := updatePromotionWithdrawalReviewTx(tx, id, reviewerId, model.PromotionWithdrawalStatusPaid, req.TradeNo, req.ReviewNote, []string{
			model.PromotionWithdrawalStatusApproved,
		})
		if err != nil {
			return err
		}
		if err := model.ValidatePromotionWithdrawalLedgersPayableTx(tx, updatedWithdrawal.Id); err != nil {
			return err
		}
		withdrawal = updatedWithdrawal
		if err := releasePromotionWithdrawalLedgersTx(tx, withdrawal.Id, model.PromotionCommissionStatusWithdrawn); err != nil {
			return err
		}
		now := common.GetTimestamp()
		if err := tx.Model(&model.PromotionCommissionLedger{}).
			Where("id IN (?)", tx.Model(&model.PromotionWithdrawalItem{}).Select("ledger_id").Where("withdrawal_id = ?", withdrawal.Id)).
			Update("withdrawn_at", now).Error; err != nil {
			return err
		}
		return model.CreatePromotionEventTx(tx, &model.PromotionEvent{
			UserId:          withdrawal.UserId,
			EventType:       model.PromotionEventTypeCommissionWithdrawPaid,
			SourceTable:     model.PromotionEventSourceWithdrawal,
			SourceId:        withdrawal.Id,
			Direction:       model.PromotionEventDirectionOutcome,
			CashAmountCents: -withdrawal.NetAmountCents,
			Currency:        withdrawal.Currency,
			Status:          withdrawal.Status,
			Title:           "Cash withdrawal paid",
			Remark:          req.ReviewNote,
			CreatedAt:       withdrawal.PaidAt,
		})
	})
	return withdrawal, err
}

func updatePromotionWithdrawalReviewTx(tx *gorm.DB, id int, reviewerId int, status string, tradeNo string, note string, allowedStatuses []string) (*model.PromotionWithdrawal, error) {
	if id <= 0 {
		return nil, errors.New("invalid withdrawal")
	}
	now := common.GetTimestamp()
	updates := map[string]interface{}{
		"status":      status,
		"reviewer_id": reviewerId,
		"review_note": strings.TrimSpace(note),
		"reviewed_at": now,
	}
	if strings.TrimSpace(tradeNo) != "" {
		updates["trade_no"] = strings.TrimSpace(tradeNo)
	}
	if status == model.PromotionWithdrawalStatusPaid {
		updates["paid_at"] = now
	}
	query := tx.Model(&model.PromotionWithdrawal{}).Where("id = ?", id)
	if len(allowedStatuses) > 0 {
		query = query.Where("status IN ?", allowedStatuses)
	}
	res := query.Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, errors.New("withdrawal status does not allow this operation")
	}
	var withdrawal model.PromotionWithdrawal
	if err := tx.Where("id = ?", id).First(&withdrawal).Error; err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

func releasePromotionWithdrawalLedgersTx(tx *gorm.DB, withdrawalId int, targetStatus string) error {
	if withdrawalId <= 0 || targetStatus == "" {
		return nil
	}
	updates := map[string]interface{}{"status": targetStatus}
	sourceStatus := model.PromotionCommissionStatusWithdrawing
	if targetStatus == model.PromotionCommissionStatusWithdrawn {
		updates["withdrawn_at"] = common.GetTimestamp()
	}
	var itemCount int64
	if err := tx.Model(&model.PromotionWithdrawalItem{}).Where("withdrawal_id = ?", withdrawalId).Count(&itemCount).Error; err != nil {
		return err
	}
	if itemCount == 0 {
		return model.ErrPromotionWithdrawalLedgerNotPayable
	}
	result := tx.Model(&model.PromotionCommissionLedger{}).
		Where("status = ? AND id IN (?)", sourceStatus, tx.Model(&model.PromotionWithdrawalItem{}).Select("ledger_id").Where("withdrawal_id = ?", withdrawalId)).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != itemCount {
		return model.ErrPromotionWithdrawalLedgerNotPayable
	}
	return nil
}

func ListGrowthRewardItemsForUser(userId int) ([]*GrowthRewardItemStatus, error) {
	growthSetting := operation_setting.GetGrowthSetting()
	var rewardItems []*model.GrowthRewardItem
	if err := model.DB.Order("id ASC").Find(&rewardItems).Error; err != nil {
		return nil, err
	}
	items := make([]*GrowthRewardItemStatus, 0, len(rewardItems))
	for _, item := range rewardItems {
		if shouldHideGrowthRewardItem(item, growthSetting) {
			continue
		}
		rewardQuotaMin, rewardQuotaMax := resolveGrowthRewardQuotaRange(item)
		rewardQuota := rewardQuotaMin
		if rewardQuota <= 0 && item.Code != model.GrowthRewardItemDailyCheckin {
			continue
		}
		status := "not_available"
		claimable := false
		reason := ""

		if item.ItemType == model.GrowthRewardItemTypeManual || item.ItemType == model.GrowthRewardItemTypeSemiAuto {
			status, reason = submissionStatus(userId, item)
		} else {
			completed, err := rewardItemCompleted(userId, item)
			if err != nil {
				return nil, err
			}
			if completed {
				status = "completed"
			} else {
				ok, msg, err := canClaimAutoRewardItem(userId, item)
				if err != nil {
					return nil, err
				}
				if ok {
					status = "available"
					claimable = true
				} else {
					reason = msg
				}
			}
		}

		items = append(items, &GrowthRewardItemStatus{
			GrowthRewardItem:     item,
			RewardQuota:          rewardQuota,
			RewardQuotaMin:       rewardQuotaMin,
			RewardQuotaMax:       rewardQuotaMax,
			ProgressCurrentQuota: growthRewardItemProgressCurrentQuota(userId, item),
			ProgressTargetQuota:  growthRewardItemProgressTargetQuota(item),
			Status:               status,
			Claimable:            claimable,
			Reason:               reason,
		})
	}
	return items, nil
}

func shouldHideGrowthRewardItem(item *model.GrowthRewardItem, growthSetting *operation_setting.GrowthSetting) bool {
	if item == nil || !item.Enabled {
		return true
	}
	if item.Code == model.GrowthRewardItemDailyCheckin {
		return !growthSetting.DailyCheckinEnabled
	}
	if item.ItemType == model.GrowthRewardItemTypeManual || item.ItemType == model.GrowthRewardItemTypeSemiAuto {
		return !growthSetting.SubmissionEnabled
	}
	return !growthSetting.Enabled
}

func ClaimGrowthRewardItem(userId int, code string, password string) (*model.GrowthReward, error) {
	item, err := getGrowthRewardItem(code)
	if err != nil {
		return nil, err
	}
	if !item.Enabled {
		return nil, errors.New("reward item disabled")
	}
	if item.ItemType != model.GrowthRewardItemTypeAuto {
		return nil, errors.New("this reward item requires submission review")
	}
	if err := validateGrowthRewardClaimPassword(item, password); err != nil {
		return nil, err
	}

	if item.Code == model.GrowthRewardItemDailyCheckin {
		return claimDailyCheckin(userId, item)
	}

	if !operation_setting.GetGrowthSetting().Enabled {
		return nil, errors.New("growth rewards are not enabled")
	}
	completed, err := rewardItemCompleted(userId, item)
	if err != nil {
		return nil, err
	}
	if completed {
		return nil, errors.New("reward item already completed")
	}
	ok, msg, err := canClaimAutoRewardItem(userId, item)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New(msg)
	}
	rewardQuota := resolveGrowthRewardQuota(item)
	var reward *model.GrowthReward
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockUserForRewardTx(tx, userId); err != nil {
			return err
		}
		completed, err := rewardItemCompletedTx(tx, userId, item)
		if err != nil {
			return err
		}
		if completed {
			return errors.New("reward item already completed")
		}
		if err := checkRewardBudgetTx(tx, userId, rewardQuota); err != nil {
			return err
		}
		reward = model.NewSettledGrowthReward(userId, item.Code, rewardQuota, 0, "")
		reward.ClaimKey = growthRewardClaimKey(userId, item, 0)
		return model.CreateSettledGrowthRewardTx(tx, reward)
	})
	if err != nil {
		return nil, err
	}
	_ = model.InvalidateUserCache(userId)
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("Growth reward settled: %s for %s", logger.LogQuota(rewardQuota), item.Code))
	return reward, nil
}

func ListGrowthRewards(userId int, pageInfo *common.PageInfo) ([]*model.GrowthReward, int64, error) {
	return model.ListGrowthRewards(userId, pageInfo)
}

func ListPromotionEvents(userId int, pageInfo *common.PageInfo) ([]*model.PromotionEvent, int64, error) {
	return model.ListPromotionEvents(userId, pageInfo)
}

func CreateGrowthSubmission(userId int, req GrowthSubmissionRequest) (*model.GrowthSubmission, error) {
	if !operation_setting.GetGrowthSetting().SubmissionEnabled {
		return nil, errors.New("growth submissions are not enabled")
	}
	var err error
	req, err = normalizeGrowthSubmissionRequest(req)
	if err != nil {
		return nil, err
	}
	itemCode := req.ItemCode
	if itemCode == "" {
		itemCode = req.LegacyTaskCode
	}
	if itemCode == "" {
		return nil, errors.New("reward item is required")
	}
	item, err := getGrowthRewardItem(itemCode)
	if err != nil {
		return nil, err
	}
	if !item.Enabled {
		return nil, errors.New("reward item disabled")
	}
	if item.ItemType == model.GrowthRewardItemTypeAuto {
		return nil, errors.New("this reward item does not accept submissions")
	}
	submission := &model.GrowthSubmission{
		UserId:   userId,
		ItemCode: item.Code,
		Platform: req.Platform,
		Url:      req.Url,
		Remark:   req.Remark,
		Status:   model.GrowthSubmissionStatusPending,
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if item.DailyLimit > 0 {
			if err := lockUserForRewardTx(tx, userId); err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&model.GrowthSubmission{}).
				Where("user_id = ? AND item_code = ? AND created_at >= ? AND status <> ?", userId, item.Code, startOfToday(), model.GrowthSubmissionStatusRejected).
				Count(&count).Error; err != nil {
				return err
			}
			if count >= int64(item.DailyLimit) {
				return errors.New("daily submission limit reached")
			}
		}
		if err := tx.Create(submission).Error; err != nil {
			return err
		}
		return model.CreateGrowthSubmissionEventTx(tx, submission, model.PromotionEventTypeGrowthSubmissionCreated)
	}); err != nil {
		return nil, err
	}
	return submission, nil
}

func ListGrowthSubmissions(userId int, pageInfo *common.PageInfo) ([]*model.GrowthSubmission, int64, error) {
	return model.ListGrowthSubmissions(userId, pageInfo)
}

func AdminListGrowthRewards(pageInfo *common.PageInfo) ([]*model.GrowthReward, int64, error) {
	var total int64
	if err := model.DB.Model(&model.GrowthReward{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rewards []*model.GrowthReward
	err := model.DB.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&rewards).Error
	return rewards, total, err
}

func ApproveGrowthSubmission(id int, reviewerId int, req GrowthReviewRequest) (*model.GrowthSubmission, error) {
	var submission model.GrowthSubmission
	if err := model.DB.Where("id = ?", id).First(&submission).Error; err != nil {
		return nil, err
	}
	if submission.Status != model.GrowthSubmissionStatusPending {
		return nil, errors.New("submission already reviewed")
	}
	item, err := getGrowthRewardItem(submission.ItemCode)
	if err != nil {
		return nil, err
	}
	rewardQuota := req.RewardQuota
	if rewardQuota <= 0 {
		rewardQuota = resolveGrowthRewardQuota(item)
	}
	if rewardQuota <= 0 {
		rewardQuota = operation_setting.GetGrowthSetting().SubmissionMinRewardQuota
	}
	minRewardQuota, maxRewardQuota := resolveGrowthRewardQuotaRange(item)
	if rewardQuota < minRewardQuota {
		return nil, errors.New("reward quota is below minimum")
	}
	if rewardQuota > maxRewardQuota {
		return nil, errors.New("reward quota exceeds maximum")
	}
	now := time.Now().Unix()
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockUserForRewardTx(tx, submission.UserId); err != nil {
			return err
		}
		if err := checkRewardBudgetTx(tx, submission.UserId, rewardQuota); err != nil {
			return err
		}
		res := tx.Model(&model.GrowthSubmission{}).
			Where("id = ? AND status = ?", submission.Id, model.GrowthSubmissionStatusPending).
			Updates(map[string]interface{}{
				"status":      model.GrowthSubmissionStatusApproved,
				"reviewer_id": reviewerId,
				"review_note": req.ReviewNote,
				"reviewed_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("submission already reviewed")
		}
		submission.Status = model.GrowthSubmissionStatusApproved
		submission.ReviewerId = reviewerId
		submission.ReviewNote = req.ReviewNote
		submission.ReviewedAt = now
		if err := model.CreateGrowthSubmissionEventTx(tx, &submission, model.PromotionEventTypeGrowthSubmissionApproved); err != nil {
			return err
		}
		reward := model.NewSettledGrowthReward(submission.UserId, submission.ItemCode, rewardQuota, submission.Id, req.ReviewNote)
		reward.ClaimKey = growthRewardClaimKey(submission.UserId, item, submission.Id)
		reward.CreatedAt = now
		reward.AvailableAt = now
		reward.SettledAt = now
		return model.CreateSettledGrowthRewardTx(tx, reward)
	})
	if err != nil {
		return nil, err
	}
	if err := model.DB.Where("id = ?", id).First(&submission).Error; err != nil {
		return nil, err
	}
	_ = model.InvalidateUserCache(submission.UserId)
	model.RecordLog(submission.UserId, model.LogTypeSystem, fmt.Sprintf("Growth submission approved: %s for %s", logger.LogQuota(rewardQuota), submission.ItemCode))
	return &submission, nil
}

func RejectGrowthSubmission(id int, reviewerId int, note string) (*model.GrowthSubmission, error) {
	var submission model.GrowthSubmission
	if err := model.DB.Where("id = ?", id).First(&submission).Error; err != nil {
		return nil, err
	}
	if submission.Status != model.GrowthSubmissionStatusPending {
		return nil, errors.New("submission already reviewed")
	}
	now := time.Now().Unix()
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.GrowthSubmission{}).
			Where("id = ? AND status = ?", id, model.GrowthSubmissionStatusPending).
			Updates(map[string]interface{}{
				"status":      model.GrowthSubmissionStatusRejected,
				"reviewer_id": reviewerId,
				"review_note": note,
				"reviewed_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("submission already reviewed")
		}
		submission.Status = model.GrowthSubmissionStatusRejected
		submission.ReviewerId = reviewerId
		submission.ReviewNote = note
		submission.ReviewedAt = now
		return model.CreateGrowthSubmissionEventTx(tx, &submission, model.PromotionEventTypeGrowthSubmissionRejected)
	}); err != nil {
		return nil, err
	}
	if err := model.DB.Where("id = ?", id).First(&submission).Error; err != nil {
		return nil, err
	}
	return &submission, nil
}

func GetGrowthAdminStats() (map[string]interface{}, error) {
	stats := map[string]interface{}{}
	var totalRewards int64
	var pendingSubmissions int64
	var totalRewardQuota int64
	if err := model.DB.Model(&model.GrowthReward{}).Count(&totalRewards).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.GrowthSubmission{}).Where("status = ?", model.GrowthSubmissionStatusPending).Count(&pendingSubmissions).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.GrowthReward{}).
		Where("status IN ?", []string{model.GrowthRewardStatusSettled, model.GrowthRewardStatusTransferred}).
		Select("COALESCE(SUM(reward_quota), 0)").
		Scan(&totalRewardQuota).Error; err != nil {
		return nil, err
	}
	stats["total_rewards"] = totalRewards
	stats["pending_submissions"] = pendingSubmissions
	stats["total_reward_quota"] = totalRewardQuota
	return stats, nil
}

func getGrowthRewardItem(code string) (*model.GrowthRewardItem, error) {
	var item model.GrowthRewardItem
	err := model.DB.Where("code = ?", code).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("reward item not found")
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func resolveGrowthRewardQuota(item *model.GrowthRewardItem) int {
	minQuota, _ := resolveGrowthRewardQuotaRange(item)
	return minQuota
}

func growthRewardItemProgressCurrentQuota(userId int, item *model.GrowthRewardItem) int64 {
	if item == nil || item.Code != model.GrowthRewardItemMonthlySpendTarget {
		return 0
	}
	quota, err := sumUserConsumeQuota(userId, startOfMonth(), time.Now().Unix())
	if err != nil {
		return 0
	}
	return quota
}

func growthRewardItemProgressTargetQuota(item *model.GrowthRewardItem) int64 {
	if item == nil || item.Code != model.GrowthRewardItemMonthlySpendTarget {
		return 0
	}
	return int64(operation_setting.GetGrowthSetting().MonthlySpendTargetQuota)
}

func resolveGrowthRewardQuotaRange(item *model.GrowthRewardItem) (int, int) {
	setting := operation_setting.GetGrowthSetting()
	if item.Code == model.GrowthRewardItemDailyCheckin {
		if setting.DailyCheckinEnabled {
			return normalizeRewardQuotaRange(setting.DailyCheckinMinRewardQuota, setting.DailyCheckinMaxRewardQuota)
		}
		return 0, 0
	}
	if item.RewardQuota > 0 {
		return item.RewardQuota, item.RewardQuota
	}
	switch item.Code {
	case model.GrowthRewardItemCreateFirstAPIKey:
		return setting.FirstAPIKeyRewardQuota, setting.FirstAPIKeyRewardQuota
	case model.GrowthRewardItemFirstAPIRequest:
		return setting.FirstAPIRequestRewardQuota, setting.FirstAPIRequestRewardQuota
	case model.GrowthRewardItemFirstTopUp:
		return setting.FirstTopUpRewardQuota, setting.FirstTopUpRewardQuota
	case model.GrowthRewardItemThreeDayUsage:
		return setting.ThreeDayUsageRewardQuota, setting.ThreeDayUsageRewardQuota
	case model.GrowthRewardItemMonthlySpendTarget:
		return setting.MonthlySpendRewardQuota, setting.MonthlySpendRewardQuota
	case model.GrowthRewardItemContentPublish, model.GrowthRewardItemBacklinkSubmission:
		return normalizeRewardQuotaRange(setting.SubmissionMinRewardQuota, setting.SubmissionMaxRewardQuota)
	case model.GrowthRewardItemJoinCommunity:
		return setting.SubmissionMinRewardQuota, setting.SubmissionMinRewardQuota
	default:
		return 0, 0
	}
}

func normalizeRewardQuotaRange(minQuota int, maxQuota int) (int, int) {
	if maxQuota <= 0 || maxQuota < minQuota {
		return minQuota, minQuota
	}
	return minQuota, maxQuota
}

func rewardItemCompleted(userId int, item *model.GrowthRewardItem) (bool, error) {
	return rewardItemCompletedTx(model.DB, userId, item)
}

func rewardItemCompletedTx(tx *gorm.DB, userId int, item *model.GrowthRewardItem) (bool, error) {
	if item.Code == model.GrowthRewardItemDailyCheckin {
		var count int64
		if tx == nil {
			tx = model.DB
		}
		err := tx.Model(&model.Checkin{}).
			Where("user_id = ? AND checkin_date = ?", userId, time.Now().Format("2006-01-02")).
			Count(&count).Error
		return count > 0, err
	}
	if item.Code == model.GrowthRewardItemMonthlySpendTarget {
		count, err := countGrowthRewardsSinceTx(tx, userId, item.Code, startOfMonth())
		return count > 0, err
	}
	if item.OncePerUser {
		count, err := countGrowthRewardsSinceTx(tx, userId, item.Code, 0)
		return count > 0, err
	}
	if item.DailyLimit > 0 {
		count, err := countGrowthRewardsSinceTx(tx, userId, item.Code, startOfToday())
		return count >= int64(item.DailyLimit), err
	}
	return false, nil
}

func countGrowthRewardsSinceTx(tx *gorm.DB, userId int, itemCode string, since int64) (int64, error) {
	if tx == nil {
		tx = model.DB
	}
	query := tx.Model(&model.GrowthReward{}).
		Where("user_id = ? AND item_code = ? AND status <> ?", userId, itemCode, model.GrowthRewardStatusRejected)
	if since > 0 {
		query = query.Where("created_at >= ?", since)
	}
	var count int64
	err := query.Count(&count).Error
	return count, err
}

func lockUserForRewardTx(tx *gorm.DB, userId int) error {
	return model.LockUserForGrowthRewardTx(tx, userId)
}

func growthRewardClaimKey(userId int, item *model.GrowthRewardItem, sourceId int) *string {
	if item == nil || userId <= 0 {
		return nil
	}
	var key string
	switch {
	case sourceId > 0:
		key = fmt.Sprintf("submission:%d", sourceId)
	case item.Code == model.GrowthRewardItemMonthlySpendTarget:
		key = fmt.Sprintf("%d:%s:%s", userId, item.Code, time.Now().Format("2006-01"))
	case item.Code == model.GrowthRewardItemDailyCheckin || item.DailyLimit > 0:
		key = fmt.Sprintf("%d:%s:%s", userId, item.Code, time.Now().Format("2006-01-02"))
	case item.OncePerUser:
		key = fmt.Sprintf("%d:%s:once", userId, item.Code)
	default:
		return nil
	}
	return &key
}

func canClaimAutoRewardItem(userId int, item *model.GrowthRewardItem) (bool, string, error) {
	switch item.Code {
	case model.GrowthRewardItemDailyCheckin:
		checked, err := model.HasCheckedInToday(userId)
		return !checked, "Already checked in today", err
	case model.GrowthRewardItemCreateFirstAPIKey:
		count, err := model.CountUserTokens(userId)
		return count > 0, "Create an API key first", err
	case model.GrowthRewardItemFirstAPIRequest:
		var count int
		err := model.DB.Model(&model.User{}).Where("id = ?", userId).Select("request_count").Scan(&count).Error
		return count > 0, "Complete one API request first", err
	case model.GrowthRewardItemFirstTopUp:
		var count int64
		err := model.DB.Model(&model.TopUp{}).
			Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
			Where("refund_status IS NULL OR refund_status NOT IN ?", []string{model.TopUpRefundStatusFull, model.TopUpRefundStatusDisputed}).
			Count(&count).Error
		return count > 0, "Complete your first top-up first", err
	case model.GrowthRewardItemThreeDayUsage:
		ok, err := hasConsecutiveUsageDays(userId, 3)
		return ok, "Use the API for 3 consecutive days first", err
	case model.GrowthRewardItemMonthlySpendTarget:
		targetQuota := operation_setting.GetGrowthSetting().MonthlySpendTargetQuota
		if targetQuota <= 0 {
			return false, "Monthly spend target is not configured", nil
		}
		quota, err := sumUserConsumeQuota(userId, startOfMonth(), time.Now().Unix())
		return quota >= int64(targetQuota), "Reach this month's spend target first", err
	case model.GrowthRewardItemJoinCommunity:
		return true, "", nil
	default:
		return false, "This automatic reward item is not available yet", nil
	}
}

func validateGrowthRewardClaimPassword(item *model.GrowthRewardItem, password string) error {
	if item.Code != model.GrowthRewardItemJoinCommunity || item.ClaimPassword == "" {
		return nil
	}
	if strings.TrimSpace(password) != strings.TrimSpace(item.ClaimPassword) {
		return errors.New("Invalid task password")
	}
	return nil
}

func claimDailyCheckin(userId int, item *model.GrowthRewardItem) (*model.GrowthReward, error) {
	completed, err := rewardItemCompleted(userId, item)
	if err != nil {
		return nil, err
	}
	if completed {
		return nil, errors.New("already checked in today")
	}
	rewardQuota, err := model.CalculateCheckinQuota()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	checkin := model.NewCheckinRecord(userId, rewardQuota)
	reward := model.NewSettledGrowthReward(userId, item.Code, rewardQuota, 0, "")
	reward.ClaimKey = growthRewardClaimKey(userId, item, 0)
	reward.CreatedAt = now
	reward.AvailableAt = now
	reward.SettledAt = now
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockUserForRewardTx(tx, userId); err != nil {
			return err
		}
		completed, err := rewardItemCompletedTx(tx, userId, item)
		if err != nil {
			return err
		}
		if completed {
			return errors.New("already checked in today")
		}
		if err := checkRewardBudgetTx(tx, userId, rewardQuota); err != nil {
			return err
		}
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}
		reward.SourceId = checkin.Id
		return model.CreateSettledGrowthRewardTx(tx, reward)
	})
	if err != nil {
		return nil, err
	}
	_ = model.InvalidateUserCache(userId)
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	return reward, nil
}

func submissionStatus(userId int, item *model.GrowthRewardItem) (string, string) {
	var submission model.GrowthSubmission
	err := model.DB.Where("user_id = ? AND item_code = ?", userId, item.Code).Order("id DESC").First(&submission).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "available", ""
	}
	if err != nil {
		return "not_available", err.Error()
	}
	switch submission.Status {
	case model.GrowthSubmissionStatusPending:
		return "pending_review", ""
	case model.GrowthSubmissionStatusApproved:
		return "available", ""
	case model.GrowthSubmissionStatusRejected:
		return "available", "Previous submission was rejected"
	default:
		return "not_available", ""
	}
}

func checkRewardBudget(userId int, rewardQuota int) error {
	return checkRewardBudgetTx(model.DB, userId, rewardQuota)
}

func checkRewardBudgetTx(tx *gorm.DB, userId int, rewardQuota int) error {
	if rewardQuota <= 0 {
		return nil
	}
	if tx == nil {
		tx = model.DB
	}
	setting := operation_setting.GetGrowthSetting()
	todayStart := startOfToday()
	if setting.UserDailyRewardLimitQuota > 0 {
		var total int64
		if err := tx.Model(&model.GrowthReward{}).
			Where("user_id = ? AND created_at >= ? AND status IN ?", userId, todayStart, []string{model.GrowthRewardStatusPending, model.GrowthRewardStatusSettled, model.GrowthRewardStatusTransferred}).
			Select("COALESCE(SUM(reward_quota), 0)").
			Scan(&total).Error; err != nil {
			return err
		}
		if total+int64(rewardQuota) > int64(setting.UserDailyRewardLimitQuota) {
			return errors.New("daily user reward limit reached")
		}
	}
	if setting.SiteDailyBudgetQuota > 0 {
		return model.ReserveGrowthRewardBudgetTx(tx, time.Now().Format("2006-01-02"), rewardQuota, setting.SiteDailyBudgetQuota)
	}
	return nil
}

func startOfToday() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
}

func startOfMonth() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
}

func hasConsecutiveUsageDays(userId int, days int) (bool, error) {
	if userId <= 0 || days <= 0 {
		return false, nil
	}
	todayStart := startOfToday()
	for offset := 0; offset < days; offset++ {
		dayStart := todayStart - int64(offset)*24*60*60
		dayEnd := dayStart + 24*60*60
		var count int64
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("user_id = ? AND type = ? AND created_at >= ? AND created_at < ?", userId, model.LogTypeConsume, dayStart, dayEnd).
			Count(&count).Error; err != nil {
			return false, err
		}
		if count == 0 {
			return false, nil
		}
	}
	return true, nil
}

func sumUserConsumeQuota(userId int, startTimestamp int64, endTimestamp int64) (int64, error) {
	var quota int64
	err := model.LOG_DB.Model(&model.Log{}).
		Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userId, model.LogTypeConsume, startTimestamp, endTimestamp).
		Select("COALESCE(SUM(quota), 0)").
		Scan(&quota).Error
	return quota, err
}
