package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromotionCommissionAccrualAndSettlementJournalExactSources(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 982, Username: "commission-journal-user", Status: common.UserStatusEnabled}).Error)
	ledger := &PromotionCommissionLedger{
		UserId: 982, InviteeId: 983, SourceType: "test_source", SourceId: 1001,
		SourceTradeNo: "source-1001", Cashable: true, Currency: "CNY",
		GrossAmountCents: 1200, NetAmountCents: 1200, Status: PromotionCommissionStatusPending,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionCommissionLedgerTx(tx, ledger)
	}))

	var accrual PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").Where("transaction_key = ?", fmt.Sprintf("commission:%d:accrued", ledger.Id)).First(&accrual).Error)
	assert.Equal(t, PromotionFundKindCommissionPendingAccrued, accrual.Kind)
	require.Len(t, accrual.Legs, 1)
	assert.Equal(t, PromotionFundAccountCommissionPending, accrual.Legs[0].Account)
	assert.Equal(t, int64(1200), accrual.Legs[0].Amount)
	assert.Equal(t, ledger.Id, accrual.Legs[0].SourceId)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return SettlePromotionCommissionLedgerTx(tx, ledger.SourceType, ledger.SourceId, 200)
	}))
	var settlement PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").Where("transaction_key = ?", fmt.Sprintf("commission:%d:settled", ledger.Id)).First(&settlement).Error)
	assert.Equal(t, PromotionFundKindCommissionSettled, settlement.Kind)
	require.Len(t, settlement.Legs, 2)
	assert.Equal(t, PromotionFundAccountCommissionPending, settlement.Legs[0].Account)
	assert.Equal(t, int64(-1200), settlement.Legs[0].Amount)
	assert.Equal(t, PromotionFundAccountCommissionAvailable, settlement.Legs[1].Account)
	assert.Equal(t, int64(1200), settlement.Legs[1].Amount)
	assert.Equal(t, ledger.Id, settlement.Legs[0].SourceId)
	assert.Equal(t, ledger.Id, settlement.Legs[1].SourceId)
}

func TestPromotionCommissionAccrualRollsBackWhenFundJournalFails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 984, Username: "commission-rollback-user", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Migrator().DropTable(&PromotionFundTransactionLeg{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&PromotionFundTransactionLeg{}))
	})
	ledger := &PromotionCommissionLedger{
		UserId: 984, InviteeId: 985, SourceType: "test_source", SourceId: 1002,
		Cashable: true, Currency: "CNY", GrossAmountCents: 500, NetAmountCents: 500,
		Status: PromotionCommissionStatusPending,
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionCommissionLedgerTx(tx, ledger)
	})
	require.Error(t, err)

	var ledgerCount int64
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("user_id = ?", 984).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
	var eventCount int64
	require.NoError(t, DB.Model(&PromotionEvent{}).Where("user_id = ?", 984).Count(&eventCount).Error)
	assert.Zero(t, eventCount)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("user_id = ?", 984).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}
