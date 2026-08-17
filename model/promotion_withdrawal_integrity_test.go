package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidatePromotionWithdrawalLedgersPayableRejectsBrokenAllocation(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*PromotionWithdrawal, *PromotionCommissionLedger, *PromotionWithdrawalItem)
	}{
		{
			name: "ledger belongs to another user",
			mutate: func(_ *PromotionWithdrawal, ledger *PromotionCommissionLedger, _ *PromotionWithdrawalItem) {
				ledger.UserId++
			},
		},
		{
			name: "item amount differs from ledger",
			mutate: func(_ *PromotionWithdrawal, _ *PromotionCommissionLedger, item *PromotionWithdrawalItem) {
				item.AmountCents--
			},
		},
		{
			name: "item total differs from withdrawal",
			mutate: func(withdrawal *PromotionWithdrawal, _ *PromotionCommissionLedger, _ *PromotionWithdrawalItem) {
				withdrawal.GrossAmountCents++
				withdrawal.NetAmountCents++
			},
		},
		{
			name: "withdrawal net calculation is inconsistent",
			mutate: func(withdrawal *PromotionWithdrawal, _ *PromotionCommissionLedger, _ *PromotionWithdrawalItem) {
				withdrawal.FeeAmountCents = 1
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openPromotionFundBackfillTestDB(t)
			withdrawal := &PromotionWithdrawal{
				Id: 1, UserId: 101, Currency: "CNY", GrossAmountCents: 500, NetAmountCents: 500,
				Status: PromotionWithdrawalStatusPendingReview, CreatedAt: 10,
			}
			ledger := &PromotionCommissionLedger{
				Id: 1, UserId: withdrawal.UserId, SourceType: "test", SourceId: 1,
				Cashable: true, Currency: "CNY", GrossAmountCents: 500, NetAmountCents: 500,
				Status: PromotionCommissionStatusWithdrawing, CreatedAt: 10,
			}
			item := &PromotionWithdrawalItem{Id: 1, WithdrawalId: withdrawal.Id, LedgerId: ledger.Id, AmountCents: 500, CreatedAt: 10}
			testCase.mutate(withdrawal, ledger, item)
			require.NoError(t, db.Create(ledger).Error)
			require.NoError(t, db.Create(withdrawal).Error)
			require.NoError(t, db.Create(item).Error)

			err := db.Transaction(func(tx *gorm.DB) error {
				return ValidatePromotionWithdrawalLedgersPayableTx(tx, withdrawal.Id)
			})
			require.ErrorIs(t, err, ErrPromotionWithdrawalLedgerNotPayable)
		})
	}
}
