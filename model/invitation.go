package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

type UserInvitationRecord struct {
	UserID                      int     `json:"user_id"`
	Username                    string  `json:"username"`
	DisplayName                 string  `json:"display_name"`
	CreatedAt                   int64   `json:"created_at"`
	RequestCount                int     `json:"request_count"`
	TotalTopUpAmount            float64 `json:"total_topup_amount"`
	TotalContributionRebate     float64 `json:"total_contribution_rebate"`
	TotalRebateQuota            int     `json:"total_rebate_quota"`
	FirstRequestCompleted       bool    `json:"first_request_completed"`
	FirstTopUpCompleted         bool    `json:"first_topup_completed"`
	FirstRequestRewardQuota     int     `json:"first_request_reward_quota"`
	FirstTopUpRewardQuota       int     `json:"first_topup_reward_quota"`
	RegisterRewardQuota         int     `json:"register_reward_quota"`
	FirstRequestRuleRewardQuota int     `json:"first_request_rule_reward_quota"`
	FirstTopUpRuleRewardQuota   int     `json:"first_topup_rule_reward_quota"`
	InviteRebatePercentage      float64 `json:"invite_rebate_percentage"`
}

type userInvitationRecordRow struct {
	UserID                  int     `json:"user_id"`
	Username                string  `json:"username"`
	DisplayName             string  `json:"display_name"`
	CreatedAt               int64   `json:"created_at"`
	RequestCount            int     `json:"request_count"`
	TotalTopUpAmount        float64 `json:"total_topup_amount"`
	TotalContributionRebate float64 `json:"total_contribution_rebate"`
	TotalRebateQuota        int     `json:"total_rebate_quota"`
}

type invitationTopUpStatusRow struct {
	UserID int   `json:"user_id"`
	Count  int64 `json:"count"`
}

type invitationRewardStatusRow struct {
	InviteeId   int    `json:"invitee_id"`
	RewardType  string `json:"reward_type"`
	RewardQuota int    `json:"reward_quota"`
}

func CalculateInvitationRebateAmount(totalTopUpAmount float64) float64 {
	percentage := operation_setting.GetInviteRebatePercentage()
	if totalTopUpAmount <= 0 || percentage <= 0 {
		return 0
	}

	return decimal.NewFromFloat(totalTopUpAmount).
		Mul(decimal.NewFromFloat(percentage)).
		Div(decimal.NewFromInt(100)).
		InexactFloat64()
}

func CalculateInvitationRebateQuota(totalTopUpAmount float64) int {
	percentage := operation_setting.GetInviteRebatePercentage()
	if totalTopUpAmount <= 0 || percentage <= 0 || common.QuotaPerUnit <= 0 || operation_setting.USDExchangeRate <= 0 {
		return 0
	}

	quota, clamp := common.QuotaFromDecimalChecked(decimal.NewFromFloat(totalTopUpAmount).
		Mul(decimal.NewFromFloat(percentage)).
		Div(decimal.NewFromInt(100)).
		Div(decimal.NewFromFloat(operation_setting.USDExchangeRate)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if clamp != nil {
		return 0
	}
	return quota
}

func GetUserInvitationRecords(inviterId int, pageInfo *common.PageInfo) (
	records []*UserInvitationRecord,
	total int64,
	err error,
) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Model(&User{}).Where("inviter_id = ?", inviterId).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	var rows []*userInvitationRecordRow
	err = tx.Table("users").
		Select("users.id AS user_id, users.username, users.display_name, users.created_at, users.request_count, COALESCE(SUM(invitation_rebates.top_up_money), 0) AS total_topup_amount, COALESCE(SUM(invitation_rebates.rebate_amount), 0) AS total_contribution_rebate, COALESCE(SUM(invitation_rebates.rebate_quota), 0) AS total_rebate_quota").
		Joins("LEFT JOIN invitation_rebates ON invitation_rebates.invitee_id = users.id AND invitation_rebates.inviter_id = ? AND invitation_rebates.status = ?", inviterId, InvitationRebateStatusSettled).
		Where("users.inviter_id = ?", inviterId).
		Group("users.id, users.username, users.display_name, users.created_at, users.request_count").
		Order("users.id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&rows).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	inviteeIds := make([]int, 0, len(rows))
	for _, row := range rows {
		inviteeIds = append(inviteeIds, row.UserID)
	}

	topUpCompleted := make(map[int]bool, len(inviteeIds))
	rewardByInvitee := make(map[int]map[string]int, len(inviteeIds))
	if len(inviteeIds) > 0 {
		var topUpRows []*invitationTopUpStatusRow
		err = tx.Table("top_ups").
			Select("top_ups.user_id, COUNT(*) AS count").
			Joins("LEFT JOIN subscription_orders ON subscription_orders.trade_no = top_ups.trade_no").
			Where("top_ups.user_id IN ? AND top_ups.status = ? AND subscription_orders.id IS NULL", inviteeIds, common.TopUpStatusSuccess).
			Where("top_ups.refund_status IS NULL OR top_ups.refund_status NOT IN ?", []string{TopUpRefundStatusFull, TopUpRefundStatusDisputed}).
			Group("top_ups.user_id").
			Scan(&topUpRows).Error
		if err != nil {
			tx.Rollback()
			return nil, 0, err
		}
		for _, row := range topUpRows {
			topUpCompleted[row.UserID] = row.Count > 0
		}

		var rewardRows []*invitationRewardStatusRow
		err = tx.Model(&InvitationReward{}).
			Select("invitee_id, reward_type, reward_quota").
			Where("inviter_id = ? AND invitee_id IN ? AND status = ?", inviterId, inviteeIds, InvitationRewardStatusSettled).
			Scan(&rewardRows).Error
		if err != nil {
			tx.Rollback()
			return nil, 0, err
		}
		for _, row := range rewardRows {
			if _, ok := rewardByInvitee[row.InviteeId]; !ok {
				rewardByInvitee[row.InviteeId] = map[string]int{}
			}
			rewardByInvitee[row.InviteeId][row.RewardType] += row.RewardQuota
		}
	}

	records = make([]*UserInvitationRecord, 0, len(rows))
	growthSetting := operation_setting.GetGrowthSetting()
	for _, row := range rows {
		milestoneRewards := rewardByInvitee[row.UserID]
		firstRequestRewardQuota := milestoneRewards[InvitationRewardTypeFirstRequest]
		firstTopUpRewardQuota := milestoneRewards[InvitationRewardTypeFirstTopUp]
		records = append(records, &UserInvitationRecord{
			UserID:                      row.UserID,
			Username:                    row.Username,
			DisplayName:                 row.DisplayName,
			CreatedAt:                   row.CreatedAt,
			RequestCount:                row.RequestCount,
			TotalTopUpAmount:            row.TotalTopUpAmount,
			TotalContributionRebate:     row.TotalContributionRebate,
			TotalRebateQuota:            row.TotalRebateQuota,
			FirstRequestCompleted:       row.RequestCount > 0 || firstRequestRewardQuota > 0,
			FirstTopUpCompleted:         topUpCompleted[row.UserID] || firstTopUpRewardQuota > 0,
			FirstRequestRewardQuota:     firstRequestRewardQuota,
			FirstTopUpRewardQuota:       firstTopUpRewardQuota,
			RegisterRewardQuota:         common.QuotaForInviter,
			FirstRequestRuleRewardQuota: growthSetting.InviteFirstRequestRewardQuota,
			FirstTopUpRuleRewardQuota:   growthSetting.InviteFirstTopUpRewardQuota,
			InviteRebatePercentage:      operation_setting.GetInviteRebatePercentage(),
		})
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}
