package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTransferAffQuotaAllocatesTrackedRewardsFIFOAndSupportsSameSecondTransfers(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	require.NoError(t, DB.Create(&User{
		Id: 991, Username: "fifo-inviter", Status: common.UserStatusEnabled,
		Quota: 10, AffQuota: 700, AffHistoryQuota: 700,
	}).Error)
	rewards := []*InvitationReward{
		{InviterId: 991, InviteeId: 992, RewardType: InvitationRewardTypeFirstRequest, RewardQuota: 300, Status: InvitationRewardStatusSettled, SettledAt: 100, CreatedAt: 100},
		{InviterId: 991, InviteeId: 993, RewardType: InvitationRewardTypeFirstRequest, RewardQuota: 400, Status: InvitationRewardStatusSettled, SettledAt: 100, CreatedAt: 100},
	}
	for _, reward := range rewards {
		require.NoError(t, DB.Create(reward).Error)
		require.NoError(t, createInvitationRewardFundTransactionTx(DB, reward))
	}

	var user User
	require.NoError(t, DB.Where("id = ?", 991).First(&user).Error)
	require.NoError(t, user.TransferAffQuotaToQuota(500))
	require.NoError(t, DB.Where("id = ?", rewards[0].Id).First(rewards[0]).Error)
	require.NoError(t, DB.Where("id = ?", rewards[1].Id).First(rewards[1]).Error)
	assert.Equal(t, 300, rewards[0].TransferredQuota)
	assert.Equal(t, 200, rewards[1].TransferredQuota)

	var firstTransfer PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("user_id = ? AND kind = ?", 991, PromotionFundKindInvitationRewardTransferred).
		First(&firstTransfer).Error)
	require.Len(t, firstTransfer.Legs, 4)
	assert.Equal(t, []int{rewards[0].Id, rewards[0].Id, rewards[1].Id, rewards[1].Id}, []int{
		firstTransfer.Legs[0].SourceId, firstTransfer.Legs[1].SourceId,
		firstTransfer.Legs[2].SourceId, firstTransfer.Legs[3].SourceId,
	})
	assert.Equal(t, []int64{-300, 300, -200, 200}, []int64{
		firstTransfer.Legs[0].Amount, firstTransfer.Legs[1].Amount,
		firstTransfer.Legs[2].Amount, firstTransfer.Legs[3].Amount,
	})

	require.NoError(t, user.TransferAffQuotaToQuota(50))
	require.NoError(t, user.TransferAffQuotaToQuota(50))
	var transfers []PromotionFundTransaction
	require.NoError(t, DB.Where("user_id = ? AND kind = ?", 991, PromotionFundKindInvitationRewardTransferred).
		Order("id ASC").Find(&transfers).Error)
	require.Len(t, transfers, 3)
	assert.NotEqual(t, transfers[1].TransactionKey, transfers[2].TransactionKey)
	var events []PromotionEvent
	require.NoError(t, DB.Where("user_id = ? AND event_type = ?", 991, PromotionEventTypePromotionRewardTransferred).
		Order("id ASC").Find(&events).Error)
	require.Len(t, events, 3)
	assert.NotEqual(t, events[1].EventKey, events[2].EventKey)
}

func TestTransferAffQuotaKeepsUntrackedHistoryInLegacyAggregate(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	require.NoError(t, DB.Create(&User{
		Id: 994, Username: "legacy-inviter", Status: common.UserStatusEnabled,
		AffQuota: 1000, AffHistoryQuota: 1000,
	}).Error)
	tracked := &InvitationReward{
		InviterId: 994, InviteeId: 995, RewardType: InvitationRewardTypeFirstRequest,
		RewardQuota: 200, Status: InvitationRewardStatusSettled, SettledAt: 100, CreatedAt: 100,
	}
	untracked := &InvitationReward{
		InviterId: 994, InviteeId: 996, RewardType: InvitationRewardTypeFirstRequest,
		RewardQuota: 300, Status: InvitationRewardStatusSettled, SettledAt: 99, CreatedAt: 99,
	}
	require.NoError(t, DB.Create(tracked).Error)
	require.NoError(t, DB.Create(untracked).Error)
	require.NoError(t, createInvitationRewardFundTransactionTx(DB, tracked))

	var user User
	require.NoError(t, DB.Where("id = ?", 994).First(&user).Error)
	require.NoError(t, user.TransferAffQuotaToQuota(900))
	require.NoError(t, DB.Where("id = ?", tracked.Id).First(tracked).Error)
	require.NoError(t, DB.Where("id = ?", untracked.Id).First(untracked).Error)
	assert.Equal(t, 100, tracked.TransferredQuota)
	assert.Zero(t, untracked.TransferredQuota)

	var transfer PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("user_id = ? AND kind = ?", 994, PromotionFundKindInvitationRewardTransferred).
		First(&transfer).Error)
	require.Len(t, transfer.Legs, 4)
	assert.Equal(t, PromotionFundSourceLegacyAggregate, transfer.Legs[0].SourceType)
	assert.Equal(t, PromotionFundSourceLegacyAggregate, transfer.Legs[1].SourceType)
	assert.Equal(t, tracked.Id, transfer.Legs[2].SourceId)
	assert.Equal(t, tracked.Id, transfer.Legs[3].SourceId)
	assert.Equal(t, []int64{-800, 800, -100, 100}, []int64{
		transfer.Legs[0].Amount, transfer.Legs[1].Amount, transfer.Legs[2].Amount, transfer.Legs[3].Amount,
	})
}

func TestInvitationRewardRefundReversesOnlyItsAllocatedSource(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	require.NoError(t, DB.Create(&User{
		Id: 997, Username: "refund-inviter", Status: common.UserStatusEnabled,
		AffCode: "refund-inviter-997", AffQuota: 700, AffHistoryQuota: 700,
	}).Error)
	require.NoError(t, DB.Create(&User{
		Id: 998, Username: "refund-invitee", Status: common.UserStatusEnabled, Quota: 1000,
		AffCode: "refund-invitee-998",
	}).Error)
	topUp := &TopUp{
		UserId: 998, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1000, PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "reward-source-refund", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: time.Now().Unix(), CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	target := &InvitationReward{
		InviterId: 997, InviteeId: 998, RewardType: InvitationRewardTypeFirstTopUp,
		RewardQuota: 300, TriggerTopUpId: topUp.Id, TriggerTradeNo: topUp.TradeNo,
		Status: InvitationRewardStatusSettled, SettledAt: 100, CreatedAt: 100,
	}
	other := &InvitationReward{
		InviterId: 997, InviteeId: 999, RewardType: InvitationRewardTypeFirstRequest,
		RewardQuota: 400, Status: InvitationRewardStatusSettled, SettledAt: 101, CreatedAt: 101,
	}
	for _, reward := range []*InvitationReward{target, other} {
		require.NoError(t, DB.Create(reward).Error)
		require.NoError(t, createInvitationRewardFundTransactionTx(DB, reward))
	}
	var inviter User
	require.NoError(t, DB.Where("id = ?", 997).First(&inviter).Error)
	require.NoError(t, inviter.TransferAffQuotaToQuota(100))
	require.NoError(t, DB.Where("id = ?", target.Id).First(target).Error)
	assert.Equal(t, 100, target.TransferredQuota)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderEpay, TradeNo: topUp.TradeNo, RefundTradeNo: "reward-source-refund-1",
		Kind: PromotionRefundKindFull, PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	require.NoError(t, DB.Where("id = ?", target.Id).First(target).Error)
	require.NoError(t, DB.Where("id = ?", other.Id).First(other).Error)
	assert.Equal(t, InvitationRewardStatusReversed, target.Status)
	assert.Equal(t, InvitationRewardStatusSettled, other.Status)
	require.NoError(t, DB.Where("id = ?", 997).First(&inviter).Error)
	assert.Equal(t, 400, inviter.AffQuota)
	assert.Equal(t, 400, inviter.AffHistoryQuota)
	assert.Zero(t, inviter.Quota)

	var reversal PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("transaction_key = ?", fmt.Sprintf("refund:%d:invitation_reward:%d", refundCase.Id, target.Id)).
		First(&reversal).Error)
	require.Len(t, reversal.Legs, 2)
	var issued PromotionFundTransaction
	require.NoError(t, DB.Where("transaction_key = ?", fmt.Sprintf("invitation_reward:%d:issued", target.Id)).First(&issued).Error)
	assert.Equal(t, issued.Id, reversal.ReversesTransactionId)
	assert.Equal(t, PromotionFundAccountReferralCredit, reversal.Legs[0].Account)
	assert.Equal(t, int64(-200), reversal.Legs[0].Amount)
	assert.Equal(t, PromotionFundAccountAPIBalance, reversal.Legs[1].Account)
	assert.Equal(t, int64(-100), reversal.Legs[1].Amount)
	assert.Equal(t, target.Id, reversal.Legs[0].SourceId)
	assert.Equal(t, target.Id, reversal.Legs[1].SourceId)
}

func TestLegacyInvitationRewardRefundPreservesCanonicalReferralCredit(t *testing.T) {
	truncateTables(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	inviter := &User{
		Username: "mixed-reward-inviter", AffCode: "mixed-reward-inviter",
		Status: common.UserStatusEnabled, AffQuota: 700, AffHistoryQuota: 700,
	}
	require.NoError(t, DB.Create(inviter).Error)
	invitee := &User{
		Username: "mixed-reward-invitee", AffCode: "mixed-reward-invitee",
		Status: common.UserStatusEnabled, Quota: 1000, InviterId: inviter.Id,
	}
	require.NoError(t, DB.Create(invitee).Error)
	topUp := &TopUp{
		UserId: invitee.Id, Purpose: TopUpPurposeAPIBalance, Amount: 10, Money: 10,
		CreditedQuota: 1000, PaidAmountMinor: 1000, PaidCurrency: "CNY", PaidAmountVerified: true,
		TradeNo: "mixed-reward-refund", PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: time.Now().Unix(), CompleteTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())
	legacyTarget := &InvitationReward{
		InviterId: inviter.Id, InviteeId: invitee.Id, RewardType: InvitationRewardTypeFirstTopUp,
		RewardQuota: 300, TriggerTopUpId: topUp.Id, TriggerTradeNo: topUp.TradeNo,
		Status: InvitationRewardStatusSettled, SettledAt: 100, CreatedAt: 100,
	}
	canonicalReward := &InvitationReward{
		InviterId: inviter.Id, InviteeId: invitee.Id + 1, RewardType: InvitationRewardTypeFirstRequest,
		RewardQuota: 400, Status: InvitationRewardStatusSettled, SettledAt: 101, CreatedAt: 101,
	}
	require.NoError(t, DB.Create(legacyTarget).Error)
	require.NoError(t, DB.Create(canonicalReward).Error)
	require.NoError(t, createInvitationRewardFundTransactionTx(DB, canonicalReward))

	require.NoError(t, inviter.TransferAffQuotaToQuota(300))
	require.NoError(t, DB.First(legacyTarget, legacyTarget.Id).Error)
	require.NoError(t, DB.First(canonicalReward, canonicalReward.Id).Error)
	assert.Zero(t, legacyTarget.TransferredQuota, "legacy aggregate transfers cannot identify an individual reward")
	assert.Zero(t, canonicalReward.TransferredQuota)

	refundCase, err := HandlePromotionRefund(PromotionRefundInput{
		Provider: PaymentProviderEpay, TradeNo: topUp.TradeNo, RefundTradeNo: "mixed-reward-refund-event",
		Kind: PromotionRefundKindFull, PaidAmountMinor: 1000, RefundedAmountMinor: 1000, Currency: "CNY",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionRefundCaseStatusResolved, refundCase.Status)
	require.NoError(t, DB.First(legacyTarget, legacyTarget.Id).Error)
	assert.Equal(t, InvitationRewardStatusReversed, legacyTarget.Status)

	require.NoError(t, DB.First(inviter, inviter.Id).Error)
	assert.Equal(t, 400, inviter.AffQuota, "the refund must preserve the canonical reward's referral balance")
	assert.Equal(t, 400, inviter.AffHistoryQuota)
	assert.Zero(t, inviter.Quota, "the transferred legacy reward is recovered from the wallet")
	assert.Zero(t, inviter.RefundDebtQuota)

	require.NoError(t, inviter.TransferAffQuotaToQuota(400))
	require.NoError(t, DB.First(canonicalReward, canonicalReward.Id).Error)
	assert.Equal(t, 400, canonicalReward.TransferredQuota)
}
