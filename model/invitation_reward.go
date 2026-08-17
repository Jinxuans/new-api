package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	InvitationRewardTypeRegister     = "register"
	InvitationRewardTypeFirstRequest = "first_request"
	InvitationRewardTypeFirstTopUp   = "first_topup"
	InvitationRewardStatusPending    = "pending"
	InvitationRewardStatusSettled    = "settled"
	InvitationRewardStatusReversed   = "reversed"
)

var ErrInvitationRewardQuotaLimitExceeded = errors.New("invitation reward quota limit exceeded")

type InvitationReward struct {
	Id               int    `json:"id"`
	InviterId        int    `json:"inviter_id" gorm:"index"`
	InviteeId        int    `json:"invitee_id" gorm:"index:idx_invitation_reward_invitee_type,unique"`
	RewardType       string `json:"reward_type" gorm:"type:varchar(32);index:idx_invitation_reward_invitee_type,unique"`
	RewardQuota      int    `json:"reward_quota"`
	TransferredQuota int    `json:"transferred_quota"`
	TriggerAt        int64  `json:"trigger_at" gorm:"index"`
	TriggerTopUpId   int    `json:"trigger_top_up_id" gorm:"index"`
	TriggerTradeNo   string `json:"trigger_trade_no" gorm:"type:varchar(255);index"`
	RuleSnapshot     string `json:"rule_snapshot" gorm:"type:text"`
	Remark           string `json:"remark" gorm:"type:text"`
	Status           string `json:"status" gorm:"type:varchar(32);index"`
	CreatedAt        int64  `json:"created_at" gorm:"index"`
	SettledAt        int64  `json:"settled_at" gorm:"index"`
}

type UserInvitationRewardRecord struct {
	InviteeName string `json:"invitee_name"`
	RewardType  string `json:"reward_type"`
	RewardQuota int    `json:"reward_quota"`
	TriggerAt   int64  `json:"trigger_at"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	SettledAt   int64  `json:"settled_at"`
}

func SettleInvitationMilestoneRewardTx(tx *gorm.DB, inviteeId int, rewardType string) (*InvitationReward, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if inviteeId <= 0 || rewardType == "" {
		return nil, nil
	}

	var existing InvitationReward
	err := lockForUpdate(tx).
		Where("invitee_id = ? AND reward_type = ?", inviteeId, rewardType).
		First(&existing).Error
	if err == nil {
		if existing.Status != InvitationRewardStatusPending || existing.RewardQuota <= 0 {
			return nil, nil
		}
		return settlePendingInvitationRewardTx(tx, &existing)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return nil, nil
	}

	rewardQuota := resolveInvitationMilestoneRewardQuota(rewardType)
	if rewardQuota <= 0 {
		return nil, nil
	}

	var invitee User
	if err := tx.Select("id", "inviter_id").Where("id = ?", inviteeId).First(&invitee).Error; err != nil {
		return nil, err
	}
	if invitee.InviterId == 0 {
		return nil, nil
	}
	triggerAt := common.GetTimestamp()
	triggerTopUpId := 0
	triggerTradeNo := ""
	if rewardType == InvitationRewardTypeFirstTopUp {
		var successTopUpCount int64
		if err := tx.Model(&TopUp{}).
			Where("user_id = ? AND purpose = ? AND status = ?", inviteeId, TopUpPurposeAPIBalance, common.TopUpStatusSuccess).
			Where("refund_status IS NULL OR refund_status NOT IN ?", []string{TopUpRefundStatusFull, TopUpRefundStatusDisputed}).
			Count(&successTopUpCount).Error; err != nil {
			return nil, err
		}
		if successTopUpCount != 1 {
			return nil, nil
		}
		var topUp TopUp
		if err := tx.Select("id", "trade_no", "complete_time", "create_time").
			Where("user_id = ? AND purpose = ? AND status = ?", inviteeId, TopUpPurposeAPIBalance, common.TopUpStatusSuccess).
			Where("refund_status IS NULL OR refund_status NOT IN ?", []string{TopUpRefundStatusFull, TopUpRefundStatusDisputed}).
			Order("id ASC").
			First(&topUp).Error; err != nil {
			return nil, err
		}
		triggerTopUpId = topUp.Id
		triggerTradeNo = topUp.TradeNo
		if topUp.CompleteTime > 0 {
			triggerAt = topUp.CompleteTime
		} else if topUp.CreateTime > 0 {
			triggerAt = topUp.CreateTime
		}
	}

	now := common.GetTimestamp()
	reward := &InvitationReward{
		InviterId:      invitee.InviterId,
		InviteeId:      invitee.Id,
		RewardType:     rewardType,
		RewardQuota:    rewardQuota,
		TriggerAt:      triggerAt,
		TriggerTopUpId: triggerTopUpId,
		TriggerTradeNo: triggerTradeNo,
		RuleSnapshot:   buildInvitationRewardRuleSnapshot(rewardType, rewardQuota),
		Status:         InvitationRewardStatusPending,
		CreatedAt:      now,
	}
	if err = tx.Create(reward).Error; err != nil {
		return nil, err
	}
	return settlePendingInvitationRewardTx(tx, reward)
}

// QueueInvitationFirstRequestRewardTx persists the amount owed for the first
// successful request before request_count can advance. Configuration changes
// after this point do not alter the snapshotted reward.
func QueueInvitationFirstRequestRewardTx(tx *gorm.DB, inviteeId int) (*InvitationReward, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if inviteeId <= 0 || !operation_setting.IsPaymentComplianceConfirmed() {
		return nil, nil
	}
	rewardQuota := resolveInvitationMilestoneRewardQuota(InvitationRewardTypeFirstRequest)
	if rewardQuota <= 0 {
		return nil, nil
	}

	var existing InvitationReward
	err := tx.Where("invitee_id = ? AND reward_type = ?", inviteeId, InvitationRewardTypeFirstRequest).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var invitee User
	if err := lockForUpdate(tx).
		Select("id", "inviter_id", "request_count").
		Where("id = ?", inviteeId).
		First(&invitee).Error; err != nil {
		return nil, err
	}
	if invitee.InviterId <= 0 || invitee.RequestCount != 0 {
		return nil, nil
	}
	var inviter User
	if err := tx.Select("id").Where("id = ?", invitee.InviterId).First(&inviter).Error; err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	reward := &InvitationReward{
		InviterId:    invitee.InviterId,
		InviteeId:    invitee.Id,
		RewardType:   InvitationRewardTypeFirstRequest,
		RewardQuota:  rewardQuota,
		TriggerAt:    now,
		RuleSnapshot: buildInvitationRewardRuleSnapshot(InvitationRewardTypeFirstRequest, rewardQuota),
		Status:       InvitationRewardStatusPending,
		CreatedAt:    now,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "invitee_id"}, {Name: "reward_type"}},
		DoNothing: true,
	}).Create(reward)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if err := tx.Where("invitee_id = ? AND reward_type = ?", inviteeId, InvitationRewardTypeFirstRequest).First(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	return reward, nil
}

func settlePendingInvitationRewardTx(tx *gorm.DB, reward *InvitationReward) (*InvitationReward, error) {
	if tx == nil || reward == nil || reward.Id <= 0 || reward.Status != InvitationRewardStatusPending || reward.RewardQuota <= 0 {
		return nil, nil
	}
	var inviter User
	if err := lockForUpdate(tx).Select("id").Where("id = ?", reward.InviterId).First(&inviter).Error; err != nil {
		return nil, err
	}
	credited, err := addInvitationRewardQuotaTx(tx, reward.InviterId, reward.RewardQuota, false)
	if err != nil {
		return nil, err
	}
	if !credited {
		return nil, nil
	}

	now := common.GetTimestamp()
	result := tx.Model(&InvitationReward{}).
		Where("id = ? AND status = ?", reward.Id, InvitationRewardStatusPending).
		Updates(map[string]interface{}{"status": InvitationRewardStatusSettled, "settled_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, errors.New("invitation reward status changed during settlement")
	}
	reward.Status = InvitationRewardStatusSettled
	reward.SettledAt = now
	if err := CreateInvitationRewardEventTx(tx, reward); err != nil {
		return nil, err
	}
	if err := createInvitationRewardFundTransactionTx(tx, reward); err != nil {
		return nil, err
	}
	return reward, nil
}

func addInvitationRewardQuotaTx(tx *gorm.DB, userId int, rewardQuota int, incrementInviteCount bool) (bool, error) {
	if tx == nil {
		return false, errors.New("transaction is required")
	}
	if userId <= 0 || rewardQuota < 0 || rewardQuota >= common.MaxQuota {
		return false, ErrInvitationRewardQuotaLimitExceeded
	}
	updates := map[string]interface{}{}
	query := tx.Model(&User{}).Where("id = ?", userId)
	if rewardQuota > 0 {
		maxCurrentQuota := common.MaxQuota - 1 - rewardQuota
		query = query.Where("aff_quota <= ? AND aff_history <= ?", maxCurrentQuota, maxCurrentQuota)
		updates["aff_quota"] = gorm.Expr("aff_quota + ?", rewardQuota)
		updates["aff_history"] = gorm.Expr("aff_history + ?", rewardQuota)
	}
	if incrementInviteCount {
		query = query.Where("aff_count < ?", common.MaxQuota)
		updates["aff_count"] = gorm.Expr("aff_count + 1")
	}
	if len(updates) == 0 {
		return false, nil
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, nil
	}
	return true, nil
}

func CreateInvitationRegisterRewardTx(tx *gorm.DB, inviterId int, inviteeId int) (*InvitationReward, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if inviterId <= 0 || inviteeId <= 0 {
		return nil, nil
	}

	var existing InvitationReward
	err := tx.Where("invitee_id = ? AND reward_type = ?", inviteeId, InvitationRewardTypeRegister).First(&existing).Error
	if err == nil {
		return nil, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	rewardQuota := 0
	if operation_setting.IsPaymentComplianceConfirmed() && common.QuotaForInviter > 0 {
		rewardQuota = common.QuotaForInviter
	}
	credited, err := addInvitationRewardQuotaTx(tx, inviterId, rewardQuota, true)
	if err != nil {
		return nil, err
	}
	if !credited && rewardQuota > 0 {
		rewardQuota = 0
		credited, err = addInvitationRewardQuotaTx(tx, inviterId, 0, true)
		if err != nil {
			return nil, err
		}
	}
	if !credited {
		return nil, nil
	}

	now := common.GetTimestamp()
	reward := &InvitationReward{
		InviterId:    inviterId,
		InviteeId:    inviteeId,
		RewardType:   InvitationRewardTypeRegister,
		RewardQuota:  rewardQuota,
		TriggerAt:    now,
		RuleSnapshot: buildInvitationRewardRuleSnapshot(InvitationRewardTypeRegister, rewardQuota),
		Status:       InvitationRewardStatusSettled,
		CreatedAt:    now,
		SettledAt:    now,
	}
	if err = tx.Create(reward).Error; err != nil {
		return nil, err
	}
	if reward.RewardQuota > 0 {
		if err = CreateInvitationRewardEventTx(tx, reward); err != nil {
			return nil, err
		}
		if err = createInvitationRewardFundTransactionTx(tx, reward); err != nil {
			return nil, err
		}
	}
	return reward, nil
}

func createInvitationRewardFundTransactionTx(tx *gorm.DB, reward *InvitationReward) error {
	if tx == nil || reward == nil || reward.Id <= 0 || reward.RewardQuota <= 0 {
		return nil
	}
	var inviter User
	if err := tx.Select("aff_quota").Where("id = ?", reward.InviterId).First(&inviter).Error; err != nil {
		return err
	}
	balanceAfter := int64(inviter.AffQuota)
	return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("invitation_reward:%d:issued", reward.Id),
		Kind:           PromotionFundKindInvitationRewardIssued,
		UserId:         reward.InviterId,
		SourceType:     "invitation_rewards",
		SourceId:       reward.Id,
		SourceKey:      fmt.Sprintf("invitation_rewards:%d", reward.Id),
		ActorType:      "system",
		Remark:         reward.Remark,
		OccurredAt:     reward.SettledAt,
	}, []PromotionFundTransactionLeg{{
		Account:      PromotionFundAccountReferralCredit,
		Asset:        PromotionFundAssetQuota,
		Amount:       int64(reward.RewardQuota),
		SourceType:   "invitation_rewards",
		SourceId:     reward.Id,
		BalanceAfter: &balanceAfter,
	}})
}

func buildInvitationRewardRuleSnapshot(rewardType string, rewardQuota int) string {
	snapshot := map[string]interface{}{
		"reward_type":  rewardType,
		"reward_quota": rewardQuota,
	}
	data, err := common.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(data)
}

func SettleInvitationMilestoneReward(inviteeId int, rewardType string) (*InvitationReward, error) {
	var reward *InvitationReward
	err := DB.Transaction(func(tx *gorm.DB) error {
		settledReward, err := SettleInvitationMilestoneRewardTx(tx, inviteeId, rewardType)
		if err != nil {
			return err
		}
		reward = settledReward
		return nil
	})
	return reward, err
}

func RecordInvitationMilestoneRewardLog(reward *InvitationReward) {
	if reward == nil {
		return
	}
	content := fmt.Sprintf(
		"Invitation milestone reward settled: %s for %s from user #%d",
		logger.LogQuota(reward.RewardQuota),
		reward.RewardType,
		reward.InviteeId,
	)
	RecordLog(reward.InviterId, LogTypeSystem, content)
}

func SettleInvitationFirstRequestReward(inviteeId int) {
	reward, err := SettleInvitationMilestoneReward(inviteeId, InvitationRewardTypeFirstRequest)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to settle invitation first request reward for user %d: %v", inviteeId, err))
		return
	}
	RecordInvitationMilestoneRewardLog(reward)
}

func resolveInvitationMilestoneRewardQuota(rewardType string) int {
	setting := operation_setting.GetGrowthSetting()
	switch rewardType {
	case InvitationRewardTypeRegister:
		return common.QuotaForInviter
	case InvitationRewardTypeFirstRequest:
		return setting.InviteFirstRequestRewardQuota
	case InvitationRewardTypeFirstTopUp:
		return setting.InviteFirstTopUpRewardQuota
	default:
		return 0
	}
}

func GetUserInvitationRewardRecords(inviterId int, pageInfo *common.PageInfo) (
	records []*UserInvitationRewardRecord,
	total int64,
	err error,
) {
	var pendingInviteeIds []int
	if err = DB.Model(&InvitationReward{}).
		Where("inviter_id = ? AND reward_type = ? AND status = ?", inviterId, InvitationRewardTypeFirstRequest, InvitationRewardStatusPending).
		Order("id ASC").
		Pluck("invitee_id", &pendingInviteeIds).Error; err != nil {
		return nil, 0, err
	}
	for _, inviteeId := range pendingInviteeIds {
		settled, settleErr := SettleInvitationMilestoneReward(inviteeId, InvitationRewardTypeFirstRequest)
		if settleErr != nil {
			return nil, 0, settleErr
		}
		RecordInvitationMilestoneRewardLog(settled)
	}

	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Model(&InvitationReward{}).Where("inviter_id = ? AND reward_quota > 0", inviterId).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Table("invitation_rewards").
		Select("COALESCE(NULLIF(users.display_name, ''), users.username) AS invitee_name, invitation_rewards.reward_type, invitation_rewards.reward_quota, invitation_rewards.trigger_at, invitation_rewards.status, invitation_rewards.created_at, invitation_rewards.settled_at").
		Joins("LEFT JOIN users ON users.id = invitation_rewards.invitee_id").
		Where("invitation_rewards.inviter_id = ? AND invitation_rewards.reward_quota > 0", inviterId).
		Order("invitation_rewards.id DESC").
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
