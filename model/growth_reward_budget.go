package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGrowthRewardSiteBudgetReached = errors.New("daily site reward budget reached")

// GrowthRewardBudget serializes the site-wide daily reward budget. A SUM over
// growth_rewards cannot prevent two users from both spending the last budget
// concurrently; this row is reserved in the same transaction as the reward.
type GrowthRewardBudget struct {
	BudgetDate  string `json:"budget_date" gorm:"primaryKey;type:varchar(10)"`
	RewardQuota int    `json:"reward_quota"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;index"`
}

func ReserveGrowthRewardBudgetTx(tx *gorm.DB, budgetDate string, rewardQuota int, limit int) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if limit <= 0 || rewardQuota <= 0 {
		return nil
	}
	if budgetDate == "" || rewardQuota > limit {
		return ErrGrowthRewardSiteBudgetReached
	}
	budgetDay, err := time.ParseInLocation("2006-01-02", budgetDate, time.Local)
	if err != nil {
		return ErrGrowthRewardSiteBudgetReached
	}
	row := &GrowthRewardBudget{BudgetDate: budgetDate}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return err
	}
	if err := lockForUpdate(tx).Where("budget_date = ?", budgetDate).First(row).Error; err != nil {
		return err
	}

	var existingRewardQuota int64
	if err := tx.Model(&GrowthReward{}).
		Where("created_at >= ? AND created_at < ?", budgetDay.Unix(), budgetDay.AddDate(0, 0, 1).Unix()).
		Where("status IN ?", []string{GrowthRewardStatusPending, GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
		Select("COALESCE(SUM(reward_quota), 0)").
		Scan(&existingRewardQuota).Error; err != nil {
		return err
	}
	if existingRewardQuota > int64(common.MaxQuota) {
		existingRewardQuota = int64(common.MaxQuota)
	}
	if existingRewardQuota > int64(row.RewardQuota) {
		row.RewardQuota = int(existingRewardQuota)
		if err := tx.Model(&GrowthRewardBudget{}).
			Where("budget_date = ?", budgetDate).
			Updates(map[string]interface{}{
				"reward_quota": row.RewardQuota,
				"updated_at":   time.Now().Unix(),
			}).Error; err != nil {
			return err
		}
	}
	result := tx.Model(&GrowthRewardBudget{}).
		Where("budget_date = ? AND reward_quota <= ?", budgetDate, limit-rewardQuota).
		Updates(map[string]interface{}{
			"reward_quota": gorm.Expr("reward_quota + ?", rewardQuota),
			"updated_at":   time.Now().Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGrowthRewardSiteBudgetReached
	}
	return nil
}
