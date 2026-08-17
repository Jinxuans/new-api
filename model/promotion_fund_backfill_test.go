package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openPromotionFundBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openPromotionFundTransactionTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&PromotionFundBackfillCheckpoint{},
		&GrowthReward{},
		&InvitationRebate{},
		&InvitationReward{},
		&PromotionCommissionLedger{},
		&PromotionWithdrawal{},
		&PromotionWithdrawalItem{},
		&PromotionRefundCase{},
		&PromotionRefundCaseUser{},
		&PromotionRefundObligation{},
		&PromotionRefundAction{},
		&PromotionEvent{},
		&Redemption{},
		&SubscriptionOrder{},
		&TopUp{},
	))
	return db
}

func TestBackfillPromotionFundTransactionsIncludesSoftDeletedUsedRedemption(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	redemption := &Redemption{
		Name:         "historical credit",
		Key:          "20000000000000000000000000000001",
		Status:       common.RedemptionCodeStatusUsed,
		Quota:        750,
		CreatedTime:  100,
		RedeemedTime: 200,
		UsedUserId:   83,
	}
	require.NoError(t, db.Create(redemption).Error)
	require.NoError(t, db.Delete(redemption).Error)

	require.NoError(t, BackfillPromotionFundTransactions(db))

	var transaction PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", fmt.Sprintf("pfb:redemptions:%d:credited", redemption.Id)).First(&transaction).Error)
	assert.Equal(t, PromotionFundKindRedemptionCredited, transaction.Kind)
	assert.Equal(t, redemption.UsedUserId, transaction.UserId)
	assert.Equal(t, "redemptions", transaction.SourceType)
	assert.Equal(t, redemption.Id, transaction.SourceId)
	assert.Equal(t, "user", transaction.ActorType)
	assert.Equal(t, redemption.UsedUserId, transaction.ActorId)
	assert.Equal(t, redemption.RedeemedTime, transaction.OccurredAt)
	assert.Equal(t, redemption.Name, transaction.Remark)
	require.Len(t, transaction.Legs, 1)
	assert.Equal(t, PromotionFundAccountAPIBalance, transaction.Legs[0].Account)
	assert.Equal(t, PromotionFundAssetQuota, transaction.Legs[0].Asset)
	assert.Equal(t, int64(redemption.Quota), transaction.Legs[0].Amount)
	assert.Nil(t, transaction.Legs[0].BalanceAfter)
}

func TestBackfillPromotionFundTransactionsReusesRealtimeRedemptionJournal(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	redemption := &Redemption{
		Name:         "realtime credit",
		Key:          "20000000000000000000000000000002",
		Status:       common.RedemptionCodeStatusUsed,
		Quota:        900,
		CreatedTime:  300,
		RedeemedTime: 400,
		UsedUserId:   84,
	}
	require.NoError(t, db.Create(redemption).Error)
	balanceAfter := int64(1_200)
	realtime := &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("redemption:%d:credited", redemption.Id),
		Kind:           PromotionFundKindRedemptionCredited,
		UserId:         redemption.UsedUserId,
		SourceType:     "redemptions",
		SourceId:       redemption.Id,
		SourceKey:      fmt.Sprintf("redemptions:%d", redemption.Id),
		ActorType:      "user",
		ActorId:        redemption.UsedUserId,
		Remark:         redemption.Name,
		OccurredAt:     redemption.RedeemedTime,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, realtime, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
			Amount: int64(redemption.Quota), SourceType: "redemptions", SourceId: redemption.Id,
			BalanceAfter: &balanceAfter,
		}})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))

	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("source_type = ? AND source_id = ?", "redemptions", redemption.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.First(&PromotionFundTransaction{}, realtime.Id).Error)
}

func TestBackfillPromotionFundTransactionsDoesNotInventMalformedLegacyRedemption(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	redemption := &Redemption{
		Name:         "invalid historical value",
		Key:          "20000000000000000000000000000003",
		Status:       common.RedemptionCodeStatusUsed,
		Quota:        0,
		RedeemedTime: 500,
		UsedUserId:   85,
	}
	require.NoError(t, db.Create(redemption).Error)
	require.NoError(t, db.Model(redemption).UpdateColumn("quota", 0).Error)

	require.NoError(t, BackfillPromotionFundTransactions(db))

	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("source_type = ? AND source_id = ?", "redemptions", redemption.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestListPromotionFundTransactionsPaginatesWithLegs(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	for _, transaction := range []*PromotionFundTransaction{
		{TransactionKey: "query:user-51:older", Kind: "test", UserId: 51, OccurredAt: 100},
		{TransactionKey: "query:user-51:newer", Kind: "test", UserId: 51, OccurredAt: 200},
		{TransactionKey: "query:user-52", Kind: "test", UserId: 52, OccurredAt: 300},
	} {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return CreatePromotionFundTransactionTx(tx, transaction, []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountAPIBalance,
				Asset:   PromotionFundAssetQuota,
				Amount:  10,
			}})
		}))
	}

	transactions, total, err := ListPromotionFundTransactions(51, &common.PageInfo{Page: 1, PageSize: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, transactions, 1)
	assert.Equal(t, "query:user-51:newer", transactions[0].TransactionKey)
	require.Len(t, transactions[0].Legs, 1)
	assert.Equal(t, int64(10), transactions[0].Legs[0].Amount)

	transactions, total, err = ListPromotionFundTransactions(51, &common.PageInfo{Page: 2, PageSize: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, transactions, 1)
	assert.Equal(t, "query:user-51:older", transactions[0].TransactionKey)
}

func TestListPromotionFundTransactionsRejectsInvalidOrOverflowingPagination(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	maxInt := int(^uint(0) >> 1)
	testCases := []struct {
		name string
		page *common.PageInfo
	}{
		{name: "nil", page: nil},
		{name: "negative page", page: &common.PageInfo{Page: -1, PageSize: 20}},
		{name: "zero page size", page: &common.PageInfo{Page: 1, PageSize: 0}},
		{name: "page size above API limit", page: &common.PageInfo{Page: 1, PageSize: 101}},
		{name: "offset overflow", page: &common.PageInfo{Page: maxInt, PageSize: 100}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			transactions, total, err := ListPromotionFundTransactions(51, testCase.page)
			require.Error(t, err)
			assert.Nil(t, transactions)
			assert.Zero(t, total)
		})
	}
}

func TestBackfillPromotionFundTransactionsBatchResumesAtTwoHundred(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	rewards := make([]GrowthReward, PromotionFundBackfillBatchSize+1)
	for i := range rewards {
		status := GrowthRewardStatusPending
		if i == PromotionFundBackfillBatchSize {
			status = GrowthRewardStatusSettled
		}
		rewards[i] = GrowthReward{
			Id:          i + 1,
			UserId:      61,
			ItemCode:    fmt.Sprintf("batch_reward_%d", i+1),
			RewardQuota: 1,
			Status:      status,
			CreatedAt:   int64(1_000 + i),
			SettledAt:   int64(1_000 + i),
		}
	}
	require.NoError(t, db.Create(&rewards).Error)
	require.NoError(t, db.Create(&PromotionFundBackfillCheckpoint{
		BackfillKey: PromotionFundBackfillKey,
		Version:     PromotionFundBackfillVersion - 1,
		Completed:   true,
	}).Error)

	first, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.Equal(t, PromotionFundBackfillBatchSize, first.Processed)
	assert.Equal(t, promotionFundBackfillSourceGrowthRewards, first.CursorSource)
	assert.Equal(t, PromotionFundBackfillBatchSize, first.CursorId)
	assert.False(t, first.Completed)

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)

	second, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.Equal(t, 1, second.Processed)
	assert.True(t, second.Completed)
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)

	var checkpointCount int64
	require.NoError(t, db.Model(&PromotionFundBackfillCheckpoint{}).Count(&checkpointCount).Error)
	assert.Equal(t, int64(2), checkpointCount)

	completed, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, completed.Completed)
	assert.Zero(t, completed.Processed)
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)
}

func TestBackfillPromotionFundTransactionsBatchRollsBackCursorAndCanResume(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&GrowthReward{
		Id: 1, UserId: 62, ItemCode: "recoverable_growth", RewardQuota: 20,
		Status: GrowthRewardStatusSettled, CreatedAt: 100, SettledAt: 100,
	}).Error)
	require.NoError(t, db.Create(&InvitationReward{
		Id: 1, InviterId: 62, InviteeId: 6201, RewardType: InvitationRewardTypeRegister,
		RewardQuota: 30, Status: "corrupt_status", CreatedAt: 101, SettledAt: 101,
	}).Error)

	_, err := BackfillPromotionFundTransactionsBatch(db)
	require.Error(t, err)
	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
	var checkpointCount int64
	require.NoError(t, db.Model(&PromotionFundBackfillCheckpoint{}).
		Where("backfill_key = ? AND version = ?", PromotionFundBackfillKey, PromotionFundBackfillVersion).
		Count(&checkpointCount).Error)
	assert.Zero(t, checkpointCount)

	require.NoError(t, db.Model(&InvitationReward{}).Where("id = ?", 1).
		Update("status", InvitationRewardStatusSettled).Error)
	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(2), transactionCount)
}

func TestBackfillPromotionFundTransactionsSkipsPendingInvitationReward(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	reward := &InvitationReward{
		Id: 1, InviterId: 62, InviteeId: 6201, RewardType: InvitationRewardTypeFirstRequest,
		RewardQuota: 30, Status: InvitationRewardStatusPending, CreatedAt: 101,
	}
	require.NoError(t, db.Create(reward).Error)

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestBackfillPromotionCommissionFundTransactionsRejectsUnknownStatus(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	ledger := &PromotionCommissionLedger{
		UserId: 56, SourceType: "test", SourceId: 901, Cashable: true,
		Currency: "CNY", GrossAmountCents: 125, NetAmountCents: 125,
		Status: "legacy_unknown",
	}
	require.NoError(t, db.Create(ledger).Error)

	err := BackfillPromotionFundTransactions(db)
	require.ErrorContains(t, err, fmt.Sprintf("unknown promotion commission status %q for row %d", ledger.Status, ledger.Id))

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", ledger.Id).
		Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
	var availableLegCount int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("source_type = ? AND source_id = ? AND account = ?", "promotion_commission_ledgers", ledger.Id, PromotionFundAccountCommissionAvailable).
		Count(&availableLegCount).Error)
	assert.Zero(t, availableLegCount)
}

func TestPromotionFundReconciliationSkipsOnlyExplicitlyQuarantinedUnknownCommission(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	unknown := &PromotionCommissionLedger{
		UserId: 56, SourceType: "test", SourceId: 902, Cashable: true,
		Currency: "CNY", GrossAmountCents: 125, NetAmountCents: 125,
		Status: "legacy_unknown",
	}
	recoverable := &PromotionCommissionLedger{
		UserId: 56, SourceType: "test", SourceId: 903, Cashable: true,
		Currency: "CNY", GrossAmountCents: 75, NetAmountCents: 75,
		Status: PromotionCommissionStatusSettled,
	}
	require.NoError(t, db.Create(unknown).Error)
	require.NoError(t, db.Create(recoverable).Error)
	refundCase := &PromotionRefundCase{
		EventKey: "quarantined-unknown-commission", Provider: PaymentProviderStripe,
		TradeNo: "quarantined-unknown-order", RefundTradeNo: "quarantined-unknown-refund",
		Kind: PromotionRefundKindFull, CommissionLedgerId: unknown.Id,
		Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, db.Create(refundCase).Error)

	require.ErrorContains(t, ReconcilePromotionFundTransactions(db),
		fmt.Sprintf("unknown promotion commission status %q for row %d", unknown.Status, unknown.Id))
	var recoverableTransactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", recoverable.Id).
		Count(&recoverableTransactionCount).Error)
	assert.Zero(t, recoverableTransactionCount, "an unhandled poison row must roll back the batch")

	require.NoError(t, db.Create(&PromotionRefundAction{
		ActionKey: "quarantined-unknown-action", RefundCaseId: refundCase.Id,
		UserId: unknown.UserId, Action: PromotionRefundActionQuarantineUnknownCommission,
		ActorId: 1, ExternalRef: "PROVIDER-AUDIT-902", Remark: "Root verified this legacy state externally",
		CommissionLedgerId: unknown.Id, CommissionLedgerStatus: unknown.Status,
	}).Error)
	require.NoError(t, ReconcilePromotionFundTransactions(db))

	var storedUnknown PromotionCommissionLedger
	require.NoError(t, db.First(&storedUnknown, unknown.Id).Error)
	assert.Equal(t, "legacy_unknown", storedUnknown.Status)
	var unknownTransactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", unknown.Id).
		Count(&unknownTransactionCount).Error)
	assert.Zero(t, unknownTransactionCount, "quarantine must not fabricate a fund transition")
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", recoverable.Id).
		Count(&recoverableTransactionCount).Error)
	assert.Equal(t, int64(1), recoverableTransactionCount, "the checkpoint must advance to later rows")

	var checkpoint PromotionFundBackfillCheckpoint
	require.NoError(t, db.Where("backfill_key = ? AND version = ?", promotionFundReconcileKey, PromotionFundBackfillVersion).
		First(&checkpoint).Error)
	assert.True(t, checkpoint.Completed)
}

func TestPromotionFundReconciliationCatchesLegacyTransitionsAfterCompletedCheckpoint(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	reward := &GrowthReward{
		Id: 2, UserId: 62, ItemCode: "rolling_deploy_growth", RewardQuota: 40,
		Status: GrowthRewardStatusPending, CreatedAt: 120,
	}
	require.NoError(t, db.Create(reward).Error)
	require.NoError(t, BackfillPromotionFundTransactions(db))
	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)

	// Simulate an older rolling-deployment instance settling a row after the
	// first one-way cursor had already completed.
	require.NoError(t, db.Model(&GrowthReward{}).Where("id = ?", reward.Id).Updates(map[string]interface{}{
		"status": GrowthRewardStatusSettled, "settled_at": int64(130),
	}).Error)
	require.NoError(t, BackfillPromotionFundTransactions(db))
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount, "startup backfill must not replay a completed checkpoint")
	var migrationCheckpoint PromotionFundBackfillCheckpoint
	require.NoError(t, db.Where("backfill_key = ? AND version = ?", PromotionFundBackfillKey, PromotionFundBackfillVersion).
		First(&migrationCheckpoint).Error)
	assert.True(t, migrationCheckpoint.Completed)

	require.NoError(t, ReconcilePromotionFundTransactions(db))
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)
	var reconciliationCheckpoint PromotionFundBackfillCheckpoint
	require.NoError(t, db.Where("backfill_key = ? AND version = ?", promotionFundReconcileKey, PromotionFundBackfillVersion).
		First(&reconciliationCheckpoint).Error)
	assert.True(t, reconciliationCheckpoint.Completed)

	// Reconciliation remains append-only and idempotent on later scheduled runs.
	require.NoError(t, ReconcilePromotionFundTransactions(db))
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)
}

func TestBackfillPromotionFundTransactionsBuildsExactLegsAndHonestLegacyAggregate(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&GrowthReward{
		Id: 1, UserId: 63, ItemCode: "settled_growth", RewardQuota: 100,
		Status: GrowthRewardStatusSettled, CreatedAt: 10, SettledAt: 10,
	}).Error)
	require.NoError(t, db.Create(&InvitationReward{
		Id: 1, InviterId: 63, InviteeId: 6301, RewardType: InvitationRewardTypeRegister,
		RewardQuota: 50, Status: InvitationRewardStatusSettled, CreatedAt: 11, SettledAt: 11,
	}).Error)
	require.NoError(t, db.Create(&[]PromotionCommissionLedger{
		{
			Id: 1, UserId: 63, SourceType: "test", SourceId: 1, Cashable: true, Currency: "CNY",
			GrossAmountCents: 300, NetAmountCents: 300, QuotaEquivalent: 600,
			Status: PromotionCommissionStatusTransferred, CreatedAt: 12, SettledAt: 12, TransferredAt: 20,
		},
		{
			Id: 2, UserId: 63, SourceType: "test", SourceId: 2, Cashable: true, Currency: "CNY",
			GrossAmountCents: 400, NetAmountCents: 400,
			Status: PromotionCommissionStatusWithdrawn, CreatedAt: 13, SettledAt: 13, WithdrawnAt: 40,
		},
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawal{
		Id: 1, UserId: 63, Currency: "CNY", GrossAmountCents: 400, NetAmountCents: 400,
		Status: PromotionWithdrawalStatusPaid, TradeNo: "payout-1", AppliedAt: 30, ReviewedAt: 35, PaidAt: 40, CreatedAt: 30,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawalItem{
		Id: 1, WithdrawalId: 1, LedgerId: 2, AmountCents: 400, CreatedAt: 30,
	}).Error)
	require.NoError(t, db.Create(&[]PromotionEvent{
		{
			Id: 1, EventKey: "legacy-invitation-transfer", UserId: 63,
			EventType: PromotionEventTypePromotionRewardTransferred, SourceTable: PromotionEventSourceInvitationQuota,
			SourceId: 30, QuotaDelta: 30, CreatedAt: 25,
		},
		{
			Id: 2, EventKey: "exact-commission-transfer", UserId: 63,
			EventType: PromotionEventTypeCommissionTransferred, SourceTable: PromotionEventSourceCommissionTransfer,
			SourceId: 20, QuotaDelta: 600, CashAmountCents: 300, Currency: "CNY", CreatedAt: 20,
		},
	}).Error)

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)

	var transactions []PromotionFundTransaction
	require.NoError(t, db.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Order("id ASC").Find(&transactions).Error)
	assert.Len(t, transactions, 8)

	var commissionTransfer PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", "pfb:promotion_commission_ledgers:1:transferred").First(&commissionTransfer).Error)
	require.Len(t, commissionTransfer.Legs, 2)
	assert.Equal(t, PromotionFundAccountCommissionAvailable, commissionTransfer.Legs[0].Account)
	assert.Equal(t, int64(-300), commissionTransfer.Legs[0].Amount)
	assert.Equal(t, PromotionFundAccountAPIBalance, commissionTransfer.Legs[1].Account)
	assert.Equal(t, int64(600), commissionTransfer.Legs[1].Amount)

	var withdrawalReserve PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", "pfb:promotion_withdrawals:1:reserved").First(&withdrawalReserve).Error)
	require.Len(t, withdrawalReserve.Legs, 2)
	assert.Equal(t, "promotion_commission_ledgers", withdrawalReserve.Legs[0].SourceType)
	assert.Equal(t, 2, withdrawalReserve.Legs[0].SourceId)

	var legacy PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("source_key = ?", "promotion_events:1").First(&legacy).Error)
	assert.Equal(t, PromotionFundKindLegacyAggregate, legacy.Kind)
	assert.Equal(t, PromotionFundSourceLegacyAggregate, legacy.SourceType)
	require.Len(t, legacy.Legs, 2)
	assert.Equal(t, PromotionFundSourceLegacyAggregate, legacy.Legs[0].SourceType)
	assert.Equal(t, int64(-30), legacy.Legs[0].Amount)
	assert.Equal(t, int64(30), legacy.Legs[1].Amount)

	var exactEventJournalCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("source_key = ?", "promotion_events:2").Count(&exactEventJournalCount).Error)
	assert.Zero(t, exactEventJournalCount)
}

func TestBackfillPromotionFundTransactionsRejectsInconsistentWithdrawalAllocation(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&PromotionCommissionLedger{
		Id: 9, UserId: 69, SourceType: "test", SourceId: 9, Cashable: true, Currency: "CNY",
		GrossAmountCents: 500, NetAmountCents: 500, Status: PromotionCommissionStatusWithdrawn,
		CreatedAt: 90, SettledAt: 90, WithdrawnAt: 100,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawal{
		Id: 9, UserId: 69, Currency: "CNY", GrossAmountCents: 500, NetAmountCents: 500,
		Status: PromotionWithdrawalStatusPaid, TradeNo: "payout-9", AppliedAt: 95, PaidAt: 100, CreatedAt: 95,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawalItem{
		Id: 9, WithdrawalId: 9, LedgerId: 9, AmountCents: 400, CreatedAt: 95,
	}).Error)

	_, err := BackfillPromotionFundTransactionsBatch(db)
	require.ErrorIs(t, err, ErrPromotionWithdrawalLedgerNotPayable)
	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestBackfillInvitationRewardReversalUsesStoredTransferAllocation(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&InvitationReward{
		Id: 10, InviterId: 64, InviteeId: 6401, RewardType: InvitationRewardTypeFirstTopUp,
		RewardQuota: 75, TransferredQuota: 25, Status: InvitationRewardStatusReversed, CreatedAt: 100, SettledAt: 100,
	}).Error)

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)

	var transactions []PromotionFundTransaction
	require.NoError(t, db.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("source_key = ?", "invitation_rewards:10").Order("id ASC").Find(&transactions).Error)
	require.Len(t, transactions, 2)
	assert.Equal(t, PromotionFundKindInvitationRewardIssued, transactions[0].Kind)
	require.Len(t, transactions[0].Legs, 1)
	assert.Equal(t, PromotionFundAccountReferralCredit, transactions[0].Legs[0].Account)
	assert.Equal(t, int64(75), transactions[0].Legs[0].Amount)
	assert.Equal(t, PromotionFundKindReversal, transactions[1].Kind)
	assert.Equal(t, transactions[0].Id, transactions[1].ReversesTransactionId)
	require.Len(t, transactions[1].Legs, 2)
	assert.Equal(t, PromotionFundAccountReferralCredit, transactions[1].Legs[0].Account)
	assert.Equal(t, int64(-50), transactions[1].Legs[0].Amount)
	assert.Equal(t, PromotionFundAccountAPIBalance, transactions[1].Legs[1].Account)
	assert.Equal(t, int64(-25), transactions[1].Legs[1].Amount)
}

func TestBackfillPromotionFundTransactionsPrefersCanonicalRealtimeTransition(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	reward := &GrowthReward{
		Id: 20, UserId: 65, ItemCode: "realtime_growth", RewardQuota: 90,
		Status: GrowthRewardStatusSettled, CreatedAt: 200, SettledAt: 200,
	}
	require.NoError(t, db.Create(reward).Error)
	balanceAfter := int64(1090)
	realtime := &PromotionFundTransaction{
		TransactionKey: "growth_reward:20:issued", Kind: PromotionFundKindGrowthRewardIssued,
		UserId: 65, SourceType: "growth_rewards", SourceId: 20,
		SourceKey: "growth_rewards:20", ActorType: "system", Remark: "realtime metadata", OccurredAt: 200,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, realtime, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 90,
			SourceType: "growth_rewards", SourceId: 20, BalanceAfter: &balanceAfter,
		}})
	}))

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("transaction_key LIKE ?", "pfb:%").Count(&count).Error)
	assert.Zero(t, count)
}

func TestBackfillPromotionFundTransactionsRecognizesRealtimeInvitationTransferAllocation(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	event := &PromotionEvent{
		Id: 24, EventKey: "realtime-invitation-transfer", UserId: 65,
		EventType: PromotionEventTypePromotionRewardTransferred, SourceTable: PromotionEventSourceInvitationQuota,
		SourceId: 65, QuotaDelta: 50, CreatedAt: 240,
	}
	require.NoError(t, db.Create(event).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
			TransactionKey: "invitation_transfer:24", Kind: PromotionFundKindInvitationRewardTransferred,
			UserId: 65, SourceType: "promotion_events", SourceId: 24, SourceKey: event.EventKey,
			ActorType: "user", ActorId: 65, OccurredAt: 240,
		}, []PromotionFundTransactionLeg{
			{Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota, Amount: -20, SourceType: "invitation_rewards", SourceId: 100},
			{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 20, SourceType: "invitation_rewards", SourceId: 100},
			{Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota, Amount: -30, SourceType: PromotionFundSourceLegacyAggregate},
			{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 30, SourceType: PromotionFundSourceLegacyAggregate},
		})
	}))

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestBackfillPromotionFundTransactionsUsesRealtimeAccrualStateWhenTimestampsTie(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	ledger := &PromotionCommissionLedger{
		Id: 25, UserId: 66, SourceType: "test", SourceId: 25, Cashable: true, Currency: "CNY",
		GrossAmountCents: 100, NetAmountCents: 100, QuotaEquivalent: 200,
		Status: PromotionCommissionStatusTransferred, CreatedAt: 250, SettledAt: 250, TransferredAt: 260,
	}
	require.NoError(t, db.Create(ledger).Error)
	transactions := []struct {
		header *PromotionFundTransaction
		legs   []PromotionFundTransactionLeg
	}{
		{
			header: &PromotionFundTransaction{TransactionKey: "commission:25:accrued", Kind: PromotionFundKindCommissionPendingAccrued, UserId: 66, SourceType: "promotion_commission_ledgers", SourceId: 25, OccurredAt: 250},
			legs:   []PromotionFundTransactionLeg{{Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 100, SourceType: "promotion_commission_ledgers", SourceId: 25}},
		},
		{
			header: &PromotionFundTransaction{TransactionKey: "commission:25:settled", Kind: PromotionFundKindCommissionSettled, UserId: 66, SourceType: "promotion_commission_ledgers", SourceId: 25, OccurredAt: 250},
			legs: []PromotionFundTransactionLeg{
				{Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: -100, SourceType: "promotion_commission_ledgers", SourceId: 25},
				{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 100, SourceType: "promotion_commission_ledgers", SourceId: 25},
			},
		},
		{
			header: &PromotionFundTransaction{TransactionKey: "commission:25:transferred", Kind: PromotionFundKindCommissionTransferredToBalance, UserId: 66, SourceType: "promotion_commission_ledgers", SourceId: 25, OccurredAt: 260},
			legs: []PromotionFundTransactionLeg{
				{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: -100, SourceType: "promotion_commission_ledgers", SourceId: 25},
				{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 200, SourceType: "promotion_commission_ledgers", SourceId: 25},
			},
		},
	}
	for _, transaction := range transactions {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return CreatePromotionFundTransactionTx(tx, transaction.header, transaction.legs)
		}))
	}

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&count).Error)
	assert.Equal(t, int64(3), count)
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("transaction_key LIKE ?", "pfb:%").Count(&count).Error)
	assert.Zero(t, count)
}

func TestReconcilePromotionFundTransactionsReusesBackfillAccrualWhenSettlementTimestampTies(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	ledger := &PromotionCommissionLedger{
		Id: 43, UserId: 76, SourceType: "test", SourceId: 43, Cashable: true, Currency: "CNY",
		GrossAmountCents: 100, NetAmountCents: 100,
		Status: PromotionCommissionStatusPending, CreatedAt: 430,
	}
	require.NoError(t, db.Create(ledger).Error)
	require.NoError(t, ReconcilePromotionFundTransactions(db))

	require.NoError(t, db.Model(ledger).Updates(map[string]interface{}{
		"status":     PromotionCommissionStatusSettled,
		"settled_at": ledger.CreatedAt,
	}).Error)
	require.NoError(t, ReconcilePromotionFundTransactions(db))

	var transactions []PromotionFundTransaction
	require.NoError(t, db.Preload("Legs", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("id ASC")
	}).Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", ledger.Id).
		Order("id ASC").Find(&transactions).Error)
	require.Len(t, transactions, 2)
	assert.Equal(t, "pfb:promotion_commission_ledgers:43:accrued", transactions[0].TransactionKey)
	assert.Equal(t, PromotionFundKindCommissionPendingAccrued, transactions[0].Kind)
	assert.Equal(t, "pfb:promotion_commission_ledgers:43:settled", transactions[1].TransactionKey)
	assert.Equal(t, PromotionFundKindCommissionSettled, transactions[1].Kind)
}

func TestBackfillPromotionFundTransactionsRejectsConflictingCanonicalTransition(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&GrowthReward{
		Id: 21, UserId: 66, ItemCode: "conflicting_growth", RewardQuota: 90,
		Status: GrowthRewardStatusSettled, CreatedAt: 210, SettledAt: 210,
	}).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
			TransactionKey: "growth_reward:21:issued", Kind: PromotionFundKindGrowthRewardIssued,
			UserId: 66, SourceType: "growth_rewards", SourceId: 21, OccurredAt: 210,
		}, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 89,
			SourceType: "growth_rewards", SourceId: 21,
		}})
	}))

	_, err := BackfillPromotionFundTransactionsBatch(db)
	require.ErrorIs(t, err, ErrPromotionFundTransactionConflict)
	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("transaction_key LIKE ?", "pfb:%").Count(&count).Error)
	assert.Zero(t, count)
}

func TestBackfillPromotionFundTransactionsRecognizesCanonicalRefundReversals(t *testing.T) {
	t.Run("growth reward with debt allocation", func(t *testing.T) {
		db := openPromotionFundBackfillTestDB(t)
		reward := &GrowthReward{
			Id: 26, UserId: 72, ItemCode: "refunded_growth", RewardQuota: 100,
			Status: GrowthRewardStatusReversed, CreatedAt: 260, SettledAt: 260,
		}
		require.NoError(t, db.Create(reward).Error)
		refundCase := &PromotionRefundCase{
			Id: 61, EventKey: "growth-refund-case", RefundTradeNo: "growth-refund",
			UserId: reward.UserId, Status: PromotionRefundCaseStatusPendingReview,
		}
		require.NoError(t, db.Create(refundCase).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			issued := &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("growth_reward:%d:issued", reward.Id),
				Kind:           PromotionFundKindGrowthRewardIssued, UserId: reward.UserId,
				SourceType: "growth_rewards", SourceId: reward.Id, OccurredAt: reward.SettledAt,
			}
			if err := CreatePromotionFundTransactionTx(tx, issued, []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
				Amount: int64(reward.RewardQuota), SourceType: "growth_rewards", SourceId: reward.Id,
			}}); err != nil {
				return err
			}
			if err := CreatePromotionRefundObligationTx(tx, &PromotionRefundObligation{
				ObligationKey: fmt.Sprintf("refund:%d:growth_reward:%d:debt", refundCase.Id, reward.Id),
				RefundCaseId:  refundCase.Id, UserId: reward.UserId,
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 20,
				SourceType: "growth_rewards", SourceId: reward.Id,
			}); err != nil {
				return err
			}
			return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("refund:%d:growth_reward:%d", refundCase.Id, reward.Id),
				Kind:           PromotionFundKindReversal, UserId: reward.UserId,
				SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
				ReversesTransactionId: issued.Id, OccurredAt: 270,
			}, []PromotionFundTransactionLeg{
				{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: -80, SourceType: "growth_rewards", SourceId: reward.Id},
				{Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 20, SourceType: "growth_rewards", SourceId: reward.Id},
			})
		}))

		require.NoError(t, BackfillPromotionFundTransactions(db))
		var count int64
		require.NoError(t, db.Model(&PromotionFundTransaction{}).
			Where("transaction_key = ?", fmt.Sprintf("pfb:growth_rewards:%d:reversed", reward.Id)).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("invitation reward with transferred and debt allocation", func(t *testing.T) {
		db := openPromotionFundBackfillTestDB(t)
		reward := &InvitationReward{
			Id: 29, InviterId: 75, InviteeId: 7501, RewardType: InvitationRewardTypeFirstTopUp,
			RewardQuota: 75, TransferredQuota: 30, Status: InvitationRewardStatusReversed,
			CreatedAt: 290, SettledAt: 290,
		}
		require.NoError(t, db.Create(reward).Error)
		refundCase := &PromotionRefundCase{
			Id: 65, EventKey: "invitation-refund-case", RefundTradeNo: "invitation-refund",
			Status: PromotionRefundCaseStatusPendingReview,
		}
		require.NoError(t, db.Create(refundCase).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			issued := &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("invitation_reward:%d:issued", reward.Id),
				Kind:           PromotionFundKindInvitationRewardIssued, UserId: reward.InviterId,
				SourceType: "invitation_rewards", SourceId: reward.Id, OccurredAt: reward.SettledAt,
			}
			if err := CreatePromotionFundTransactionTx(tx, issued, []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota,
				Amount: int64(reward.RewardQuota), SourceType: "invitation_rewards", SourceId: reward.Id,
			}}); err != nil {
				return err
			}
			if err := CreatePromotionRefundObligationTx(tx, &PromotionRefundObligation{
				ObligationKey: fmt.Sprintf("refund:%d:invitation_reward:%d:debt", refundCase.Id, reward.Id),
				RefundCaseId:  refundCase.Id, UserId: reward.InviterId,
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 5,
				SourceType: "invitation_rewards", SourceId: reward.Id,
			}); err != nil {
				return err
			}
			return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("refund:%d:invitation_reward:%d", refundCase.Id, reward.Id),
				Kind:           PromotionFundKindReversal, UserId: reward.InviterId,
				SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
				ReversesTransactionId: issued.Id, OccurredAt: 300,
			}, []PromotionFundTransactionLeg{
				{Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota, Amount: -50, SourceType: "invitation_rewards", SourceId: reward.Id},
				{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: -20, SourceType: "invitation_rewards", SourceId: reward.Id},
				{Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 5, SourceType: "invitation_rewards", SourceId: reward.Id},
			})
		}))

		require.NoError(t, BackfillPromotionFundTransactions(db))
		var count int64
		require.NoError(t, db.Model(&PromotionFundTransaction{}).
			Where("transaction_key = ?", fmt.Sprintf("pfb:invitation_rewards:%d:reversed", reward.Id)).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("cash commission", func(t *testing.T) {
		db := openPromotionFundBackfillTestDB(t)
		ledger := &PromotionCommissionLedger{
			Id: 27, UserId: 73, SourceType: "test", SourceId: 27, Cashable: true, Currency: "CNY",
			GrossAmountCents: 300, NetAmountCents: 300, Status: PromotionCommissionStatusReversed,
			CreatedAt: 270, ReversedAt: 280, ReversalAmountCents: 300,
		}
		require.NoError(t, db.Create(ledger).Error)
		refundCase := &PromotionRefundCase{
			Id: 62, EventKey: "commission-refund-case", RefundTradeNo: "commission-refund",
			CommissionLedgerId: ledger.Id, Status: PromotionRefundCaseStatusPendingReview,
		}
		require.NoError(t, db.Create(refundCase).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			accrual := &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("commission:%d:accrued", ledger.Id),
				Kind:           PromotionFundKindCommissionPendingAccrued, UserId: ledger.UserId,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id, OccurredAt: ledger.CreatedAt,
			}
			if err := CreatePromotionFundTransactionTx(tx, accrual, []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash, Currency: ledger.Currency,
				Amount: ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}}); err != nil {
				return err
			}
			return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("refund:%d:commission:%d", refundCase.Id, ledger.Id),
				Kind:           PromotionFundKindReversal, UserId: ledger.UserId,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
				ReversesTransactionId: accrual.Id, OccurredAt: ledger.ReversedAt,
			}, []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash, Currency: ledger.Currency,
				Amount: -ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}})
		}))

		require.NoError(t, BackfillPromotionFundTransactions(db))
		var count int64
		require.NoError(t, db.Model(&PromotionFundTransaction{}).
			Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:reversed", ledger.Id)).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("converted commission with debt allocation", func(t *testing.T) {
		db := openPromotionFundBackfillTestDB(t)
		ledger := &PromotionCommissionLedger{
			Id: 30, UserId: 76, SourceType: "test", SourceId: 30, Cashable: true, Currency: "CNY",
			GrossAmountCents: 300, NetAmountCents: 300, QuotaEquivalent: 200,
			Status: PromotionCommissionStatusReversed, CreatedAt: 300, SettledAt: 300,
			TransferredAt: 310, ReversedAt: 320, ReversalAmountCents: 300, ReversalQuota: 200,
		}
		require.NoError(t, db.Create(ledger).Error)
		refundCase := &PromotionRefundCase{
			Id: 66, EventKey: "converted-commission-refund-case", RefundTradeNo: "converted-commission-refund",
			CommissionLedgerId: ledger.Id, Status: PromotionRefundCaseStatusPendingReview,
		}
		require.NoError(t, db.Create(refundCase).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			accrual := &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("commission:%d:accrued", ledger.Id),
				Kind:           PromotionFundKindCommissionAvailableAccrued, UserId: ledger.UserId,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id, OccurredAt: ledger.CreatedAt,
			}
			if err := CreatePromotionFundTransactionTx(tx, accrual, []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: ledger.Currency,
				Amount: ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}}); err != nil {
				return err
			}
			transfer := &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("commission:%d:transferred", ledger.Id),
				Kind:           PromotionFundKindCommissionTransferredToBalance, UserId: ledger.UserId,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id, OccurredAt: ledger.TransferredAt,
			}
			if err := CreatePromotionFundTransactionTx(tx, transfer, []PromotionFundTransactionLeg{
				{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: -ledger.NetAmountCents, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
				{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: int64(ledger.QuotaEquivalent), SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
			}); err != nil {
				return err
			}
			if err := CreatePromotionRefundObligationTx(tx, &PromotionRefundObligation{
				ObligationKey: fmt.Sprintf("refund:%d:commission:%d:debt", refundCase.Id, ledger.Id),
				RefundCaseId:  refundCase.Id, UserId: ledger.UserId,
				Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 50,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}); err != nil {
				return err
			}
			return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("refund:%d:commission:%d", refundCase.Id, ledger.Id),
				Kind:           PromotionFundKindReversal, UserId: ledger.UserId,
				SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
				ReversesTransactionId: transfer.Id, OccurredAt: ledger.ReversedAt,
			}, []PromotionFundTransactionLeg{
				{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: -150, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
				{Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: 50, SourceType: "promotion_commission_ledgers", SourceId: ledger.Id},
			})
		}))

		require.NoError(t, BackfillPromotionFundTransactions(db))
		var count int64
		require.NoError(t, db.Model(&PromotionFundTransaction{}).
			Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:reversed", ledger.Id)).Count(&count).Error)
		assert.Zero(t, count)
	})
}

func TestBackfillPromotionFundTransactionsRejectsInvalidRefundReversalAliasAndPayload(t *testing.T) {
	testCases := []struct {
		name           string
		transactionKey string
		amount         int64
		legSourceId    int
	}{
		{name: "invalid alias", transactionKey: "unrelated:growth:reversal", amount: -90, legSourceId: 28},
		{name: "conflicting amount", transactionKey: "refund:63:growth_reward:28", amount: -89, legSourceId: 28},
		{name: "conflicting source", transactionKey: "refund:63:growth_reward:28", amount: -90, legSourceId: 29},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openPromotionFundBackfillTestDB(t)
			reward := &GrowthReward{
				Id: 28, UserId: 74, ItemCode: "conflicting_refund_growth", RewardQuota: 90,
				Status: GrowthRewardStatusReversed, CreatedAt: 280, SettledAt: 280,
			}
			require.NoError(t, db.Create(reward).Error)
			refundCase := &PromotionRefundCase{
				Id: 63, EventKey: "conflicting-growth-refund", RefundTradeNo: "conflicting-growth-refund",
				UserId: reward.UserId, Status: PromotionRefundCaseStatusPendingReview,
			}
			require.NoError(t, db.Create(refundCase).Error)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
					TransactionKey: testCase.transactionKey, Kind: PromotionFundKindReversal,
					UserId: reward.UserId, SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
				}, []PromotionFundTransactionLeg{{
					Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: testCase.amount,
					SourceType: "growth_rewards", SourceId: testCase.legSourceId,
				}})
			}))

			_, err := BackfillPromotionFundTransactionsBatch(db)
			require.ErrorIs(t, err, ErrPromotionFundTransactionConflict)
			var count int64
			require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("transaction_key LIKE ?", "pfb:%").Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestBackfillPromotionFundTransactionsIsStableAcrossCheckpointVersions(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&GrowthReward{
		Id: 22, UserId: 67, ItemCode: "versioned_growth", RewardQuota: 70,
		Status: GrowthRewardStatusSettled, CreatedAt: 220, SettledAt: 220,
	}).Error)
	legacyBackfill := &PromotionFundTransaction{
		TransactionKey: "pfb:growth_rewards:22:issued", Kind: PromotionFundKindGrowthRewardIssued,
		UserId: 67, SourceType: "growth_rewards", SourceId: 22, SourceKey: "growth_rewards:22",
		ActorType: "system", ActorRef: "backfill:v1", OccurredAt: 220,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, legacyBackfill, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 70,
			SourceType: "growth_rewards", SourceId: 22,
		}})
	}))

	progress, err := backfillPromotionFundTransactionsBatch(db, PromotionFundBackfillVersion+1)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	var count int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var persisted PromotionFundTransaction
	require.NoError(t, db.First(&persisted, legacyBackfill.Id).Error)
	assert.Equal(t, "backfill:v1", persisted.ActorRef)

	require.NoError(t, db.Create(&GrowthReward{
		Id: 23, UserId: 67, ItemCode: "stable_actor_growth", RewardQuota: 30,
		Status: GrowthRewardStatusSettled, CreatedAt: 230, SettledAt: 230,
	}).Error)
	progress, err = backfillPromotionFundTransactionsBatch(db, PromotionFundBackfillVersion+2)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	var stableActor PromotionFundTransaction
	require.NoError(t, db.Where("transaction_key = ?", "pfb:growth_rewards:23:issued").First(&stableActor).Error)
	assert.Equal(t, promotionFundBackfillActorRef, stableActor.ActorRef)
	assert.NotContains(t, stableActor.ActorRef, "v")
}

func TestBackfillWithdrawnCommissionReversalDoesNotInventDebtBeforeRootReview(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&PromotionCommissionLedger{
		Id: 30, UserId: 68, SourceType: "test", SourceId: 30, Cashable: false, Currency: "CNY",
		GrossAmountCents: 500, NetAmountCents: 500, Status: PromotionCommissionStatusReversed,
		CreatedAt: 300, SettledAt: 310, WithdrawnAt: 330, ReversedAt: 340, ReversalAmountCents: 500,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawal{
		Id: 31, UserId: 68, Currency: "CNY", GrossAmountCents: 500, NetAmountCents: 500,
		Status: PromotionWithdrawalStatusPaid, TradeNo: "paid-31", AppliedAt: 320, ReviewedAt: 325, PaidAt: 330, CreatedAt: 320,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawalItem{
		Id: 32, WithdrawalId: 31, LedgerId: 30, AmountCents: 500, CreatedAt: 320,
	}).Error)

	progress, err := BackfillPromotionFundTransactionsBatch(db)
	require.NoError(t, err)
	assert.True(t, progress.Completed)
	type accountTotal struct {
		Account string
		Amount  int64
	}
	var totals []accountTotal
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Select("account, COALESCE(SUM(amount), 0) AS amount").Group("account").Scan(&totals).Error)
	amountByAccount := make(map[string]int64, len(totals))
	for _, total := range totals {
		amountByAccount[total.Account] = total.Amount
	}
	assert.Zero(t, amountByAccount[PromotionFundAccountCommissionAvailable])
	assert.Zero(t, amountByAccount[PromotionFundAccountCommissionReserved])
	assert.Zero(t, amountByAccount[PromotionFundAccountRefundDebt])

	var debtCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", "pfb:promotion_commission_ledgers:30:paid_refund_debt").Count(&debtCount).Error)
	assert.Zero(t, debtCount)
	var obligationCount int64
	require.NoError(t, db.Model(&PromotionRefundObligation{}).Count(&obligationCount).Error)
	assert.Zero(t, obligationCount)
	var payout PromotionFundTransaction
	require.NoError(t, db.Where("transaction_key = ?", "pfb:promotion_withdrawals:31:paid").First(&payout).Error)
	assert.NotZero(t, payout.Id)
}

func TestBackfillWithdrawnCommissionReversalDoesNotDuplicateRootAssessedDebt(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	ledger := &PromotionCommissionLedger{
		Id: 32, UserId: 70, SourceType: "test", SourceId: 32, Cashable: false, Currency: "CNY",
		GrossAmountCents: 500, NetAmountCents: 500, Status: PromotionCommissionStatusReversed,
		CreatedAt: 350, SettledAt: 360, WithdrawnAt: 380, ReversedAt: 390, ReversalAmountCents: 500,
	}
	require.NoError(t, db.Create(ledger).Error)
	refundCase := &PromotionRefundCase{
		Id: 64, EventKey: "root-reviewed-paid-commission", RefundTradeNo: "root-reviewed-paid-commission",
		UserId: ledger.UserId, CommissionLedgerId: ledger.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, db.Create(refundCase).Error)
	obligation := &PromotionRefundObligation{
		ObligationKey: "refund-action:root-assessment:obligation", RefundCaseId: refundCase.Id,
		UserId: ledger.UserId, Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
		Currency: ledger.Currency, Amount: ledger.NetAmountCents,
		SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := CreatePromotionRefundObligationTx(tx, obligation); err != nil {
			return err
		}
		return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
			TransactionKey: "refund-action:root-assessment:fund", Kind: "refund_debt_assessment",
			UserId: ledger.UserId, SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
			ActorType: "admin", ActorId: 1,
		}, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
			Currency: ledger.Currency, Amount: ledger.NetAmountCents,
			SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
		}})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))
	var debtTotal int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("account = ? AND asset = ? AND currency = ?", PromotionFundAccountRefundDebt, PromotionFundAssetCash, ledger.Currency).
		Select("COALESCE(SUM(amount), 0)").Scan(&debtTotal).Error)
	assert.Equal(t, ledger.NetAmountCents, debtTotal)
	var backfillDebtCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:paid_refund_debt", ledger.Id)).Count(&backfillDebtCount).Error)
	assert.Zero(t, backfillDebtCount)
}

func TestBackfillWithdrawnCommissionReversalPreservesRealtimeDebtWithObligation(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	ledger := &PromotionCommissionLedger{
		Id: 35, UserId: 77, SourceType: "test", SourceId: 35, Cashable: false, Currency: "CNY",
		GrossAmountCents: 550, NetAmountCents: 550, Status: PromotionCommissionStatusReversed,
		CreatedAt: 350, SettledAt: 360, WithdrawnAt: 380, ReversedAt: 390, ReversalAmountCents: 550,
	}
	require.NoError(t, db.Create(ledger).Error)
	refundCase := &PromotionRefundCase{
		Id: 67, EventKey: "realtime-paid-commission", RefundTradeNo: "realtime-paid-commission",
		UserId: ledger.UserId, CommissionLedgerId: ledger.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, db.Create(refundCase).Error)
	canonicalKey := fmt.Sprintf("refund:%d:promotion_commission_ledgers:%d:cash_debt", refundCase.Id, ledger.Id)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := CreatePromotionRefundObligationTx(tx, &PromotionRefundObligation{
			ObligationKey: fmt.Sprintf("refund:%d:promotion_commission_ledgers:%d:cash", refundCase.Id, ledger.Id),
			RefundCaseId:  refundCase.Id, UserId: ledger.UserId,
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
			Currency: ledger.Currency, Amount: ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		}); err != nil {
			return err
		}
		return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
			TransactionKey: canonicalKey, Kind: PromotionFundKindReversal,
			UserId: ledger.UserId, SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
			ActorType: "provider", ActorRef: "stripe", ExternalRef: refundCase.RefundTradeNo,
		}, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
			Currency: ledger.Currency, Amount: ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		}})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))
	require.NoError(t, BackfillPromotionFundTransactions(db))
	var debtTotal int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("account = ? AND asset = ? AND currency = ?", PromotionFundAccountRefundDebt, PromotionFundAssetCash, ledger.Currency).
		Select("COALESCE(SUM(amount), 0)").Scan(&debtTotal).Error)
	assert.Equal(t, ledger.NetAmountCents, debtTotal)
	var canonicalCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("transaction_key = ?", canonicalKey).Count(&canonicalCount).Error)
	assert.Equal(t, int64(1), canonicalCount)
	var backfillDebtCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:paid_refund_debt", ledger.Id)).Count(&backfillDebtCount).Error)
	assert.Zero(t, backfillDebtCount)
	var obligationCount int64
	require.NoError(t, db.Model(&PromotionRefundObligation{}).Where("refund_case_id = ?", refundCase.Id).Count(&obligationCount).Error)
	assert.Equal(t, int64(1), obligationCount)
}

func TestBackfillWithdrawnCommissionReversalCorrectsObsoleteInventedDebtAppendOnly(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	ledger := &PromotionCommissionLedger{
		Id: 36, UserId: 75, SourceType: "test", SourceId: 36, Cashable: false, Currency: "CNY",
		GrossAmountCents: 450, NetAmountCents: 450, Status: PromotionCommissionStatusReversed,
		CreatedAt: 360, SettledAt: 370, WithdrawnAt: 390, ReversedAt: 400, ReversalAmountCents: 450,
	}
	require.NoError(t, db.Create(ledger).Error)
	obsoleteDebt := &PromotionFundTransaction{
		TransactionKey: "pfb:promotion_commission_ledgers:36:paid_refund_debt",
		Kind:           PromotionFundKindReversal,
		UserId:         ledger.UserId,
		SourceType:     "promotion_commission_ledgers",
		SourceId:       ledger.Id,
		SourceKey:      "promotion_commission_ledgers:36",
		ActorType:      "system",
		ActorRef:       "backfill:v2",
		OccurredAt:     ledger.ReversedAt,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, obsoleteDebt, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
			Currency: ledger.Currency, Amount: ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		}})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))
	require.NoError(t, BackfillPromotionFundTransactions(db))
	var debtTotal int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("account = ? AND asset = ? AND currency = ?", PromotionFundAccountRefundDebt, PromotionFundAssetCash, ledger.Currency).
		Select("COALESCE(SUM(amount), 0)").Scan(&debtTotal).Error)
	assert.Zero(t, debtTotal)
	var correction PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").
		Where("transaction_key = ?", "pfb:promotion_commission_ledgers:36:paid_refund_debt_correction").First(&correction).Error)
	assert.Equal(t, obsoleteDebt.Id, correction.ReversesTransactionId)
	require.Len(t, correction.Legs, 1)
	assert.Equal(t, int64(-450), correction.Legs[0].Amount)
	var correctionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", correction.TransactionKey).Count(&correctionCount).Error)
	assert.Equal(t, int64(1), correctionCount)
	var obligationCount int64
	require.NoError(t, db.Model(&PromotionRefundObligation{}).Count(&obligationCount).Error)
	assert.Zero(t, obligationCount)
}

func TestBackfillVersionUpgradeCorrectsObsoletePaidCommissionDebitAppendOnly(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	require.NoError(t, db.Create(&PromotionCommissionLedger{
		Id: 33, UserId: 71, SourceType: "test", SourceId: 33, Cashable: false, Currency: "CNY",
		GrossAmountCents: 700, NetAmountCents: 700, Status: PromotionCommissionStatusReversed,
		CreatedAt: 500, SettledAt: 500, WithdrawnAt: 520, ReversedAt: 530, ReversalAmountCents: 700,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawal{
		Id: 34, UserId: 71, Currency: "CNY", GrossAmountCents: 700, NetAmountCents: 700,
		Status: PromotionWithdrawalStatusPaid, TradeNo: "paid-34", AppliedAt: 510, ReviewedAt: 515, PaidAt: 520, CreatedAt: 510,
	}).Error)
	require.NoError(t, db.Create(&PromotionWithdrawalItem{
		Id: 35, WithdrawalId: 34, LedgerId: 33, AmountCents: 700, CreatedAt: 510,
	}).Error)
	obsolete := &PromotionFundTransaction{
		TransactionKey: "pfb:promotion_commission_ledgers:33:reversed",
		Kind:           PromotionFundKindCommissionReversed,
		UserId:         71,
		SourceType:     "promotion_commission_ledgers",
		SourceId:       33,
		SourceKey:      "promotion_commission_ledgers:33",
		ActorType:      "system",
		ActorRef:       "backfill:v1",
		OccurredAt:     530,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, obsolete, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash,
			Currency: "CNY", Amount: -700, SourceType: "promotion_commission_ledgers", SourceId: 33,
		}})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))
	type accountTotal struct {
		Account string
		Amount  int64
	}
	var totals []accountTotal
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Select("account, COALESCE(SUM(amount), 0) AS amount").Group("account").Scan(&totals).Error)
	amountByAccount := make(map[string]int64, len(totals))
	for _, total := range totals {
		amountByAccount[total.Account] = total.Amount
	}
	assert.Zero(t, amountByAccount[PromotionFundAccountCommissionAvailable])
	assert.Zero(t, amountByAccount[PromotionFundAccountCommissionReserved])
	assert.Zero(t, amountByAccount[PromotionFundAccountRefundDebt])

	var correction PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", "pfb:promotion_commission_ledgers:33:paid_refund_reversal_correction").First(&correction).Error)
	assert.Equal(t, obsolete.Id, correction.ReversesTransactionId)
	require.Len(t, correction.Legs, 1)
	assert.Equal(t, int64(700), correction.Legs[0].Amount)
}

func TestFreezeUnverifiedTopUpCommissionBeforeFundBackfillSkipsAvailableAccrual(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.Create(&InvitationRebate{
		Id: 40, InviterId: 69, InviteeId: 70, TopUpId: 71, PaidAmountVerified: false,
		Status: InvitationRebateStatusSettled,
	}).Error)
	require.NoError(t, db.Create(&PromotionCommissionLedger{
		Id: 40, UserId: 69, SourceType: PromotionCommissionSourceTopUpRebate, SourceId: 40,
		Cashable: true, Currency: "CNY", GrossAmountCents: 600, NetAmountCents: 600,
		Status: PromotionCommissionStatusSettled, CreatedAt: 400, SettledAt: 400,
	}).Error)

	require.NoError(t, FreezeUnverifiedTopUpPromotionCommissions())
	var ledger PromotionCommissionLedger
	require.NoError(t, db.First(&ledger, 40).Error)
	assert.False(t, ledger.Cashable)
	require.NoError(t, BackfillPromotionFundTransactions(db))
	var availableLegs int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("account = ?", PromotionFundAccountCommissionAvailable).Count(&availableLegs).Error)
	assert.Zero(t, availableLegs)
}

func TestBackfillPromotionFundTransactionsCorrectsFrozenUnverifiedAccrualAppendOnly(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	rebate := &InvitationRebate{
		Id: 41, InviterId: 71, InviteeId: 72, TopUpId: 73, PaidAmountVerified: false,
		Status: InvitationRebateStatusSettled,
	}
	require.NoError(t, db.Create(rebate).Error)
	ledger := &PromotionCommissionLedger{
		Id: 41, UserId: rebate.InviterId, SourceType: PromotionCommissionSourceTopUpRebate, SourceId: rebate.Id,
		Cashable: false, Currency: "CNY", GrossAmountCents: 600, NetAmountCents: 600,
		Status: PromotionCommissionStatusSettled, CreatedAt: 410, SettledAt: 410,
	}
	require.NoError(t, db.Create(ledger).Error)
	original := &PromotionFundTransaction{
		TransactionKey: "pfb:promotion_commission_ledgers:41:accrued",
		Kind:           PromotionFundKindCommissionAvailableAccrued,
		UserId:         ledger.UserId,
		SourceType:     "promotion_commission_ledgers",
		SourceId:       ledger.Id,
		SourceKey:      "promotion_commission_ledgers:41",
		ActorType:      "system",
		ActorRef:       "backfill:v1",
		OccurredAt:     ledger.CreatedAt,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, original, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash,
			Currency: "CNY", Amount: ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		}})
	}))

	pendingRebate := &InvitationRebate{
		Id: 42, InviterId: 74, InviteeId: 75, TopUpId: 76, PaidAmountVerified: false,
		Status: InvitationRebateStatusSettled,
	}
	require.NoError(t, db.Create(pendingRebate).Error)
	pendingLedger := &PromotionCommissionLedger{
		Id: 42, UserId: pendingRebate.InviterId, SourceType: PromotionCommissionSourceTopUpRebate, SourceId: pendingRebate.Id,
		Cashable: false, Currency: "CNY", GrossAmountCents: 400, NetAmountCents: 400,
		Status: PromotionCommissionStatusSettled, CreatedAt: 420, SettledAt: 430,
	}
	require.NoError(t, db.Create(pendingLedger).Error)
	pendingAccrual := &PromotionFundTransaction{
		TransactionKey: "commission:42:accrued", Kind: PromotionFundKindCommissionPendingAccrued,
		UserId: pendingLedger.UserId, SourceType: "promotion_commission_ledgers", SourceId: pendingLedger.Id,
		SourceKey: "promotion_commission_ledgers:42", ActorType: "system", OccurredAt: pendingLedger.CreatedAt,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, pendingAccrual, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash,
			Currency: "CNY", Amount: pendingLedger.NetAmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: pendingLedger.Id,
		}})
	}))
	settlement := &PromotionFundTransaction{
		TransactionKey: "commission:42:settled", Kind: PromotionFundKindCommissionSettled,
		UserId: pendingLedger.UserId, SourceType: "promotion_commission_ledgers", SourceId: pendingLedger.Id,
		SourceKey: "promotion_commission_ledgers:42", ActorType: "system", OccurredAt: pendingLedger.SettledAt,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, settlement, []PromotionFundTransactionLeg{
			{
				Account: PromotionFundAccountCommissionPending, Asset: PromotionFundAssetCash,
				Currency: "CNY", Amount: -pendingLedger.NetAmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: pendingLedger.Id,
			},
			{
				Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash,
				Currency: "CNY", Amount: pendingLedger.NetAmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: pendingLedger.Id,
			},
		})
	}))

	require.NoError(t, BackfillPromotionFundTransactions(db))
	require.NoError(t, BackfillPromotionFundTransactions(db))

	var persistedOriginal PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", original.TransactionKey).First(&persistedOriginal).Error)
	assert.Equal(t, "backfill:v1", persistedOriginal.ActorRef)
	require.Len(t, persistedOriginal.Legs, 1)
	assert.Equal(t, int64(600), persistedOriginal.Legs[0].Amount)

	var correction PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", "pfb:promotion_commission_ledgers:41:uncashable_accrual_correction").First(&correction).Error)
	assert.Equal(t, PromotionFundKindReversal, correction.Kind)
	assert.Equal(t, persistedOriginal.Id, correction.ReversesTransactionId)
	require.Len(t, correction.Legs, 1)
	assert.Equal(t, PromotionFundAccountCommissionAvailable, correction.Legs[0].Account)
	assert.Equal(t, int64(-600), correction.Legs[0].Amount)

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("source_type = ? AND source_id = ?", "promotion_commission_ledgers", ledger.Id).Count(&transactionCount).Error)
	assert.Equal(t, int64(2), transactionCount)
	var availableBalance int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("source_type = ? AND source_id = ? AND account = ?", "promotion_commission_ledgers", ledger.Id, PromotionFundAccountCommissionAvailable).
		Select("COALESCE(SUM(amount), 0)").Scan(&availableBalance).Error)
	assert.Zero(t, availableBalance)

	var pendingCorrection PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?", "pfb:promotion_commission_ledgers:42:uncashable_accrual_correction").First(&pendingCorrection).Error)
	assert.Equal(t, settlement.Id, pendingCorrection.ReversesTransactionId)
	require.Len(t, pendingCorrection.Legs, 1)
	assert.Equal(t, PromotionFundAccountCommissionAvailable, pendingCorrection.Legs[0].Account)
	assert.Equal(t, int64(-400), pendingCorrection.Legs[0].Amount)
	var pendingBalance int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("source_type = ? AND source_id = ? AND account = ?", "promotion_commission_ledgers", pendingLedger.Id, PromotionFundAccountCommissionPending).
		Select("COALESCE(SUM(amount), 0)").Scan(&pendingBalance).Error)
	assert.Zero(t, pendingBalance)
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).
		Where("source_type = ? AND source_id = ? AND account = ?", "promotion_commission_ledgers", pendingLedger.Id, PromotionFundAccountCommissionAvailable).
		Select("COALESCE(SUM(amount), 0)").Scan(&availableBalance).Error)
	assert.Zero(t, availableBalance)
}
