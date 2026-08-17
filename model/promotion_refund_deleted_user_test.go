package model

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLateRefundRecoversFromSoftDeletedUserAndSurvivesRestore(t *testing.T) {
	truncateTables(t)
	user := &User{
		Username: "soft-deleted-refund-user", AffCode: "soft-deleted-refund-user",
		Status: common.UserStatusEnabled, Quota: 200,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: user.Id, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1000, PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "soft-deleted-late-refund", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: time.Now().Unix(), CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	require.NoError(t, user.Delete())

	var scoped User
	require.ErrorIs(t, DB.First(&scoped, user.Id).Error, gorm.ErrRecordNotFound)
	var deleted User
	require.NoError(t, DB.Unscoped().First(&deleted, user.Id).Error)
	require.True(t, deleted.DeletedAt.Valid)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderEpay, TradeNo: topUp.TradeNo, RefundTradeNo: "soft-deleted-late-refund-event",
		Kind: PromotionRefundKindFull, PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, user.Id, refundCase.UserId)
	assert.Equal(t, int64(800), refundCase.DebtCreatedQuota)
	assert.Equal(t, PromotionRefundCaseStatusPendingReview, refundCase.Status)

	require.NoError(t, DB.Unscoped().First(&deleted, user.Id).Error)
	assert.Zero(t, deleted.Quota)
	assert.Equal(t, int64(800), deleted.RefundDebtQuota)
	assert.True(t, deleted.RefundHold)
	assert.True(t, deleted.DeletedAt.Valid)

	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Update("deleted_at", nil).Error)
	var restored User
	require.NoError(t, DB.First(&restored, user.Id).Error)
	assert.Equal(t, int64(800), restored.RefundDebtQuota)
	assert.True(t, restored.RefundHold)
}

func TestHardDeleteAllowsUserWithoutFinancialHistory(t *testing.T) {
	truncateTables(t)
	user := &User{Username: "hard-delete-clean-user", AffCode: "hard-delete-clean-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, user.HardDelete())
	require.ErrorIs(t, DB.Unscoped().First(&User{}, user.Id).Error, gorm.ErrRecordNotFound)
}

func TestHardDeletePreservesEveryFinancialEvidenceType(t *testing.T) {
	tests := []struct {
		name string
		seed func(t *testing.T, user *User)
	}{
		{
			name: "top-up",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&TopUp{UserId: user.Id, TradeNo: "hard-delete-topup", Status: common.TopUpStatusFailed}).Error)
			},
		},
		{
			name: "subscription order",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&SubscriptionOrder{
					UserId: user.Id, TradeNo: "hard-delete-subscription", Status: common.TopUpStatusFailed,
				}).Error)
			},
		},
		{
			name: "admin-granted subscription",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&UserSubscription{
					UserId: user.Id, Status: "expired", Source: "admin",
				}).Error)
			},
		},
		{
			name: "invitation rebate",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&InvitationRebate{
					InviterId: user.Id, InviteeId: user.Id + 1000, TopUpId: user.Id + 2000,
					TradeNo: "hard-delete-rebate", Status: InvitationRebateStatusReversed,
				}).Error)
			},
		},
		{
			name: "invitation reward",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&InvitationReward{
					InviterId: user.Id, InviteeId: user.Id + 1000, RewardType: InvitationRewardTypeRegister,
					RewardQuota: 100, Status: InvitationRewardStatusReversed,
				}).Error)
			},
		},
		{
			name: "growth reward",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&GrowthReward{
					UserId: user.Id, ItemCode: GrowthRewardItemFirstTopUp, RewardQuota: 100,
					Status: GrowthRewardStatusReversed,
				}).Error)
			},
		},
		{
			name: "legacy check-in reward",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&Checkin{
					UserId: user.Id, CheckinDate: "2026-08-17", QuotaAwarded: 100,
				}).Error)
			},
		},
		{
			name: "commission ledger",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionCommissionLedger{
					UserId: user.Id, SourceType: PromotionCommissionSourceTopUpRebate, SourceId: user.Id,
					Status: PromotionCommissionStatusReversed,
				}).Error)
			},
		},
		{
			name: "promotion withdrawal",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionWithdrawal{
					UserId: user.Id, Currency: "CNY", NetAmountCents: 100,
					Status: PromotionWithdrawalStatusPaid,
				}).Error)
			},
		},
		{
			name: "promotion fund transaction",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
					return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
						TransactionKey: fmt.Sprintf("hard-delete-fund-%d", user.Id),
						Kind:           "test_fund_history",
						UserId:         user.Id,
						SourceType:     "test",
						SourceId:       user.Id,
						ActorType:      "system",
					}, []PromotionFundTransactionLeg{{
						Account:    PromotionFundAccountAPIBalance,
						Asset:      PromotionFundAssetQuota,
						Amount:     1,
						SourceType: "test",
						SourceId:   user.Id,
					}})
				}))
			},
		},
		{
			name: "promotion event",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionEvent{
					EventKey:    fmt.Sprintf("hard-delete-event-%d", user.Id),
					UserId:      user.Id,
					EventType:   PromotionEventTypeGrowthSubmissionCreated,
					SourceTable: PromotionEventSourceGrowthSubmission,
					SourceId:    user.Id,
					Direction:   PromotionEventDirectionStatus,
				}).Error)
			},
		},
		{
			name: "refund case",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionRefundCase{
					EventKey: "hard-delete-case", UserId: user.Id, Status: PromotionRefundCaseStatusResolved,
					QuotaAmount: 1,
				}).Error)
			},
		},
		{
			name: "refund obligation",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionRefundObligation{
					ObligationKey: "hard-delete-obligation", RefundCaseId: user.Id + 3000, UserId: user.Id,
					Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota,
					Amount: 100, RecoveredAmount: 100, Status: PromotionRefundObligationStatusRecovered,
				}).Error)
			},
		},
		{
			name: "refund action",
			seed: func(t *testing.T, user *User) {
				require.NoError(t, DB.Create(&PromotionRefundAction{
					ActionKey: fmt.Sprintf("hard-delete-action-%d", user.Id), RefundCaseId: user.Id + 4000,
					UserId: user.Id, Action: PromotionRefundActionRecordExternalRepayment, ActorId: 1,
				}).Error)
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			identity := fmt.Sprintf("hard-delete-evidence-%d", index)
			user := &User{Username: identity, AffCode: identity, Status: common.UserStatusEnabled}
			require.NoError(t, DB.Create(user).Error)
			test.seed(t, user)

			err := user.HardDelete()
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrUserFinancialHistory), err)
			require.NoError(t, DB.Unscoped().First(&User{}, user.Id).Error)
		})
	}
}
