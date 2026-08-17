package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSettledGrowthRewardRollsBackWhenFundJournalFails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id: 981, Username: "growth-journal-user", Status: common.UserStatusEnabled, Quota: 100,
	}).Error)
	require.NoError(t, DB.Migrator().DropTable(&PromotionFundTransactionLeg{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&PromotionFundTransactionLeg{}))
	})

	_, err := CreateSettledGrowthReward(981, "journal_failure", 500, 0, "test rollback")
	require.Error(t, err)

	var user User
	require.NoError(t, DB.Where("id = ?", 981).First(&user).Error)
	assert.Equal(t, 100, user.Quota)
	var rewardCount int64
	require.NoError(t, DB.Model(&GrowthReward{}).Where("user_id = ?", 981).Count(&rewardCount).Error)
	assert.Zero(t, rewardCount)
	var eventCount int64
	require.NoError(t, DB.Model(&PromotionEvent{}).Where("user_id = ?", 981).Count(&eventCount).Error)
	assert.Zero(t, eventCount)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("user_id = ?", 981).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}
