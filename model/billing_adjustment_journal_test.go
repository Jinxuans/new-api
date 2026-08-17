package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createBillingAdjustmentTestUser(t *testing.T, id int, quota int, refundHold bool) *User {
	t.Helper()
	user := &User{
		Id:         id,
		Username:   fmt.Sprintf("billing-adjustment-user-%d", id),
		Password:   "billing-adjustment-test",
		Status:     common.UserStatusEnabled,
		Quota:      quota,
		RefundHold: refundHold,
		AffCode:    fmt.Sprintf("ba-%d", id),
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func createBillingAdjustmentTestToken(t *testing.T, id int, userId int, remainQuota int, usedQuota int) *Token {
	t.Helper()
	token := &Token{
		Id:          id,
		UserId:      userId,
		Key:         fmt.Sprintf("billing-adjustment-token-%d", id),
		Name:        fmt.Sprintf("billing adjustment token %d", id),
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   usedQuota,
	}
	require.NoError(t, DB.Create(token).Error)
	return token
}

func createBillingAdjustmentTestRow(t *testing.T, input BillingAdjustmentInput) *BillingAdjustmentJournal {
	t.Helper()
	row, err := CreateBillingAdjustment(input)
	require.NoError(t, err)
	require.NotNil(t, row)
	return row
}

func TestWalletBillingAdjustmentRejectsPersistentQuotaOverflow(t *testing.T) {
	tests := []struct {
		name  string
		quota int
		delta int64
		kind  string
	}{
		{name: "credit overflow", quota: common.MaxQuota, delta: -1, kind: BillingAdjustmentKindRefund},
		{name: "authorized debit underflow", quota: common.MinQuota, delta: 1, kind: BillingAdjustmentKindSettle},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7100 + index
			createBillingAdjustmentTestUser(t, userId, test.quota, false)
			row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
				OperationKey:    fmt.Sprintf("wallet-range-%d", index),
				RequestId:       fmt.Sprintf("wallet-range-request-%d", index),
				Kind:            test.kind,
				FundingSource:   billingAdjustmentSourceWallet,
				UserId:          userId,
				FundingDelta:    test.delta,
				FundingRequired: true,
			})

			err := ApplyWalletBillingAdjustment(row.OperationKey)
			require.ErrorIs(t, err, ErrBillingAdjustmentQuotaOutOfRange)

			var user User
			require.NoError(t, DB.First(&user, userId).Error)
			assert.Equal(t, test.quota, user.Quota)
			stored, err := GetBillingAdjustment(row.OperationKey)
			require.NoError(t, err)
			assert.False(t, stored.FundingApplied)
			assert.Equal(t, BillingAdjustmentStatusPending, stored.Status)
		})
	}
}

func TestPositiveWalletReservationsEnforceBalanceAndPreDispatchRefundHold(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		quota      int
		refundHold bool
		wantErr    error
		wantQuota  int
	}{
		{name: "ordinary reserve balance", kind: BillingAdjustmentKindReserve, quota: 9, wantErr: ErrBillingAdjustmentFundingInsufficient, wantQuota: 9},
		{name: "usage reserve balance", kind: BillingAdjustmentKindUsageReserve, quota: 9, wantErr: ErrBillingAdjustmentFundingInsufficient, wantQuota: 9},
		{name: "ordinary reserve hold", kind: BillingAdjustmentKindReserve, quota: 100, refundHold: true, wantErr: ErrUserRefundHeld, wantQuota: 100},
		{name: "authoritative usage during hold", kind: BillingAdjustmentKindUsageReserve, quota: 100, refundHold: true, wantQuota: 90},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7120 + index
			createBillingAdjustmentTestUser(t, userId, test.quota, test.refundHold)
			row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
				OperationKey:    fmt.Sprintf("wallet-reserve-guard-%d", index),
				RequestId:       fmt.Sprintf("wallet-reserve-guard-request-%d", index),
				Kind:            test.kind,
				FundingSource:   billingAdjustmentSourceWallet,
				UserId:          userId,
				FundingDelta:    10,
				FundingTarget:   10,
				FundingRequired: true,
			})

			err := ApplyWalletBillingAdjustment(row.OperationKey)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
			var user User
			require.NoError(t, DB.First(&user, userId).Error)
			assert.Equal(t, test.wantQuota, user.Quota)
		})
	}
}

func TestTokenBillingAdjustmentRejectsPersistentQuotaOverflow(t *testing.T) {
	tests := []struct {
		name   string
		remain int
		used   int
		delta  int64
		kind   string
	}{
		{name: "remain credit overflow", remain: common.MaxQuota, used: 0, delta: -1, kind: BillingAdjustmentKindRefund},
		{name: "used debit overflow", remain: 10, used: common.MaxQuota, delta: 1, kind: BillingAdjustmentKindSettle},
		{name: "remain debit underflow", remain: common.MinQuota, used: 0, delta: 1, kind: BillingAdjustmentKindSettle},
		{name: "used credit underflow", remain: 10, used: common.MinQuota, delta: -1, kind: BillingAdjustmentKindRefund},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7140 + index
			tokenId := 7140 + index
			createBillingAdjustmentTestUser(t, userId, 0, false)
			createBillingAdjustmentTestToken(t, tokenId, userId, test.remain, test.used)
			row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
				OperationKey:  fmt.Sprintf("token-range-%d", index),
				RequestId:     fmt.Sprintf("token-range-request-%d", index),
				Kind:          test.kind,
				UserId:        userId,
				TokenId:       tokenId,
				TokenDelta:    test.delta,
				TokenRequired: true,
			})

			err := ApplyTokenBillingAdjustment(row.OperationKey)
			require.ErrorIs(t, err, ErrBillingAdjustmentQuotaOutOfRange)
			var token Token
			require.NoError(t, DB.First(&token, tokenId).Error)
			assert.Equal(t, test.remain, token.RemainQuota)
			assert.Equal(t, test.used, token.UsedQuota)
			stored, err := GetBillingAdjustment(row.OperationKey)
			require.NoError(t, err)
			assert.False(t, stored.TokenApplied)
		})
	}
}

func TestPositiveTokenReservationKindsAlwaysEnforceBalance(t *testing.T) {
	tests := []struct {
		name string
		kind string
	}{
		{name: "ordinary reserve", kind: BillingAdjustmentKindReserve},
		{name: "usage reserve", kind: BillingAdjustmentKindUsageReserve},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7160 + index
			tokenId := 7160 + index
			createBillingAdjustmentTestUser(t, userId, 0, false)
			createBillingAdjustmentTestToken(t, tokenId, userId, 9, 0)
			row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
				OperationKey:  fmt.Sprintf("token-reserve-guard-%d", index),
				RequestId:     fmt.Sprintf("token-reserve-guard-request-%d", index),
				Kind:          test.kind,
				UserId:        userId,
				TokenId:       tokenId,
				TokenDelta:    10,
				TokenRequired: true,
				// The kind itself is a durable balance-enforcement invariant.
				TokenEnforceBalance: false,
			})

			err := ApplyTokenBillingAdjustment(row.OperationKey)
			require.ErrorIs(t, err, ErrBillingAdjustmentTokenInsufficient)
			var token Token
			require.NoError(t, DB.First(&token, tokenId).Error)
			assert.Equal(t, 9, token.RemainQuota)
			assert.Zero(t, token.UsedQuota)
		})
	}
}

func TestFinalTokenAdjustmentsCompleteAfterTokenSoftDelete(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		delta int64
	}{
		{name: "settlement", kind: BillingAdjustmentKindSettle, delta: 5},
		{name: "refund", kind: BillingAdjustmentKindRefund, delta: -5},
		{name: "rollback", kind: BillingAdjustmentKindRollback, delta: -5},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7180 + index
			tokenId := 7180 + index
			createBillingAdjustmentTestUser(t, userId, 0, false)
			token := createBillingAdjustmentTestToken(t, tokenId, userId, 100, 20)
			row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
				OperationKey:  fmt.Sprintf("deleted-token-final-%d", index),
				RequestId:     fmt.Sprintf("deleted-token-final-request-%d", index),
				Kind:          test.kind,
				UserId:        userId,
				TokenId:       tokenId,
				TokenDelta:    test.delta,
				TokenRequired: true,
			})
			require.NoError(t, DB.Delete(token).Error)

			require.NoError(t, ApplyTokenBillingAdjustment(row.OperationKey))
			stored, err := GetBillingAdjustment(row.OperationKey)
			require.NoError(t, err)
			assert.True(t, stored.TokenApplied)
			assert.Equal(t, BillingAdjustmentStatusCompleted, stored.Status)

			var deleted Token
			require.NoError(t, DB.Unscoped().First(&deleted, tokenId).Error)
			assert.Equal(t, 100, deleted.RemainQuota)
			assert.Equal(t, 20, deleted.UsedQuota)
		})
	}
}

func TestDeletedTokenOwnerMismatchFailsClosed(t *testing.T) {
	truncateTables(t)
	createBillingAdjustmentTestUser(t, 7200, 0, false)
	createBillingAdjustmentTestUser(t, 7201, 0, false)
	token := createBillingAdjustmentTestToken(t, 7200, 7200, 100, 20)
	row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey:  "deleted-token-owner-mismatch",
		RequestId:     "deleted-token-owner-mismatch-request",
		Kind:          BillingAdjustmentKindRefund,
		UserId:        7201,
		TokenId:       token.Id,
		TokenDelta:    -5,
		TokenRequired: true,
	})
	require.NoError(t, DB.Delete(token).Error)

	err := ApplyTokenBillingAdjustment(row.OperationKey)
	require.ErrorIs(t, err, ErrBillingAdjustmentConflict)
	stored, loadErr := GetBillingAdjustment(row.OperationKey)
	require.NoError(t, loadErr)
	assert.False(t, stored.TokenApplied)
	assert.Equal(t, BillingAdjustmentStatusPending, stored.Status)
}

func TestDispatchRequiredReserveCanPreauthorizeBeforeConfirmation(t *testing.T) {
	truncateTables(t)
	const userId, tokenId = 7220, 7220
	createBillingAdjustmentTestUser(t, userId, 100, false)
	createBillingAdjustmentTestToken(t, tokenId, userId, 100, 0)
	row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey:        "dispatch-required-reserve",
		RequestId:           "dispatch-required-reserve-request",
		Kind:                BillingAdjustmentKindReserve,
		FundingSource:       billingAdjustmentSourceWallet,
		UserId:              userId,
		TokenId:             tokenId,
		FundingDelta:        10,
		FundingTarget:       10,
		TokenDelta:          10,
		FundingRequired:     true,
		TokenRequired:       true,
		TokenEnforceBalance: true,
		DispatchRequired:    true,
	})
	assert.False(t, row.DispatchConfirmed)
	assert.Equal(t, BillingAdjustmentStatusPending, row.Status)
	require.NoError(t, ApplyTokenBillingAdjustment(row.OperationKey))
	require.NoError(t, ApplyWalletBillingAdjustment(row.OperationKey))

	stored, err := GetBillingAdjustment(row.OperationKey)
	require.NoError(t, err)
	assert.False(t, stored.DispatchConfirmed)
	assert.True(t, stored.FundingApplied)
	assert.True(t, stored.TokenApplied)
	assert.Equal(t, BillingAdjustmentStatusPending, stored.Status)

	require.NoError(t, MarkBillingAdjustmentDispatchConfirmed(row.OperationKey))
	require.NoError(t, MarkBillingAdjustmentDispatchConfirmed(row.OperationKey))

	stored, err = GetBillingAdjustment(row.OperationKey)
	require.NoError(t, err)
	assert.True(t, stored.DispatchConfirmed)
	assert.True(t, stored.FundingApplied)
	assert.True(t, stored.TokenApplied)
	assert.Equal(t, BillingAdjustmentStatusCompleted, stored.Status)
}

func TestDispatchRequiredInitialReserveCanPreauthorizeBeforeConfirmation(t *testing.T) {
	truncateTables(t)
	const userId, tokenId = 7221, 7221
	createBillingAdjustmentTestUser(t, userId, 100, false)
	createBillingAdjustmentTestToken(t, tokenId, userId, 100, 0)
	row := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey:        "dispatch-required-initial-reserve",
		RequestId:           "dispatch-required-initial-reserve-request",
		Kind:                BillingAdjustmentKindInitialReserve,
		FundingSource:       billingAdjustmentSourceWallet,
		UserId:              userId,
		TokenId:             tokenId,
		FundingDelta:        10,
		FundingTarget:       10,
		TokenDelta:          10,
		FundingRequired:     true,
		TokenRequired:       true,
		TokenEnforceBalance: true,
		DispatchRequired:    true,
	})

	require.NoError(t, ApplyTokenBillingAdjustment(row.OperationKey))
	require.NoError(t, ApplyWalletBillingAdjustment(row.OperationKey))

	stored, err := GetBillingAdjustment(row.OperationKey)
	require.NoError(t, err)
	assert.True(t, stored.FundingApplied)
	assert.True(t, stored.TokenApplied)
	assert.False(t, stored.DispatchConfirmed)
	assert.Equal(t, BillingAdjustmentStatusPending, stored.Status)

	var user User
	require.NoError(t, DB.Select("quota").First(&user, userId).Error)
	assert.Equal(t, 90, user.Quota)
	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, tokenId).Error)
	assert.Equal(t, 90, token.RemainQuota)
	assert.Equal(t, 10, token.UsedQuota)

	require.NoError(t, MarkBillingAdjustmentsDispatchConfirmed([]string{row.OperationKey}))
	stored, err = GetBillingAdjustment(row.OperationKey)
	require.NoError(t, err)
	assert.True(t, stored.DispatchConfirmed)
	assert.Equal(t, BillingAdjustmentStatusCompleted, stored.Status)
}

func TestDispatchConfirmationBatchIsAtomic(t *testing.T) {
	truncateTables(t)
	first := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey:     "dispatch-batch-first",
		RequestId:        "dispatch-batch-request",
		Kind:             BillingAdjustmentKindInitialReserve,
		DispatchRequired: true,
	})
	second := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey:     "dispatch-batch-second",
		RequestId:        "dispatch-batch-request",
		Kind:             BillingAdjustmentKindReserve,
		DispatchRequired: true,
	})

	require.Error(t, MarkBillingAdjustmentsDispatchConfirmed([]string{first.OperationKey, "missing-dispatch-operation"}))
	stored, err := GetBillingAdjustment(first.OperationKey)
	require.NoError(t, err)
	assert.False(t, stored.DispatchConfirmed)

	require.NoError(t, MarkBillingAdjustmentsDispatchConfirmed([]string{first.OperationKey, second.OperationKey}))
	for _, operationKey := range []string{first.OperationKey, second.OperationKey} {
		stored, err = GetBillingAdjustment(operationKey)
		require.NoError(t, err)
		assert.True(t, stored.DispatchConfirmed)
	}
}

func TestUndispatchedReserveCanCloseOnlyAfterMatchingFinalIntent(t *testing.T) {
	truncateTables(t)
	reservation := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey:     "reserve-before-final-intent",
		RequestId:        "shared-final-intent-request",
		Kind:             BillingAdjustmentKindReserve,
		DispatchRequired: true,
	})
	wrongFinal := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey: "wrong-final-intent",
		RequestId:    "different-request",
		Kind:         BillingAdjustmentKindRefund,
	})
	require.ErrorIs(t,
		CancelBillingReservationAfterFinalIntent(reservation.OperationKey, wrongFinal.OperationKey),
		ErrBillingAdjustmentDispatchConflict,
	)

	finalIntent := createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
		OperationKey: "matching-final-intent",
		RequestId:    reservation.RequestId,
		Kind:         BillingAdjustmentKindRefund,
	})
	require.NoError(t, CancelBillingReservationAfterFinalIntent(reservation.OperationKey, finalIntent.OperationKey))
	require.NoError(t, CancelBillingReservationAfterFinalIntent(reservation.OperationKey, finalIntent.OperationKey))

	stored, err := GetBillingAdjustment(reservation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, BillingAdjustmentStatusCanceled, stored.Status)
	assert.Contains(t, stored.LastError, finalIntent.OperationKey)
	var count int64
	require.NoError(t, DB.Model(&BillingAdjustmentJournal{}).Count(&count).Error)
	assert.Equal(t, int64(3), count, "closing after a final intent must not create a second compensation journal")
}

func TestBillingAdjustmentErrorsBackOffAndFreshRowsAreNotStarved(t *testing.T) {
	truncateTables(t)
	rows := make([]*BillingAdjustmentJournal, 0, 4)
	for index := 0; index < 4; index++ {
		rows = append(rows, createBillingAdjustmentTestRow(t, BillingAdjustmentInput{
			OperationKey:    fmt.Sprintf("recovery-fairness-%d", index),
			RequestId:       fmt.Sprintf("recovery-fairness-request-%d", index),
			Kind:            BillingAdjustmentKindSettle,
			FundingSource:   billingAdjustmentSourceWallet,
			UserId:          7300 + index,
			FundingDelta:    1,
			FundingRequired: true,
		}))
	}
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&BillingAdjustmentJournal{}).
		Where("id IN ?", []int64{rows[0].ID, rows[1].ID, rows[2].ID}).
		Updates(map[string]any{"attempt_count": 5, "recover_after": now - 60}).Error)
	require.NoError(t, DB.Model(&BillingAdjustmentJournal{}).
		Where("id = ?", rows[3].ID).
		Updates(map[string]any{"attempt_count": 0, "recover_after": now - 1}).Error)

	due, err := ListPendingBillingAdjustments(3)
	require.NoError(t, err)
	require.Len(t, due, 3)
	assert.Equal(t, rows[3].OperationKey, due[0].OperationKey)

	require.NoError(t, RecordBillingAdjustmentError(rows[3].OperationKey, errors.New("temporary projection failure")))
	firstAttempt, err := GetBillingAdjustment(rows[3].OperationKey)
	require.NoError(t, err)
	assert.Equal(t, 1, firstAttempt.AttemptCount)
	assert.Equal(t, billingAdjustmentRetryBaseSeconds, firstAttempt.RecoverAfter-firstAttempt.UpdatedAt)

	require.NoError(t, DB.Model(&BillingAdjustmentJournal{}).
		Where("id = ?", rows[3].ID).
		Updates(map[string]any{"attempt_count": 30, "recover_after": now - 1}).Error)
	require.NoError(t, RecordBillingAdjustmentError(rows[3].OperationKey, errors.New("persistent projection failure")))
	cappedAttempt, err := GetBillingAdjustment(rows[3].OperationKey)
	require.NoError(t, err)
	assert.Equal(t, 31, cappedAttempt.AttemptCount)
	assert.Equal(t, billingAdjustmentRetryMaxSeconds, cappedAttempt.RecoverAfter-cappedAttempt.UpdatedAt)
}
