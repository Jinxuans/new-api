package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromotionRefundLocksWithdrawalAndLedgersBeforeManualIntakeUsers(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	ensureFinancialActorTestUser(t, 8110, common.RoleRootUser)
	insertInviterAndInviteeForRebateTest(t, 8111, 8112)
	topUp := &TopUp{
		UserId: 8112, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1_000, PaidAmountMinor: 1_000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "refund-lock-order", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: time.Now().Unix(), CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 8111)
	require.NoError(t, DB.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).
		Update("status", PromotionCommissionStatusWithdrawing).Error)
	withdrawal := &PromotionWithdrawal{
		UserId: 8111, Currency: "CNY", GrossAmountCents: ledger.NetAmountCents,
		NetAmountCents: ledger.NetAmountCents, Status: PromotionWithdrawalStatusPendingReview,
		PayoutMethod: "bank",
	}
	require.NoError(t, DB.Create(withdrawal).Error)
	require.NoError(t, DB.Create(&PromotionWithdrawalItem{
		WithdrawalId: withdrawal.Id, LedgerId: ledger.Id, AmountCents: ledger.NetAmountCents,
	}).Error)

	var mu sync.Mutex
	lockedTables := make([]string, 0, 3)
	const callbackName = "test:promotion-refund-lock-order"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, locked := tx.Statement.Clauses["FOR"]; !locked {
			return
		}
		table := tx.Statement.Table
		if tx.Statement.Schema != nil {
			table = tx.Statement.Schema.Table
		}
		mu.Lock()
		lockedTables = append(lockedTables, table)
		mu.Unlock()
		delete(tx.Statement.Clauses, "FOR")
	}))
	previousDatabaseType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	t.Cleanup(func() {
		common.SetMainDatabaseType(previousDatabaseType)
		DB.Callback().Query().Remove(callbackName)
	})

	err = DB.Transaction(func(tx *gorm.DB) error {
		lockedWithdrawal, lockErr := lockPromotionRefundCommissionWithdrawalTx(tx, topUp)
		if lockErr != nil {
			return lockErr
		}
		require.NotNil(t, lockedWithdrawal)
		assert.Equal(t, withdrawal.Id, lockedWithdrawal.Id)
		return lockPromotionRefundIntakeUsersTx(tx, topUp, 8110, common.RoleRootUser)
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	order := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, table := range lockedTables {
		if (table == "promotion_withdrawals" || table == "promotion_commission_ledgers" || table == "users") && !seen[table] {
			seen[table] = true
			order = append(order, table)
		}
	}
	assert.Equal(t, []string{"promotion_withdrawals", "promotion_commission_ledgers", "users"}, order)
}

func TestPromotionRefundRecoveryLocksCommissionBeforeUsers(t *testing.T) {
	truncateTables(t)
	setInvitationRebateFreezeDaysForTest(t, 0)
	setInvitationRebatePercentageForTest(t, 10)
	ensureFinancialActorTestUser(t, 8120, common.RoleAdminUser)
	insertInviterAndInviteeForRebateTest(t, 8121, 8122)
	t.Cleanup(func() {
		_ = DB.Model(&User{}).Where("id IN ?", []int{8121, 8122}).Update("refund_hold", false).Error
		_ = ClearUserRefundHoldFence(8121)
		_ = ClearUserRefundHoldFence(8122)
	})
	topUp := &TopUp{
		UserId: 8122, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1_000, PaidAmountMinor: 1_000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "refund-recovery-lock-order", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: time.Now().Unix(), CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	rebate, err := SettleInvitationRebateTx(DB, topUp)
	require.NoError(t, err)
	require.NotNil(t, rebate)
	ledger := getPromotionCommissionLedgerForTest(t, 8121)
	refundCase := &PromotionRefundCase{
		EventKey: "refund-recovery-lock-order-case", Provider: topUp.PaymentProvider,
		TradeNo: topUp.TradeNo, RefundTradeNo: "refund-recovery-lock-order-ref",
		Kind: PromotionRefundKindFull, TopUpId: topUp.Id, UserId: topUp.UserId,
		InvitationRebateId: rebate.Id, CommissionLedgerId: ledger.Id,
		Status: PromotionRefundCaseStatusPendingReview, RequiresRootReview: true,
	}
	require.NoError(t, DB.Create(refundCase).Error)
	changed, err := ReconcilePromotionRefundCaseResponsibility(refundCase.Id)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, DB.Select("responsibility_fingerprint").First(refundCase, refundCase.Id).Error)
	require.Len(t, refundCase.ResponsibilityFingerprint, 40)

	var mu sync.Mutex
	lockedTables := make([]string, 0, 12)
	const callbackName = "test:promotion-refund-recovery-lock-order"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, locked := tx.Statement.Clauses["FOR"]; !locked {
			return
		}
		table := tx.Statement.Table
		if tx.Statement.Schema != nil {
			table = tx.Statement.Schema.Table
		}
		mu.Lock()
		lockedTables = append(lockedTables, table)
		mu.Unlock()
		delete(tx.Statement.Clauses, "FOR")
	}))
	previousDatabaseType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	t.Cleanup(func() {
		common.SetMainDatabaseType(previousDatabaseType)
		DB.Callback().Query().Remove(callbackName)
	})

	_, err = ApplyPromotionRefundRecoveryAction(PromotionRefundRecoveryActionInput{
		RefundCaseId: refundCase.Id, IdempotencyKey: "recovery-lock-order",
		Action: PromotionRefundActionReleaseHold, ActorId: 8120, ActorRole: common.RoleAdminUser,
		Remark: "verify lock order", ExpectedResponsibilityFingerprint: refundCase.ResponsibilityFingerprint,
	})
	require.ErrorContains(t, err, "requires a root review waiver")

	mu.Lock()
	defer mu.Unlock()
	afterActionLock := false
	seen := map[string]bool{}
	order := make([]string, 0, 2)
	for _, table := range lockedTables {
		if table == "promotion_refund_actions" {
			afterActionLock = true
			continue
		}
		if !afterActionLock || (table != "promotion_commission_ledgers" && table != "users") || seen[table] {
			continue
		}
		seen[table] = true
		order = append(order, table)
	}
	assert.Equal(t, []string{"promotion_commission_ledgers", "users"}, order)
}
