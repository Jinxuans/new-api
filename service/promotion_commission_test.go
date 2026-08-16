package service

import (
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedPromotionCommissionLedger(t *testing.T, userID int, amountCents int64, quotaEquivalent int) *model.PromotionCommissionLedger {
	t.Helper()
	rebate := &model.InvitationRebate{
		InviterId:          userID,
		InviteeId:          userID + 100_000,
		TopUpId:            int(amountCents),
		TradeNo:            "commission-test",
		PaidAmountMinor:    amountCents,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		RebateAmountMinor:  amountCents,
		RebateCurrency:     "CNY",
		Cashable:           true,
		RebateQuota:        quotaEquivalent,
		Status:             model.InvitationRebateStatusSettled,
	}
	require.NoError(t, model.DB.Create(rebate).Error)
	ledger := &model.PromotionCommissionLedger{
		UserId:           userID,
		SourceType:       model.PromotionCommissionSourceTopUpRebate,
		SourceId:         rebate.Id,
		SourceTradeNo:    "commission-test",
		Cashable:         true,
		Currency:         "CNY",
		GrossAmountCents: amountCents,
		NetAmountCents:   amountCents,
		QuotaEquivalent:  quotaEquivalent,
		Status:           model.PromotionCommissionStatusSettled,
	}
	require.NoError(t, model.DB.Create(ledger).Error)
	return ledger
}

func TestTransferAllSettledPromotionCommissionsToQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 3001, 100)
	seedPromotionCommissionLedger(t, 3001, 1234, 5678)

	quota, err := TransferAllSettledPromotionCommissionsToQuota(3001)
	require.NoError(t, err)
	assert.Equal(t, 5678, quota)

	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 3001).First(&user).Error)
	assert.Equal(t, 5778, user.Quota)

	var ledger model.PromotionCommissionLedger
	require.NoError(t, model.DB.Where("user_id = ?", 3001).First(&ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusTransferred, ledger.Status)
	assert.NotZero(t, ledger.TransferredAt)

	var event model.PromotionEvent
	require.NoError(t, model.DB.Where("user_id = ? AND event_type = ?", 3001, model.PromotionEventTypeCommissionTransferred).First(&event).Error)
	assert.Equal(t, 5678, event.QuotaDelta)
	assert.Equal(t, int64(1234), event.CashAmountCents)
}

func TestTransferAllSettledPromotionCommissionsToQuotaRejectsWalletOverflow(t *testing.T) {
	truncate(t)
	seedUser(t, 3004, common.MaxQuota-100)
	ledger := seedPromotionCommissionLedger(t, 3004, 1000, 500)

	_, err := TransferAllSettledPromotionCommissionsToQuota(3004)
	require.ErrorIs(t, err, model.ErrTopUpQuotaLimitExceeded)

	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", 3004).First(&user).Error)
	assert.Equal(t, common.MaxQuota-100, user.Quota)
	require.NoError(t, model.DB.Select("status").Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
}

func TestCreatePromotionWithdrawalLocksLedgersAndRejectReleases(t *testing.T) {
	truncate(t)
	seedUser(t, 3002, 0)
	seedPromotionCommissionLedger(t, 3002, 1000, 5000)

	withdrawal, err := CreatePromotionWithdrawal(3002, PromotionWithdrawalRequest{
		PayoutMethod:  "alipay",
		PayoutAccount: "user@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), withdrawal.NetAmountCents)
	assert.Equal(t, model.PromotionWithdrawalStatusPendingReview, withdrawal.Status)

	var ledger model.PromotionCommissionLedger
	require.NoError(t, model.DB.Where("user_id = ?", 3002).First(&ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusWithdrawing, ledger.Status)

	withdrawal, err = AdminRejectPromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "test"})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusRejected, withdrawal.Status)
	require.NoError(t, model.DB.Where("user_id = ?", 3002).First(&ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)

	var events []model.PromotionEvent
	require.NoError(t, model.DB.Where("user_id = ?", 3002).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Equal(t, model.PromotionEventTypeCommissionWithdrawSubmitted, events[0].EventType)
	assert.Equal(t, model.PromotionEventTypeCommissionWithdrawRejected, events[1].EventType)
}

func TestAdminRejectPromotionWithdrawalCanCloseApprovedRequest(t *testing.T) {
	truncate(t)
	seedUser(t, 3003, 0)
	seedPromotionCommissionLedger(t, 3003, 1000, 5000)

	withdrawal, err := CreatePromotionWithdrawal(3003, PromotionWithdrawalRequest{
		PayoutMethod:  "alipay",
		PayoutAccount: "user@example.com",
	})
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusApproved, withdrawal.Status)

	withdrawal, err = AdminRejectPromotionWithdrawal(withdrawal.Id, 2, PromotionWithdrawalReviewRequest{ReviewNote: "payout cancelled"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("id = ?", withdrawal.Id).First(withdrawal).Error)
	assert.Equal(t, model.PromotionWithdrawalStatusRejected, withdrawal.Status)
	var ledger model.PromotionCommissionLedger
	require.NoError(t, model.DB.Where("user_id = ?", 3003).First(&ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
}

func TestCreatePromotionWithdrawalValidatesRuneLengths(t *testing.T) {
	testCases := []struct {
		name string
		req  PromotionWithdrawalRequest
	}{
		{name: "method", req: PromotionWithdrawalRequest{PayoutMethod: strings.Repeat("方", 33), PayoutAccount: "account"}},
		{name: "account", req: PromotionWithdrawalRequest{PayoutMethod: "bank", PayoutAccount: strings.Repeat("账", 201)}},
		{name: "remark", req: PromotionWithdrawalRequest{PayoutMethod: "bank", PayoutAccount: "account", Remark: strings.Repeat("注", 501)}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreatePromotionWithdrawal(3010, tc.req)
			require.Error(t, err)
		})
	}
}

func TestCreatePromotionWithdrawalRejectsAggregateOverflow(t *testing.T) {
	testCases := []struct {
		name    string
		amounts []int64
		quotas  []int
	}{
		{name: "cash amount", amounts: []int64{math.MaxInt64, 1}, quotas: []int{1, 1}},
		{name: "quota equivalent", amounts: []int64{1000, 1001}, quotas: []int{common.MaxQuota - 2, 2}},
	}
	for index, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			userId := 3020 + index
			seedUser(t, userId, 0)
			for ledgerIndex := range tc.amounts {
				seedPromotionCommissionLedger(t, userId, tc.amounts[ledgerIndex], tc.quotas[ledgerIndex])
			}

			_, err := CreatePromotionWithdrawal(userId, PromotionWithdrawalRequest{PayoutMethod: "bank", PayoutAccount: "account"})
			require.Error(t, err)
			var unsettled int64
			require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
				Where("user_id = ? AND status = ?", userId, model.PromotionCommissionStatusSettled).
				Count(&unsettled).Error)
			assert.Equal(t, int64(2), unsettled)
		})
	}
}

func TestAdminPromotionWithdrawalRechecksVerifiedCashableLedgers(t *testing.T) {
	t.Run("approval", func(t *testing.T) {
		truncate(t)
		seedUser(t, 3030, 0)
		ledger := seedPromotionCommissionLedger(t, 3030, 1000, 5000)
		withdrawal, err := CreatePromotionWithdrawal(3030, PromotionWithdrawalRequest{
			PayoutMethod:  "alipay",
			PayoutAccount: "user@example.com",
		})
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(&model.InvitationRebate{}).
			Where("id = ?", ledger.SourceId).
			Update("paid_amount_verified", false).Error)
		require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
			Where("id = ?", ledger.Id).
			Update("cashable", false).Error)

		_, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "approve"})
		require.ErrorIs(t, err, model.ErrPromotionWithdrawalLedgerNotPayable)
		require.NoError(t, model.DB.Where("id = ?", withdrawal.Id).First(withdrawal).Error)
		assert.Equal(t, model.PromotionWithdrawalStatusPendingReview, withdrawal.Status)
		require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
		assert.Equal(t, model.PromotionCommissionStatusWithdrawing, ledger.Status)

		_, err = AdminRejectPromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "manual rejection"})
		require.NoError(t, err)
		require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
		assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
		assert.False(t, ledger.Cashable)
	})

	t.Run("payout", func(t *testing.T) {
		truncate(t)
		seedUser(t, 3031, 0)
		ledger := seedPromotionCommissionLedger(t, 3031, 1100, 5500)
		withdrawal, err := CreatePromotionWithdrawal(3031, PromotionWithdrawalRequest{
			PayoutMethod:  "alipay",
			PayoutAccount: "user@example.com",
		})
		require.NoError(t, err)
		withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "approve"})
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(&model.InvitationRebate{}).
			Where("id = ?", ledger.SourceId).
			Update("paid_amount_verified", false).Error)
		require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).
			Where("id = ?", ledger.Id).
			Update("cashable", false).Error)

		_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 2, PromotionWithdrawalReviewRequest{TradeNo: "payout-3031"})
		require.ErrorIs(t, err, model.ErrPromotionWithdrawalLedgerNotPayable)
		require.NoError(t, model.DB.Where("id = ?", withdrawal.Id).First(withdrawal).Error)
		assert.Equal(t, model.PromotionWithdrawalStatusApproved, withdrawal.Status)
		require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
		assert.Equal(t, model.PromotionCommissionStatusWithdrawing, ledger.Status)

		withdrawal, err = AdminRejectPromotionWithdrawal(withdrawal.Id, 2, PromotionWithdrawalReviewRequest{ReviewNote: "legacy commission frozen"})
		require.NoError(t, err)
		assert.Equal(t, model.PromotionWithdrawalStatusRejected, withdrawal.Status)
		require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
		assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
		assert.False(t, ledger.Cashable)
	})
}
