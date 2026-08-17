package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettleDuePromotionCommissionsMakesMaturedCashAvailable(t *testing.T) {
	truncate(t)
	seedUser(t, 3005, 0)
	rebate := &model.InvitationRebate{
		InviterId:          3005,
		InviteeId:          3006,
		TopUpId:            9005,
		TradeNo:            "settle-due-test",
		PaidAmountMinor:    10_000,
		PaidCurrency:       "CNY",
		PaidAmountVerified: true,
		RebateAmountMinor:  1_000,
		RebateCurrency:     "CNY",
		Cashable:           true,
		RebateQuota:        5000,
		SettleAfter:        time.Now().Unix() - 1,
		Status:             model.InvitationRebateStatusPending,
		CreatedAt:          time.Now().Unix() - 10,
	}
	require.NoError(t, model.DB.Create(rebate).Error)
	ledger := &model.PromotionCommissionLedger{
		UserId:           3005,
		InviteeId:        3006,
		SourceType:       model.PromotionCommissionSourceTopUpRebate,
		SourceId:         rebate.Id,
		SourceTradeNo:    rebate.TradeNo,
		Cashable:         true,
		Currency:         "CNY",
		GrossAmountCents: 1000,
		NetAmountCents:   1000,
		QuotaEquivalent:  5000,
		Status:           model.PromotionCommissionStatusPending,
		AvailableAt:      rebate.SettleAfter,
	}
	require.NoError(t, model.DB.Create(ledger).Error)

	readOnlySummary, err := GetGrowthSummary(3005)
	require.NoError(t, err)
	assert.Zero(t, readOnlySummary.CashCommission.AvailableAmountCents)
	assert.Equal(t, int64(1000), readOnlySummary.CashCommission.PendingAmountCents)
	require.NoError(t, model.DB.Select("status").Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, model.InvitationRebateStatusPending, rebate.Status)

	summary, err := SettleDuePromotionCommissions(3005)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), summary.CashCommission.AvailableAmountCents)
	assert.Zero(t, summary.CashCommission.PendingAmountCents)

	require.NoError(t, model.DB.Select("status").Where("id = ?", rebate.Id).First(rebate).Error)
	assert.Equal(t, model.InvitationRebateStatusSettled, rebate.Status)
	require.NoError(t, model.DB.Select("status").Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
}
