package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

const (
	InvitationRebateStatusSettled = "settled"
)

type InvitationRebate struct {
	Id               int     `json:"id"`
	InviterId        int     `json:"inviter_id" gorm:"index"`
	InviteeId        int     `json:"invitee_id" gorm:"index"`
	TopUpId          int     `json:"top_up_id" gorm:"uniqueIndex"`
	TradeNo          string  `json:"trade_no" gorm:"type:varchar(255);index"`
	TopUpMoney       float64 `json:"top_up_money"`
	RebatePercentage float64 `json:"rebate_percentage"`
	RebateAmount     float64 `json:"rebate_amount"`
	RebateQuota      int     `json:"rebate_quota"`
	Status           string  `json:"status" gorm:"type:varchar(32);index"`
	CreatedAt        int64   `json:"created_at" gorm:"index"`
	SettledAt        int64   `json:"settled_at" gorm:"index"`
}

type UserInvitationRebateRecord struct {
	Id            int     `json:"id"`
	InviteeId     int     `json:"invitee_id"`
	InviteeName   string  `json:"invitee_name"`
	TradeNo       string  `json:"trade_no"`
	TopUpMoney    float64 `json:"top_up_money"`
	RebateAmount  float64 `json:"rebate_amount"`
	RebateQuota   int     `json:"rebate_quota"`
	Status        string  `json:"status"`
	CreatedAt     int64   `json:"created_at"`
	SettledAt     int64   `json:"settled_at"`
}

func SettleInvitationRebateTx(tx *gorm.DB, topUp *TopUp) (*InvitationRebate, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if topUp == nil {
		return nil, errors.New("topup is required")
	}
	if topUp.Id == 0 || topUp.Status != common.TopUpStatusSuccess {
		return nil, nil
	}
	if topUp.Money <= 0 || common.InviteRebatePercentage <= 0 {
		return nil, nil
	}

	var existing InvitationRebate
	err := tx.Where("top_up_id = ?", topUp.Id).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var invitee User
	if err = tx.Select("id", "inviter_id").Where("id = ?", topUp.UserId).First(&invitee).Error; err != nil {
		return nil, err
	}
	if invitee.InviterId == 0 {
		return nil, nil
	}

	rebateAmount := CalculateInvitationRebateAmount(topUp.Money)
	if rebateAmount <= 0 {
		return nil, nil
	}
	rebateQuota := CalculateInvitationRebateQuota(topUp.Money)
	settledAt := topUp.CompleteTime
	if settledAt == 0 {
		settledAt = common.GetTimestamp()
	}

	rebate := &InvitationRebate{
		InviterId:        invitee.InviterId,
		InviteeId:        invitee.Id,
		TopUpId:          topUp.Id,
		TradeNo:          topUp.TradeNo,
		TopUpMoney:       topUp.Money,
		RebatePercentage: common.InviteRebatePercentage,
		RebateAmount:     rebateAmount,
		RebateQuota:      rebateQuota,
		Status:           InvitationRebateStatusSettled,
		CreatedAt:        settledAt,
		SettledAt:        settledAt,
	}
	if err = tx.Create(rebate).Error; err != nil {
		return nil, err
	}

	if rebateQuota > 0 {
		err = tx.Model(&User{}).
			Where("id = ?", rebate.InviterId).
			Updates(map[string]interface{}{
				"aff_quota":   gorm.Expr("aff_quota + ?", rebateQuota),
				"aff_history": gorm.Expr("aff_history + ?", rebateQuota),
			}).Error
		if err != nil {
			return nil, err
		}
	}

	return rebate, nil
}

func RecordInvitationRebateLog(rebate *InvitationRebate) {
	if rebate == nil {
		return
	}

	content := fmt.Sprintf(
		"Invitation rebate settled: %s from user #%d top-up %.2f",
		logger.LogQuota(rebate.RebateQuota),
		rebate.InviteeId,
		rebate.TopUpMoney,
	)
	RecordLog(rebate.InviterId, LogTypeSystem, content)
}

func GetUserInvitationRebateRecords(inviterId int, pageInfo *common.PageInfo) (
	records []*UserInvitationRebateRecord,
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

	err = tx.Model(&InvitationRebate{}).Where("inviter_id = ?", inviterId).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Table("invitation_rebates").
		Select("invitation_rebates.id, invitation_rebates.invitee_id, invitation_rebates.trade_no, invitation_rebates.top_up_money, invitation_rebates.rebate_amount, invitation_rebates.rebate_quota, invitation_rebates.status, invitation_rebates.created_at, invitation_rebates.settled_at, COALESCE(NULLIF(users.display_name, ''), users.username) AS invitee_name").
		Joins("LEFT JOIN users ON users.id = invitation_rebates.invitee_id").
		Where("invitation_rebates.inviter_id = ?", inviterId).
		Order("invitation_rebates.id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&records).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

func SyncInvitationRebatesForInviter(inviterId int) error {
	if inviterId <= 0 || common.InviteRebatePercentage <= 0 {
		return nil
	}

	for {
		var topUps []TopUp
		err := DB.Table("top_ups").
			Select("top_ups.id, top_ups.user_id, top_ups.amount, top_ups.money, top_ups.trade_no, top_ups.payment_method, top_ups.payment_provider, top_ups.create_time, top_ups.complete_time, top_ups.status").
			Joins("INNER JOIN users ON users.id = top_ups.user_id").
			Joins("LEFT JOIN invitation_rebates ON invitation_rebates.top_up_id = top_ups.id").
			Joins("LEFT JOIN subscription_orders ON subscription_orders.trade_no = top_ups.trade_no").
			Where("users.inviter_id = ? AND top_ups.status = ? AND invitation_rebates.id IS NULL AND subscription_orders.id IS NULL", inviterId, common.TopUpStatusSuccess).
			Order("top_ups.id ASC").
			Limit(200).
			Scan(&topUps).Error
		if err != nil {
			return err
		}
		if len(topUps) == 0 {
			return nil
		}

		for _, topUp := range topUps {
			err = DB.Transaction(func(tx *gorm.DB) error {
				lockedTopUp := &TopUp{}
				if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", topUp.Id).First(lockedTopUp).Error; err != nil {
					return err
				}
				_, err := SettleInvitationRebateTx(tx, lockedTopUp)
				return err
			})
			if err != nil {
				return err
			}
		}
	}
}
