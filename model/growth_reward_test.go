package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreateSettledGrowthRewardTx_ClaimKeyCreditsOnlyOnce(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 3201, Username: "growth_claim_key", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)

	claimKey := "3201:first_api_request:once"
	first := NewSettledGrowthReward(user.Id, GrowthRewardItemFirstAPIRequest, 1234, 0, "")
	first.ClaimKey = &claimKey
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return CreateSettledGrowthRewardTx(tx, first)
	}))

	duplicate := NewSettledGrowthReward(user.Id, GrowthRewardItemFirstAPIRequest, 1234, 0, "")
	duplicate.ClaimKey = &claimKey
	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreateSettledGrowthRewardTx(tx, duplicate)
	})
	require.ErrorIs(t, err, ErrGrowthRewardAlreadyClaimed)

	var rewardCount int64
	require.NoError(t, DB.Model(&GrowthReward{}).Where("claim_key = ?", claimKey).Count(&rewardCount).Error)
	assert.Equal(t, int64(1), rewardCount)
	var reloaded User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 1234, reloaded.Quota)
}

func TestReserveGrowthRewardBudgetTx_RejectsAmountAboveRemainingBudget(t *testing.T) {
	truncateTables(t)
	const budgetDate = "2026-08-16"

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ReserveGrowthRewardBudgetTx(tx, budgetDate, 600, 1000)
	}))
	err := DB.Transaction(func(tx *gorm.DB) error {
		return ReserveGrowthRewardBudgetTx(tx, budgetDate, 500, 1000)
	})
	require.ErrorIs(t, err, ErrGrowthRewardSiteBudgetReached)

	var budget GrowthRewardBudget
	require.NoError(t, DB.Where("budget_date = ?", budgetDate).First(&budget).Error)
	assert.Equal(t, 600, budget.RewardQuota)
}

func TestReserveGrowthRewardBudgetTx_FirstRowIncludesExistingIssuedRewards(t *testing.T) {
	truncateTables(t)
	const budgetDate = "2026-08-16"
	budgetDay, err := time.ParseInLocation("2006-01-02", budgetDate, time.Local)
	require.NoError(t, err)
	rewards := []GrowthReward{
		{UserId: 1, ItemCode: "pending", RewardQuota: 100, Status: GrowthRewardStatusPending, CreatedAt: budgetDay.Unix()},
		{UserId: 2, ItemCode: "settled", RewardQuota: 200, Status: GrowthRewardStatusSettled, CreatedAt: budgetDay.Unix() + 1},
		{UserId: 3, ItemCode: "transferred", RewardQuota: 300, Status: GrowthRewardStatusTransferred, CreatedAt: budgetDay.Unix() + 2},
		{UserId: 4, ItemCode: "reversed", RewardQuota: 900, Status: GrowthRewardStatusReversed, CreatedAt: budgetDay.Unix() + 3},
	}
	require.NoError(t, DB.Create(&rewards).Error)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ReserveGrowthRewardBudgetTx(tx, budgetDate, 100, 1000)
	}))

	var budget GrowthRewardBudget
	require.NoError(t, DB.Where("budget_date = ?", budgetDate).First(&budget).Error)
	assert.Equal(t, 700, budget.RewardQuota)
}

func TestCreateSettledGrowthRewardTx_RejectsWalletOverflow(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 3202, Username: "growth_wallet_limit", Status: common.UserStatusEnabled, Quota: common.MaxWalletQuota - 10}
	require.NoError(t, DB.Create(user).Error)
	reward := NewSettledGrowthReward(user.Id, GrowthRewardItemFirstAPIRequest, 20, 0, "")

	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreateSettledGrowthRewardTx(tx, reward)
	})
	require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)

	var reloaded User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, common.MaxWalletQuota-10, reloaded.Quota)
	var rewardCount int64
	require.NoError(t, DB.Model(&GrowthReward{}).Where("user_id = ?", user.Id).Count(&rewardCount).Error)
	assert.Zero(t, rewardCount)
}
