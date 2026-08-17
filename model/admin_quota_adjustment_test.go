package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createAdminQuotaAdjustmentUser(t *testing.T, quota int) *User {
	t.Helper()
	user := &User{
		Username: fmt.Sprintf("admin-quota-user-%s", common.GetRandomString(8)),
		AffCode:  fmt.Sprintf("admin-quota-user-aff-%s", common.GetRandomString(8)),
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func createAdminQuotaAdjustmentActor(t *testing.T) *User {
	t.Helper()
	actor := &User{
		Username: fmt.Sprintf("admin-quota-actor-%s", common.GetRandomString(8)),
		AffCode:  fmt.Sprintf("admin-quota-actor-aff-%s", common.GetRandomString(8)),
		Status:   common.UserStatusEnabled,
		Role:     common.RoleAdminUser,
	}
	require.NoError(t, DB.Create(actor).Error)
	return actor
}

func adminQuotaAdjustmentInput(userId int, actorId int, key string) AdminQuotaAdjustmentInput {
	return AdminQuotaAdjustmentInput{
		UserId: userId, Mode: AdminQuotaAdjustmentModeAdd, Value: 25,
		ActorId: actorId, ActorRef: "operator@example.invalid",
		Remark: "verified support correction", IdempotencyKey: key,
	}
}

func TestAdjustUserQuotaByAdminReplaysSameRequestWithoutSecondMutation(t *testing.T) {
	truncateTables(t)
	actor := createAdminQuotaAdjustmentActor(t)
	user := createAdminQuotaAdjustmentUser(t, 100)
	input := adminQuotaAdjustmentInput(user.Id, actor.Id, "admin-quota-replay")

	first, err := AdjustUserQuotaByAdmin(input)
	require.NoError(t, err)
	assert.False(t, first.Replayed)
	assert.Equal(t, 100, first.PreviousQuota)
	assert.Equal(t, 125, first.CurrentQuota)

	replayed, err := AdjustUserQuotaByAdmin(input)
	require.NoError(t, err)
	assert.True(t, replayed.Replayed)
	assert.Equal(t, first.FundTransactionId, replayed.FundTransactionId)
	assert.Equal(t, first.PreviousQuota, replayed.PreviousQuota)
	assert.Equal(t, first.CurrentQuota, replayed.CurrentQuota)

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 125, user.Quota)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", "admin_quota:"+input.IdempotencyKey).
		Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)

	conflicting := input
	conflicting.Remark = "a different correction"
	_, err = AdjustUserQuotaByAdmin(conflicting)
	require.ErrorIs(t, err, ErrPromotionFundTransactionConflict)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 125, user.Quota)
}

func TestAdjustUserQuotaByAdminValidatesReasonAmountAndWalletRange(t *testing.T) {
	truncateTables(t)
	actor := createAdminQuotaAdjustmentActor(t)
	user := createAdminQuotaAdjustmentUser(t, 100)

	testCases := []struct {
		name    string
		mutate  func(*AdminQuotaAdjustmentInput)
		wantErr error
	}{
		{
			name: "reason required",
			mutate: func(input *AdminQuotaAdjustmentInput) {
				input.Remark = "  "
			},
			wantErr: ErrAdminQuotaAdjustmentReasonRequired,
		},
		{
			name: "idempotency key required",
			mutate: func(input *AdminQuotaAdjustmentInput) {
				input.IdempotencyKey = "  "
			},
			wantErr: ErrAdminQuotaAdjustmentInvalid,
		},
		{
			name: "zero add rejected",
			mutate: func(input *AdminQuotaAdjustmentInput) {
				input.Value = 0
			},
			wantErr: ErrAdminQuotaAdjustmentInvalid,
		},
		{
			name: "unchanged override rejected",
			mutate: func(input *AdminQuotaAdjustmentInput) {
				input.Mode = AdminQuotaAdjustmentModeOverride
				input.Value = 100
			},
			wantErr: ErrAdminQuotaAdjustmentNoChange,
		},
		{
			name: "wallet overflow rejected",
			mutate: func(input *AdminQuotaAdjustmentInput) {
				input.Value = common.MaxQuota - 100
			},
			wantErr: ErrAdminQuotaAdjustmentOutOfRange,
		},
		{
			name: "overlong reason rejected",
			mutate: func(input *AdminQuotaAdjustmentInput) {
				input.Remark = strings.Repeat("界", adminQuotaAdjustmentRemarkMaxRunes+1)
			},
			wantErr: ErrAdminQuotaAdjustmentInvalid,
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := adminQuotaAdjustmentInput(user.Id, actor.Id, fmt.Sprintf("admin-quota-invalid-%d", index))
			testCase.mutate(&input)
			_, err := AdjustUserQuotaByAdmin(input)
			require.ErrorIs(t, err, testCase.wantErr)
		})
	}

	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestAdjustUserQuotaByAdminBlocksOutflowDuringRefundRecovery(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*testing.T, *User)
	}{
		{
			name: "refund hold",
			setup: func(t *testing.T, user *User) {
				require.NoError(t, DB.Model(user).Update("refund_hold", true).Error)
			},
		},
		{
			name: "open recovery obligation",
			setup: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionRefundObligation{
					ObligationKey: "admin-quota-open-obligation", RefundCaseId: 1,
					UserId: user.Id, Account: PromotionFundAccountRefundDebt,
					Asset: PromotionFundAssetQuota, Amount: 10,
					Status: PromotionRefundObligationStatusOpen,
				}).Error)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			actor := createAdminQuotaAdjustmentActor(t)
			user := createAdminQuotaAdjustmentUser(t, 100)
			testCase.setup(t, user)
			input := adminQuotaAdjustmentInput(user.Id, actor.Id, "admin-quota-blocked-"+testCase.name)
			input.Mode = AdminQuotaAdjustmentModeSubtract
			input.Value = 10

			_, err := AdjustUserQuotaByAdmin(input)
			require.ErrorIs(t, err, ErrUserRefundHeld)
			require.NoError(t, DB.First(user, user.Id).Error)
			assert.Equal(t, 100, user.Quota)
			var transactionCount int64
			require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
			assert.Zero(t, transactionCount)
		})
	}
}

func TestAdjustUserQuotaByAdminCreatesImmutableFundEvidence(t *testing.T) {
	truncateTables(t)
	actor := createAdminQuotaAdjustmentActor(t)
	user := createAdminQuotaAdjustmentUser(t, 100)
	input := adminQuotaAdjustmentInput(user.Id, actor.Id, "admin-quota-evidence")

	result, err := AdjustUserQuotaByAdmin(input)
	require.NoError(t, err)
	assert.False(t, result.Replayed)

	var transaction PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").First(&transaction, result.FundTransactionId).Error)
	assert.Equal(t, PromotionFundKindAdminQuotaCredited, transaction.Kind)
	assert.Equal(t, user.Id, transaction.UserId)
	assert.Equal(t, "admin", transaction.ActorType)
	assert.Equal(t, input.ActorId, transaction.ActorId)
	assert.Equal(t, input.ActorRef, transaction.ActorRef)
	assert.Equal(t, input.Remark, transaction.Remark)
	require.Len(t, transaction.Legs, 1)
	assert.Equal(t, int64(25), transaction.Legs[0].Amount)
	require.NotNil(t, transaction.Legs[0].BalanceAfter)
	assert.Equal(t, int64(125), *transaction.Legs[0].BalanceAfter)

	err = DB.Model(&transaction).Update("remark", "tampered").Error
	require.ErrorIs(t, err, ErrPromotionFundTransactionImmutable)
}

func TestAdjustUserQuotaByAdminRollsBackBalanceWhenFundWriteFails(t *testing.T) {
	truncateTables(t)
	actor := createAdminQuotaAdjustmentActor(t)
	user := createAdminQuotaAdjustmentUser(t, 100)
	const triggerName = "test_fail_admin_quota_fund_write"
	require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS "+triggerName).Error)
	require.NoError(t, DB.Exec("CREATE TRIGGER "+triggerName+" BEFORE INSERT ON promotion_fund_transactions "+
		"BEGIN SELECT RAISE(ABORT, 'forced admin quota fund failure'); END").Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS "+triggerName).Error)
	})

	_, err := AdjustUserQuotaByAdmin(adminQuotaAdjustmentInput(user.Id, actor.Id, "admin-quota-rollback"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forced admin quota fund failure")
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}
