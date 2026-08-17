package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createBalanceSubscriptionPurchaseFixture(t *testing.T, username string) (*User, *SubscriptionPlan) {
	t.Helper()
	user := &User{
		Username: username,
		AffCode:  username,
		Quota:    1_000,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title: "Balance plan", PriceAmount: 3.2, Currency: "USD", Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 1_000_000, UpgradeGroup: "pro",
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	return user, plan
}

func TestPurchaseSubscriptionWithBalanceRecordsAtomicFundDebit(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	user, plan := createBalanceSubscriptionPurchaseFixture(t, "subscription-balance-fund")

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	storedUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 968, storedUser.Quota)
	assert.Equal(t, "pro", storedUser.Group)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&order).Error)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	assert.Equal(t, PaymentProviderBalance, order.PaymentProvider)
	assert.Equal(t, "charged_quota=32", order.ProviderPayload)
	require.NotNil(t, order.UserSubscriptionId)

	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, *order.UserSubscriptionId).Error)
	assert.Equal(t, user.Id, subscription.UserId)
	assert.Equal(t, plan.Id, subscription.PlanId)

	var transaction PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").Where("transaction_key = ?",
		fmt.Sprintf("subscription_order:%d:balance_debited", order.Id)).First(&transaction).Error)
	assert.Equal(t, PromotionFundKindSubscriptionBalanceDebited, transaction.Kind)
	assert.Equal(t, PromotionFundSourceSubscriptionOrders, transaction.SourceType)
	assert.Equal(t, order.Id, transaction.SourceId)
	assert.Equal(t, order.TradeNo, transaction.SourceKey)
	assert.Equal(t, "user", transaction.ActorType)
	assert.Equal(t, user.Id, transaction.ActorId)
	require.Len(t, transaction.Legs, 1)
	leg := transaction.Legs[0]
	assert.Equal(t, PromotionFundAccountAPIBalance, leg.Account)
	assert.Equal(t, PromotionFundAssetQuota, leg.Asset)
	assert.Equal(t, int64(-32), leg.Amount)
	assert.Equal(t, PromotionFundSourceSubscriptionOrders, leg.SourceType)
	assert.Equal(t, order.Id, leg.SourceId)
	require.NotNil(t, leg.BalanceAfter)
	assert.Equal(t, int64(968), *leg.BalanceAfter)
}

func TestPurchaseSubscriptionWithBalanceRollsBackWhenFundLegWriteFails(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	user, plan := createBalanceSubscriptionPurchaseFixture(t, "subscription-balance-rollback")

	require.NoError(t, DB.Migrator().DropTable(&PromotionFundTransactionLeg{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&PromotionFundTransactionLeg{}))
	})

	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	storedUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 1_000, storedUser.Quota)
	assert.Equal(t, "default", storedUser.Group)
	var orderCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("user_id = ?", user.Id).Count(&orderCount).Error)
	assert.Zero(t, orderCount)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	assert.Zero(t, subscriptionCount)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("user_id = ?", user.Id).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestCompleteSubscriptionBalanceOrderReplayDoesNotChargeTwice(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	user, plan := createBalanceSubscriptionPurchaseFixture(t, "subscription-balance-replay")
	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	var order SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&order).Error)
	common.QuotaPerUnit = 999
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		lockedUser, err := lockActiveUserForFinancialWriteTx(tx, user.Id)
		if err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("id = ?", order.Id).First(&order).Error; err != nil {
			return err
		}
		purchase, err := completeSubscriptionBalanceOrderTx(tx, &order, plan, lockedUser)
		if err != nil {
			return err
		}
		assert.True(t, purchase.Replayed)
		assert.Equal(t, 32, purchase.ChargedQuota)
		return nil
	}))

	storedUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 968, storedUser.Quota)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("source_type = ? AND source_id = ?", PromotionFundSourceSubscriptionOrders, order.Id).
		Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	assert.Equal(t, int64(1), subscriptionCount)
}

func TestAdminBindSubscriptionDoesNotCreateFundTransaction(t *testing.T) {
	truncateTables(t)
	user, plan := createBalanceSubscriptionPurchaseFixture(t, "subscription-admin-grant")
	actor := createSubscriptionAdminTestActor(t, "subscription-admin-grant-actor", common.RoleRootUser)

	_, _, err := GrantUserSubscriptionByAdmin(AdminSubscriptionOperationInput{
		UserId: user.Id, PlanId: plan.Id, ActorId: actor.Id, ActorRole: common.RoleRootUser,
		Reason: "manual grant", IdempotencyKey: "subscription-admin-grant-no-funds",
	})
	require.NoError(t, err)

	storedUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 1_000, storedUser.Quota)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Where("user_id = ?", user.Id).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestBackfillSubscriptionBalanceFundTransactionUsesPersistedChargedQuota(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	order := &SubscriptionOrder{
		UserId: 91, PlanId: 10, Money: 99,
		TradeNo: "historical-subscription-balance", PaymentMethod: PaymentMethodBalance,
		PaymentProvider: PaymentProviderBalance, Status: common.TopUpStatusSuccess,
		CreateTime: 100, CompleteTime: 120, ProviderPayload: "charged_quota=37",
	}
	require.NoError(t, db.Create(order).Error)

	require.NoError(t, BackfillPromotionFundTransactions(db))

	var transaction PromotionFundTransaction
	require.NoError(t, db.Preload("Legs").Where("transaction_key = ?",
		fmt.Sprintf("pfb:subscription_orders:%d:balance_debited", order.Id)).First(&transaction).Error)
	assert.Equal(t, PromotionFundKindSubscriptionBalanceDebited, transaction.Kind)
	assert.Equal(t, PromotionFundSourceSubscriptionOrders, transaction.SourceType)
	assert.Equal(t, order.Id, transaction.SourceId)
	assert.Equal(t, "user", transaction.ActorType)
	assert.Equal(t, order.UserId, transaction.ActorId)
	assert.Equal(t, int64(120), transaction.OccurredAt)
	require.Len(t, transaction.Legs, 1)
	assert.Equal(t, int64(-37), transaction.Legs[0].Amount)
	assert.Nil(t, transaction.Legs[0].BalanceAfter)
}

func TestBackfillSubscriptionBalanceFundTransactionSkipsUnverifiableEvidence(t *testing.T) {
	db := openPromotionFundBackfillTestDB(t)
	payloads := []string{
		"",
		"quota=10",
		"charged_quota=0",
		"charged_quota=-1",
		"charged_quota=1.5",
		"charged_quota=2147483647",
		"charged_quota=2147483648",
		"charged_quota=10&other=1",
	}
	for i, payload := range payloads {
		require.NoError(t, db.Create(&SubscriptionOrder{
			UserId: 100 + i, PlanId: 10, TradeNo: fmt.Sprintf("invalid-subscription-balance-%d", i),
			PaymentMethod: PaymentMethodBalance, PaymentProvider: PaymentProviderBalance,
			Status: common.TopUpStatusSuccess, CreateTime: 100, CompleteTime: 120,
			ProviderPayload: payload,
		}).Error)
	}
	require.NoError(t, db.Create(&SubscriptionOrder{
		UserId: 200, PlanId: 10, TradeNo: "external-subscription-order",
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess, ProviderPayload: "charged_quota=10",
	}).Error)
	require.NoError(t, db.Create(&SubscriptionOrder{
		UserId: 201, PlanId: 10, TradeNo: "pending-balance-subscription-order",
		PaymentMethod: PaymentMethodBalance, PaymentProvider: PaymentProviderBalance,
		Status: common.TopUpStatusPending, ProviderPayload: "charged_quota=10",
	}).Error)

	require.NoError(t, BackfillPromotionFundTransactions(db))

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).
		Where("source_type = ?", PromotionFundSourceSubscriptionOrders).
		Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestPurchaseSubscriptionWithBalanceRejectsRefundRecoveryRestrictions(t *testing.T) {
	tests := []struct {
		name        string
		restriction string
	}{
		{name: "durable refund hold", restriction: "refund_hold"},
		{name: "open refund obligation", restriction: "open_obligation"},
		{name: "in-flight refund fence", restriction: "refund_fence"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			user := &User{
				Username: "subscription-balance-" + test.restriction,
				AffCode:  "subbalance" + test.restriction,
				Quota:    1_000_000,
				Status:   common.UserStatusEnabled,
				Group:    "default",
			}
			require.NoError(t, DB.Create(user).Error)
			plan := &SubscriptionPlan{
				Title: "Balance plan", PriceAmount: 1, Currency: "USD", Enabled: true,
				DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
				TotalAmount: 1_000_000, UpgradeGroup: "pro",
			}
			require.NoError(t, DB.Create(plan).Error)
			InvalidateSubscriptionPlanCache(plan.Id)

			switch test.restriction {
			case "refund_hold":
				require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", true).Error)
			case "open_obligation":
				refundCase := &PromotionRefundCase{
					EventKey: "subscription-balance-open-obligation", Provider: "test",
					TradeNo: "subscription-balance-order", RefundTradeNo: "subscription-balance-refund",
					Kind: PromotionRefundKindFull, Status: PromotionRefundCaseStatusPendingReview,
				}
				require.NoError(t, DB.Create(refundCase).Error)
				require.NoError(t, DB.Create(&PromotionRefundObligation{
					ObligationKey: fmt.Sprintf("subscription-balance:%d", user.Id),
					RefundCaseId:  refundCase.Id, UserId: user.Id,
					Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota,
					Amount: 1, Status: PromotionRefundObligationStatusOpen,
				}).Error)
			case "refund_fence":
				require.NoError(t, SetUserRefundHoldFence(user.Id))
				t.Cleanup(func() {
					require.NoError(t, ClearUserRefundHoldFence(user.Id))
				})
			}

			err := PurchaseSubscriptionWithBalance(user.Id, plan.Id)
			require.ErrorIs(t, err, ErrUserRefundHeld)

			storedUser, err := GetUserById(user.Id, true)
			require.NoError(t, err)
			assert.Equal(t, 1_000_000, storedUser.Quota)
			assert.Equal(t, "default", storedUser.Group)

			subscriptions, err := GetAllUserSubscriptions(user.Id)
			require.NoError(t, err)
			assert.Empty(t, subscriptions)
		})
	}
}
