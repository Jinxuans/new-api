package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useGrowthOptionTestState(t *testing.T) {
	t.Helper()
	useFrontendOptionMigrationDB(t)
	growth := operation_setting.GetGrowthSetting()
	previousGrowth := *growth
	previousInviteRebatePercentage := common.InviteRebatePercentage
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		*growth = previousGrowth
		common.InviteRebatePercentage = previousInviteRebatePercentage
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func TestUpdateOptionsBulkRejectsConflictingLegacyAndCanonicalValues(t *testing.T) {
	useGrowthOptionTestState(t)

	err := UpdateOptionsBulk(map[string]string{
		"InviteRebatePercentage":                  "10",
		"growth_setting.invite_rebate_percentage": "20",
	})

	require.Error(t, err)
	var count int64
	require.NoError(t, DB.Model(&Option{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateOptionsBulkCoalescesMatchingLegacyAndCanonicalValues(t *testing.T) {
	useGrowthOptionTestState(t)

	require.NoError(t, UpdateOptionsBulk(map[string]string{
		"InviteRebatePercentage":                  "12.5",
		"growth_setting.invite_rebate_percentage": "12.5",
	}))

	assert.Equal(t, "12.5", requireOptionValue(t, DB, "growth_setting.invite_rebate_percentage"))
	requireOptionMissing(t, DB, "InviteRebatePercentage")
	assert.Equal(t, 12.5, operation_setting.GetInviteRebatePercentage())
	assert.Equal(t, 12.5, common.InviteRebatePercentage)
}

func TestLoadOptionsMigratesLegacyInviteRebateToCanonicalOption(t *testing.T) {
	useGrowthOptionTestState(t)
	require.NoError(t, DB.Create(&Option{
		Key:   "InviteRebatePercentage",
		Value: "8.25",
	}).Error)

	loadOptionsFromDatabase()

	assert.Equal(t, "8.25", requireOptionValue(t, DB, "growth_setting.invite_rebate_percentage"))
	assert.Equal(t, 8.25, operation_setting.GetInviteRebatePercentage())
	assert.Equal(t, 8.25, common.InviteRebatePercentage)
	assert.Equal(t, "8.25", common.OptionMap["InviteRebatePercentage"])
}
