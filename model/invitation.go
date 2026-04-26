package model

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

type UserInvitationRecord struct {
	UserID                  int     `json:"user_id"`
	Username                string  `json:"username"`
	DisplayName             string  `json:"display_name"`
	TotalTopUpAmount        float64 `json:"total_topup_amount"`
	TotalContributionRebate float64 `json:"total_contribution_rebate"`
}

type userInvitationRecordRow struct {
	UserID                  int     `json:"user_id"`
	Username                string  `json:"username"`
	DisplayName             string  `json:"display_name"`
	TotalTopUpAmount        float64 `json:"total_topup_amount"`
	TotalContributionRebate float64 `json:"total_contribution_rebate"`
}

func CalculateInvitationRebateAmount(totalTopUpAmount float64) float64 {
	if totalTopUpAmount <= 0 || common.InviteRebatePercentage <= 0 {
		return 0
	}

	return decimal.NewFromFloat(totalTopUpAmount).
		Mul(decimal.NewFromFloat(common.InviteRebatePercentage)).
		Div(decimal.NewFromInt(100)).
		InexactFloat64()
}

func CalculateInvitationRebateQuota(totalTopUpAmount float64) int {
	if totalTopUpAmount <= 0 || common.InviteRebatePercentage <= 0 || common.QuotaPerUnit <= 0 {
		return 0
	}

	return int(decimal.NewFromFloat(totalTopUpAmount).
		Mul(decimal.NewFromFloat(common.InviteRebatePercentage)).
		Div(decimal.NewFromInt(100)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		IntPart())
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
		Select("users.id AS user_id, users.username, users.display_name, COALESCE(SUM(invitation_rebates.top_up_money), 0) AS total_topup_amount, COALESCE(SUM(invitation_rebates.rebate_amount), 0) AS total_contribution_rebate").
		Joins("LEFT JOIN invitation_rebates ON invitation_rebates.invitee_id = users.id AND invitation_rebates.inviter_id = ? AND invitation_rebates.status = ?", inviterId, InvitationRebateStatusSettled).
		Where("users.inviter_id = ?", inviterId).
		Group("users.id, users.username, users.display_name").
		Order("users.id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&rows).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	records = make([]*UserInvitationRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, &UserInvitationRecord{
			UserID:                  row.UserID,
			Username:                row.Username,
			DisplayName:             row.DisplayName,
			TotalTopUpAmount:        row.TotalTopUpAmount,
			TotalContributionRebate: row.TotalContributionRebate,
		})
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}
