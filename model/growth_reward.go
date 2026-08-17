package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	GrowthRewardStatusPending     = "pending"
	GrowthRewardStatusSettled     = "settled"
	GrowthRewardStatusTransferred = "transferred"
	GrowthRewardStatusFrozen      = "frozen"
	GrowthRewardStatusRejected    = "rejected"
	GrowthRewardStatusReversed    = "reversed"
)

var ErrGrowthRewardAlreadyClaimed = errors.New("growth reward already claimed")

type GrowthReward struct {
	Id          int     `json:"id" gorm:"primaryKey"`
	UserId      int     `json:"user_id" gorm:"index;not null"`
	ItemCode    string  `json:"item_code" gorm:"type:varchar(64);index;not null"`
	RewardQuota int     `json:"reward_quota" gorm:"not null;default:0"`
	Status      string  `json:"status" gorm:"type:varchar(32);index;not null"`
	SourceId    int     `json:"source_id" gorm:"index;default:0"`
	ClaimKey    *string `json:"-" gorm:"type:varchar(191);uniqueIndex"`
	AvailableAt int64   `json:"available_at" gorm:"bigint;index"`
	CreatedAt   int64   `json:"created_at" gorm:"bigint;index"`
	SettledAt   int64   `json:"settled_at" gorm:"bigint;index"`
	Remark      string  `json:"remark" gorm:"type:text"`
}

type GrowthRewardSummary struct {
	// Legacy aggregate fields retained for service compatibility. They mix
	// task rewards and referral credits; new code should use the split fields.
	AvailableRewardQuota         int64 `json:"available_reward_quota"`
	PendingRewardQuota           int64 `json:"pending_reward_quota"`
	TotalRewardQuota             int64 `json:"total_reward_quota"`
	TaskRewardEarnedQuota        int64 `json:"task_reward_earned_quota"`
	TaskRewardPendingQuota       int64 `json:"task_reward_pending_quota"`
	ReferralCreditAvailableQuota int64 `json:"referral_credit_available_quota"`
	ReferralCreditTotalQuota     int64 `json:"referral_credit_total_quota"`
}

func (GrowthReward) TableName() string {
	return "growth_rewards"
}

func (reward *GrowthReward) BeforeCreate(_ *gorm.DB) error {
	if reward.CreatedAt == 0 {
		reward.CreatedAt = time.Now().Unix()
	}
	return nil
}

func HasGrowthReward(userId int, itemCode string) (bool, error) {
	var count int64
	err := DB.Model(&GrowthReward{}).
		Where("user_id = ? AND item_code = ? AND status <> ?", userId, itemCode, GrowthRewardStatusRejected).
		Count(&count).Error
	return count > 0, err
}

func CountGrowthRewardsSince(userId int, itemCode string, since int64) (int64, error) {
	tx := DB.Model(&GrowthReward{}).
		Where("user_id = ? AND item_code = ? AND status <> ?", userId, itemCode, GrowthRewardStatusRejected)
	if since > 0 {
		tx = tx.Where("created_at >= ?", since)
	}
	var count int64
	err := tx.Count(&count).Error
	return count, err
}

func CreateSettledGrowthReward(userId int, itemCode string, rewardQuota int, sourceId int, remark string) (*GrowthReward, error) {
	reward := NewSettledGrowthReward(userId, itemCode, rewardQuota, sourceId, remark)
	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreateSettledGrowthRewardTx(tx, reward)
	})
	if err != nil {
		return nil, err
	}
	if rewardQuota > 0 {
		invalidateUserQuotaCacheAfterDBWrite(userId, "growth reward")
	}
	return reward, nil
}

func NewSettledGrowthReward(userId int, itemCode string, rewardQuota int, sourceId int, remark string) *GrowthReward {
	now := time.Now().Unix()
	return &GrowthReward{
		UserId:      userId,
		ItemCode:    itemCode,
		RewardQuota: rewardQuota,
		Status:      GrowthRewardStatusSettled,
		SourceId:    sourceId,
		AvailableAt: now,
		CreatedAt:   now,
		SettledAt:   now,
		Remark:      remark,
	}
}

func CreateSettledGrowthRewardTx(tx *gorm.DB, reward *GrowthReward) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if reward == nil {
		return errors.New("reward is required")
	}
	if reward.RewardQuota < 0 {
		return errors.New("reward quota cannot be negative")
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "claim_key"}},
		DoNothing: true,
	}).Create(reward)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrGrowthRewardAlreadyClaimed
	}
	if err := CreateGrowthRewardEventTx(tx, reward); err != nil {
		return err
	}
	if reward.RewardQuota <= 0 {
		return nil
	}
	maxCurrentQuota, err := topUpQuotaMaxCurrent(reward.RewardQuota)
	if err != nil {
		return err
	}
	result = tx.Model(&User{}).
		Where("id = ? AND quota <= ?", reward.UserId, maxCurrentQuota).
		Update("quota", gorm.Expr("quota + ?", reward.RewardQuota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTopUpQuotaLimitExceeded
	}
	var user User
	if err := tx.Select("quota").Where("id = ?", reward.UserId).First(&user).Error; err != nil {
		return err
	}
	balanceAfter := int64(user.Quota)
	return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("growth_reward:%d:issued", reward.Id),
		Kind:           PromotionFundKindGrowthRewardIssued,
		UserId:         reward.UserId,
		SourceType:     "growth_rewards",
		SourceId:       reward.Id,
		SourceKey:      fmt.Sprintf("growth_rewards:%d", reward.Id),
		ActorType:      "system",
		Remark:         reward.Remark,
		OccurredAt:     reward.SettledAt,
	}, []PromotionFundTransactionLeg{{
		Account:      PromotionFundAccountAPIBalance,
		Asset:        PromotionFundAssetQuota,
		Amount:       int64(reward.RewardQuota),
		SourceType:   "growth_rewards",
		SourceId:     reward.Id,
		BalanceAfter: &balanceAfter,
	}})
}

func LockUserForGrowthRewardTx(tx *gorm.DB, userId int) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	var user User
	return lockForUpdate(tx).Select("id").Where("id = ?", userId).First(&user).Error
}

func GetGrowthRewardSummary(userId int) (*GrowthRewardSummary, error) {
	summary := &GrowthRewardSummary{}
	var user User
	if err := DB.Select("aff_quota", "aff_history").Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}
	summary.AvailableRewardQuota = int64(user.AffQuota)
	summary.ReferralCreditAvailableQuota = int64(user.AffQuota)
	summary.ReferralCreditTotalQuota = int64(user.AffHistoryQuota)
	if err := DB.Model(&GrowthReward{}).
		Where("user_id = ? AND status = ?", userId, GrowthRewardStatusPending).
		Select("COALESCE(SUM(reward_quota), 0)").
		Scan(&summary.TaskRewardPendingQuota).Error; err != nil {
		return nil, err
	}
	summary.PendingRewardQuota = summary.TaskRewardPendingQuota
	if err := DB.Model(&GrowthReward{}).
		Where("user_id = ? AND status IN ?", userId, []string{GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
		Select("COALESCE(SUM(reward_quota), 0)").
		Scan(&summary.TaskRewardEarnedQuota).Error; err != nil {
		return nil, err
	}
	summary.TotalRewardQuota = summary.TaskRewardEarnedQuota + summary.ReferralCreditTotalQuota
	return summary, nil
}

func ListGrowthRewards(userId int, pageInfo *common.PageInfo) ([]*GrowthReward, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
	var total int64
	if err := DB.Model(&GrowthReward{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rewards []*GrowthReward
	err := DB.Where("user_id = ?", userId).
		Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&rewards).Error
	return rewards, total, err
}
