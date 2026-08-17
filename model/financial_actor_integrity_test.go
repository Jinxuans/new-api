package model

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createFinancialActorTestUser(t *testing.T, role int) *User {
	t.Helper()
	user := &User{
		Username: fmt.Sprintf("financial-actor-%s", common.GetRandomString(8)),
		AffCode:  fmt.Sprintf("financial-actor-aff-%s", common.GetRandomString(8)),
		Role:     role,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func ensureFinancialActorTestUser(t *testing.T, userId int, role int) {
	t.Helper()
	var existing User
	err := DB.Unscoped().Where("id = ?", userId).First(&existing).Error
	if err == nil {
		require.False(t, existing.DeletedAt.Valid)
		if existing.Role != role {
			require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("role", role).Error)
		}
		return
	}
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, DB.Create(&User{
		Id:       userId,
		Username: fmt.Sprintf("financial-test-actor-%d-%s", userId, common.GetRandomString(8)),
		AffCode:  fmt.Sprintf("financial-test-actor-aff-%d-%s", userId, common.GetRandomString(8)),
		Role:     role,
		Status:   common.UserStatusEnabled,
	}).Error)
}

func TestLockActiveUsersForFinancialWriteTxDeduplicatesAndUsesAscendingOrder(t *testing.T) {
	truncateTables(t)
	users := []*User{
		createFinancialActorTestUser(t, common.RoleCommonUser),
		createFinancialActorTestUser(t, common.RoleAdminUser),
		createFinancialActorTestUser(t, common.RoleRootUser),
	}
	require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS test_financial_user_lock_order").Error)
	require.NoError(t, DB.Exec("DROP TABLE IF EXISTS test_financial_user_lock_events").Error)
	require.NoError(t, DB.Exec("CREATE TABLE test_financial_user_lock_events (sequence_id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL)").Error)
	require.NoError(t, DB.Exec(`CREATE TRIGGER test_financial_user_lock_order AFTER UPDATE OF id ON users
		BEGIN INSERT INTO test_financial_user_lock_events(user_id) VALUES (NEW.id); END`).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS test_financial_user_lock_order").Error)
		require.NoError(t, DB.Exec("DROP TABLE IF EXISTS test_financial_user_lock_events").Error)
	})

	err := DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockActiveUsersForFinancialWriteTx(tx, users[2].Id, users[0].Id, users[1].Id, users[2].Id)
		require.NoError(t, err)
		assert.Len(t, locked, 3)
		return nil
	})
	require.NoError(t, err)

	var lockedOrder []int
	require.NoError(t, DB.Table("test_financial_user_lock_events").Order("sequence_id ASC").Pluck("user_id", &lockedOrder).Error)
	expected := []int{users[0].Id, users[1].Id, users[2].Id}
	sort.Ints(expected)
	assert.Equal(t, expected, lockedOrder)
}

func TestLockActiveUsersForFinancialWriteTxRejectsMissingAndDeletedUsersAtomically(t *testing.T) {
	testCases := []struct {
		name       string
		missing    bool
		softDelete bool
	}{
		{name: "missing", missing: true},
		{name: "soft deleted", softDelete: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			active := createFinancialActorTestUser(t, common.RoleAdminUser)
			other := createFinancialActorTestUser(t, common.RoleCommonUser)
			otherId := other.Id
			if testCase.missing {
				require.NoError(t, DB.Unscoped().Delete(other).Error)
			} else if testCase.softDelete {
				require.NoError(t, DB.Delete(other).Error)
			}

			err := DB.Transaction(func(tx *gorm.DB) error {
				if _, err := lockActiveUsersForFinancialWriteTx(tx, otherId, active.Id); err != nil {
					return err
				}
				return tx.Model(&User{}).Where("id = ?", active.Id).Update("quota", 99).Error
			})
			require.Error(t, err)
			var reloaded User
			require.NoError(t, DB.First(&reloaded, active.Id).Error)
			assert.Zero(t, reloaded.Quota)
		})
	}
}

func TestHardDeletePreservesRefundCaseInitiatorEvidence(t *testing.T) {
	truncateTables(t)
	actor := createFinancialActorTestUser(t, common.RoleRootUser)
	require.NoError(t, DB.Create(&PromotionRefundCase{
		EventKey:      "refund-initiator-retention",
		Status:        PromotionRefundCaseStatusResolved,
		InitiatorType: "admin",
		InitiatorId:   actor.Id,
		InitiatorRole: common.RoleRootUser,
	}).Error)

	err := actor.HardDelete()
	require.ErrorIs(t, err, ErrUserFinancialHistory)
	var retained User
	require.NoError(t, DB.Unscoped().First(&retained, actor.Id).Error)
	assert.False(t, retained.DeletedAt.Valid)
}

func createManualRefundActorFixture(t *testing.T) (*User, *User, *TopUp, AdminPromotionRefundCaseInput) {
	t.Helper()
	root := createFinancialActorTestUser(t, common.RoleRootUser)
	target := createFinancialActorTestUser(t, common.RoleCommonUser)
	topUp := &TopUp{
		UserId: target.Id, Purpose: TopUpPurposeAPIBalance,
		TradeNo:       fmt.Sprintf("manual-actor-order-%s", common.GetRandomString(8)),
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())
	input := AdminPromotionRefundCaseInput{
		IdempotencyKey:      "manual-actor-idempotency",
		TradeNo:             topUp.TradeNo,
		ExternalReference:   "manual-actor-refund",
		IntakeSource:        PromotionRefundIntakeOfflineRefund,
		Kind:                PromotionRefundKindFull,
		RefundedAmountMinor: 100,
		Currency:            "CNY",
		Remark:              "verified offline refund",
		ActorId:             root.Id,
		ActorRole:           common.RoleRootUser,
	}
	return root, target, topUp, input
}

func TestManualRefundCaseIdempotencyIncludesInitiatorIdentityAndRole(t *testing.T) {
	t.Run("same actor and role replays", func(t *testing.T) {
		truncateTables(t)
		_, target, _, input := createManualRefundActorFixture(t)
		first, err := CreateAdminPromotionRefundCase(input)
		require.NoError(t, err)
		replayed, err := CreateAdminPromotionRefundCase(input)
		require.NoError(t, err)
		assert.Equal(t, first.Id, replayed.Id)
		assert.Equal(t, input.ActorRole, first.InitiatorRole)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", target.Id).Update("refund_hold", false).Error)
	})

	t.Run("different actor conflicts", func(t *testing.T) {
		truncateTables(t)
		_, target, _, input := createManualRefundActorFixture(t)
		_, err := CreateAdminPromotionRefundCase(input)
		require.NoError(t, err)
		otherRoot := createFinancialActorTestUser(t, common.RoleRootUser)
		input.ActorId = otherRoot.Id
		_, err = CreateAdminPromotionRefundCase(input)
		require.ErrorIs(t, err, ErrPromotionRefundEventConflict)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", target.Id).Update("refund_hold", false).Error)
	})

	t.Run("different role and legacy zero role conflict", func(t *testing.T) {
		truncateTables(t)
		_, target, topUp, input := createManualRefundActorFixture(t)
		refundCase, err := CreateAdminPromotionRefundCase(input)
		require.NoError(t, err)
		replayInput := PromotionRefundInput{
			Provider: topUp.PaymentProvider, TradeNo: topUp.TradeNo,
			RefundTradeNo: input.ExternalReference, Kind: input.Kind,
			RefundedAmountMinor: input.RefundedAmountMinor, Currency: input.Currency,
			Remark: input.Remark, adminIdempotencyKey: input.IdempotencyKey,
			intakeSource: input.IntakeSource, initiatorId: input.ActorId,
			initiatorRole: common.RoleAdminUser,
		}
		_, err = HandlePromotionRefund(replayInput)
		require.ErrorIs(t, err, ErrPromotionRefundEventConflict)

		require.NoError(t, DB.Model(&PromotionRefundCase{}).Where("id = ?", refundCase.Id).
			Update("initiator_role", 0).Error)
		replayInput.initiatorRole = common.RoleRootUser
		_, err = HandlePromotionRefund(replayInput)
		require.ErrorIs(t, err, ErrPromotionRefundEventConflict)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", target.Id).Update("refund_hold", false).Error)
	})
}

func createRefundActionActorFixture(t *testing.T) (*User, *User, *PromotionRefundCase, *PromotionRefundObligation) {
	t.Helper()
	actor := createFinancialActorTestUser(t, common.RoleRootUser)
	target := createFinancialActorTestUser(t, common.RoleCommonUser)
	require.NoError(t, DB.Model(target).Updates(map[string]interface{}{
		"refund_hold": true, "refund_debt_quota": int64(10),
	}).Error)
	refundCase := &PromotionRefundCase{
		EventKey: fmt.Sprintf("action-role-case-%s", common.GetRandomString(8)),
		UserId:   target.Id, Status: PromotionRefundCaseStatusPendingReview,
	}
	require.NoError(t, DB.Create(refundCase).Error)
	obligation := &PromotionRefundObligation{
		ObligationKey: fmt.Sprintf("action-role-obligation-%s", common.GetRandomString(8)),
		RefundCaseId:  refundCase.Id, UserId: target.Id,
		Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota,
		Amount: 10, SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
	}
	require.NoError(t, DB.Create(obligation).Error)
	return actor, target, refundCase, obligation
}

func TestPromotionRefundActionPersistsRoleAndRejectsDifferentOrLegacyRoleReplay(t *testing.T) {
	testCases := []struct {
		name       string
		legacyRole bool
		replayRole int
	}{
		{name: "different role", replayRole: common.RoleAdminUser},
		{name: "legacy zero role", legacyRole: true, replayRole: common.RoleRootUser},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			actor, _, refundCase, obligation := createRefundActionActorFixture(t)
			input := PromotionRefundRecoveryActionInput{
				RefundCaseId: refundCase.Id, IdempotencyKey: "action-role-idempotency",
				Action:       PromotionRefundActionRecordExternalRepayment,
				ObligationId: obligation.Id, Amount: 5,
				ActorId: actor.Id, ActorRole: common.RoleRootUser,
				ExternalRef: "repayment-role-evidence", Remark: "verified repayment",
			}
			result, err := ApplyPromotionRefundRecoveryAction(input)
			require.NoError(t, err)
			require.Len(t, result.Actions, 1)
			assert.Equal(t, common.RoleRootUser, result.Actions[0].ActorRole)

			replayed, err := ApplyPromotionRefundRecoveryAction(input)
			require.NoError(t, err)
			require.Len(t, replayed.Actions, 1)
			if testCase.legacyRole {
				require.NoError(t, DB.Exec("UPDATE promotion_refund_actions SET actor_role = 0 WHERE id = ?", result.Actions[0].Id).Error)
			}
			input.ActorRole = testCase.replayRole
			_, err = ApplyPromotionRefundRecoveryAction(input)
			require.ErrorContains(t, err, "idempotency key was already used")
		})
	}
}

func TestFinancialActorWritersRejectMissingOrDeletedActorWithoutMutation(t *testing.T) {
	t.Run("administrator quota adjustment", func(t *testing.T) {
		truncateTables(t)
		target := createFinancialActorTestUser(t, common.RoleCommonUser)
		target.Quota = 100
		require.NoError(t, DB.Model(target).Update("quota", 100).Error)
		_, err := AdjustUserQuotaByAdmin(AdminQuotaAdjustmentInput{
			UserId: target.Id, Mode: AdminQuotaAdjustmentModeAdd, Value: 10,
			ActorId: target.Id + 100_000, ActorRef: "missing-admin",
			Remark: "verified correction", IdempotencyKey: "missing-admin-adjustment",
		})
		require.Error(t, err)
		require.NoError(t, DB.First(target, target.Id).Error)
		assert.Equal(t, 100, target.Quota)
		var count int64
		require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("manual top-up", func(t *testing.T) {
		truncateTables(t)
		actor := createFinancialActorTestUser(t, common.RoleRootUser)
		target := createFinancialActorTestUser(t, common.RoleCommonUser)
		require.NoError(t, DB.Delete(actor).Error)
		topUp := &TopUp{
			UserId: target.Id, Purpose: TopUpPurposeAPIBalance, Amount: 1, Money: 1,
			TradeNo: "deleted-actor-manual-topup", PaymentMethod: PaymentProviderEpay,
			PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending,
		}
		require.NoError(t, topUp.Insert())
		err := ManualCompleteTopUp(ManualTopUpCompletionInput{
			TradeNo: topUp.TradeNo, ActorId: actor.Id, ActorRef: "deleted-root",
			Reason: "verified missing callback",
		})
		require.Error(t, err)
		require.NoError(t, DB.First(topUp, topUp.Id).Error)
		assert.Equal(t, common.TopUpStatusPending, topUp.Status)
		require.NoError(t, DB.First(target, target.Id).Error)
		assert.Zero(t, target.Quota)
	})

	t.Run("withdrawal operation", func(t *testing.T) {
		truncateTables(t)
		target := createFinancialActorTestUser(t, common.RoleCommonUser)
		withdrawal := &PromotionWithdrawal{
			UserId: target.Id, Currency: "CNY", Status: PromotionWithdrawalStatusPendingReview,
		}
		require.NoError(t, DB.Create(withdrawal).Error)
		err := DB.Transaction(func(tx *gorm.DB) error {
			return CreatePromotionWithdrawalOperationTx(tx, &PromotionWithdrawalOperation{
				WithdrawalId: withdrawal.Id, Action: PromotionWithdrawalActionApproved,
				ActorType: PromotionWithdrawalActorAdmin, ActorId: target.Id + 100_000,
			})
		})
		require.Error(t, err)
		var count int64
		require.NoError(t, DB.Model(&PromotionWithdrawalOperation{}).Count(&count).Error)
		assert.Zero(t, count)
	})
}

func TestFinancialActorLockPreventsConcurrentHardDeleteFromLeavingOrphanEvidence(t *testing.T) {
	truncateTables(t)
	actor := createFinancialActorTestUser(t, common.RoleAdminUser)
	target := createFinancialActorTestUser(t, common.RoleCommonUser)

	tx := DB.Begin()
	require.NoError(t, tx.Error)
	_, err := lockActiveUsersForFinancialWriteTx(tx, actor.Id, target.Id)
	require.NoError(t, err)

	deleteStarted := make(chan struct{})
	deleteDone := make(chan error, 1)
	go func() {
		close(deleteStarted)
		deleteDone <- actor.HardDelete()
	}()
	<-deleteStarted

	balanceAfter := int64(1)
	require.NoError(t, CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
		TransactionKey: "concurrent-actor-retention", Kind: PromotionFundKindAdminQuotaCredited,
		UserId: target.Id, SourceType: "admin_quota_adjustments",
		ActorType: "admin", ActorId: actor.Id,
	}, []PromotionFundTransactionLeg{{
		Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
		Amount: 1, SourceType: "admin_quota_adjustments", BalanceAfter: &balanceAfter,
	}}))
	require.NoError(t, tx.Commit().Error)

	deleteErr := <-deleteDone
	require.Error(t, deleteErr)
	var retained User
	require.NoError(t, DB.Unscoped().First(&retained, actor.Id).Error)
	assert.False(t, retained.DeletedAt.Valid)
	var evidenceCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("actor_id = ?", actor.Id).Count(&evidenceCount).Error)
	assert.Equal(t, int64(1), evidenceCount)

	if !errors.Is(deleteErr, ErrUserFinancialHistory) {
		// SQLite can fail the concurrent delete while upgrading its snapshot to
		// a writer; a retry must then observe the committed actor evidence.
		require.ErrorIs(t, actor.HardDelete(), ErrUserFinancialHistory)
	}
}
