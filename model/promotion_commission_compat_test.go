package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeUnverifiedTopUpPromotionCommissionsIsIdempotent(t *testing.T) {
	truncateTables(t)
	legacyRebate := &InvitationRebate{
		InviterId:          3301,
		InviteeId:          3302,
		TopUpId:            1,
		TradeNo:            "legacy-unverified-rebate",
		PaidAmountVerified: false,
		Status:             InvitationRebateStatusSettled,
	}
	verifiedRebate := &InvitationRebate{
		InviterId:          3301,
		InviteeId:          3303,
		TopUpId:            2,
		TradeNo:            "verified-rebate",
		PaidAmountVerified: true,
		Status:             InvitationRebateStatusSettled,
	}
	require.NoError(t, DB.Create(legacyRebate).Error)
	require.NoError(t, DB.Create(verifiedRebate).Error)
	ledgers := []PromotionCommissionLedger{
		{
			UserId:           3301,
			SourceType:       PromotionCommissionSourceTopUpRebate,
			SourceId:         legacyRebate.Id,
			Cashable:         true,
			Currency:         "CNY",
			GrossAmountCents: 100,
			NetAmountCents:   100,
			Status:           PromotionCommissionStatusSettled,
		},
		{
			UserId:           3301,
			SourceType:       PromotionCommissionSourceTopUpRebate,
			SourceId:         verifiedRebate.Id,
			Cashable:         true,
			Currency:         "CNY",
			GrossAmountCents: 200,
			NetAmountCents:   200,
			Status:           PromotionCommissionStatusSettled,
		},
	}
	require.NoError(t, DB.Create(&ledgers).Error)

	require.NoError(t, FreezeUnverifiedTopUpPromotionCommissions())
	require.NoError(t, FreezeUnverifiedTopUpPromotionCommissions())
	require.NoError(t, DB.Order("id ASC").Find(&ledgers).Error)
	assert.False(t, ledgers[0].Cashable)
	assert.True(t, ledgers[1].Cashable)

	payable, err := LockSettledPromotionCommissionLedgersTx(DB, 3301)
	require.NoError(t, err)
	require.Len(t, payable, 1)
	assert.Equal(t, verifiedRebate.Id, payable[0].SourceId)
}

func TestLockSettledPromotionCommissionLedgersRequiresVerifiedTopUpSource(t *testing.T) {
	truncateTables(t)
	rebate := &InvitationRebate{
		InviterId:          3311,
		InviteeId:          3312,
		TopUpId:            3,
		TradeNo:            "query-defense-unverified-rebate",
		PaidAmountVerified: false,
		Status:             InvitationRebateStatusSettled,
	}
	require.NoError(t, DB.Create(rebate).Error)
	ledger := &PromotionCommissionLedger{
		UserId:           3311,
		SourceType:       PromotionCommissionSourceTopUpRebate,
		SourceId:         rebate.Id,
		Cashable:         true,
		Currency:         "CNY",
		GrossAmountCents: 100,
		NetAmountCents:   100,
		Status:           PromotionCommissionStatusSettled,
	}
	require.NoError(t, DB.Create(ledger).Error)

	payable, err := LockSettledPromotionCommissionLedgersTx(DB, 3311)
	require.NoError(t, err)
	assert.Empty(t, payable)
}
