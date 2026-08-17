package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreateInitialRootUserRecordsOpeningBalanceAtomically(t *testing.T) {
	truncateTables(t)

	root := &User{
		Username: "initial-root-fund",
		Password: "password123",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Quota:    100_000_000,
	}
	require.NoError(t, CreateInitialRootUser(root))

	var transaction PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").Where("user_id = ?", root.Id).First(&transaction).Error)
	assert.Equal(t, PromotionFundKindRootInitialQuotaGranted, transaction.Kind)
	assert.Equal(t, PromotionFundSourceSystemSetup, transaction.SourceType)
	assert.Equal(t, "system", transaction.ActorType)
	require.Len(t, transaction.Legs, 1)
	leg := transaction.Legs[0]
	assert.Equal(t, PromotionFundAccountAPIBalance, leg.Account)
	assert.Equal(t, int64(root.Quota), leg.Amount)
	require.NotNil(t, leg.BalanceAfter)
	assert.Equal(t, int64(root.Quota), *leg.BalanceAfter)
}

func TestCreateInitialRootUserRollsBackWhenOpeningBalanceEvidenceFails(t *testing.T) {
	truncateTables(t)

	expectedErr := errors.New("opening balance journal failed")
	callbackName := "test:fail_root_opening_balance_leg"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "promotion_fund_legs" {
			tx.AddError(expectedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove(callbackName) })

	root := &User{
		Username: "initial-root-rollback",
		Password: "password123",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Quota:    100_000_000,
	}
	require.ErrorIs(t, CreateInitialRootUser(root), expectedErr)

	var userCount int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("username = ?", root.Username).Count(&userCount).Error)
	assert.Zero(t, userCount)
}
