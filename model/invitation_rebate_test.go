package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertInviterAndInviteeForRebateTest(t *testing.T, inviterID int, inviteeID int) {
	t.Helper()

	inviter := &User{
		Id:       inviterID,
		Username: "rebate_inviter",
		Status:   common.UserStatusEnabled,
	}
	invitee := &User{
		Id:        inviteeID,
		Username:  "rebate_invitee",
		Status:    common.UserStatusEnabled,
		InviterId: inviterID,
	}

	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)
}

func getInvitationRebateCountForTest(t *testing.T, inviterID int) int64 {
	t.Helper()

	var count int64
	require.NoError(t, DB.Model(&InvitationRebate{}).Where("inviter_id = ?", inviterID).Count(&count).Error)
	return count
}

func getInvitationQuotaForTest(t *testing.T, userID int) (int, int) {
	t.Helper()

	var user User
	require.NoError(t, DB.Select("aff_quota", "aff_history").Where("id = ?", userID).First(&user).Error)
	return user.AffQuota, user.AffHistoryQuota
}

func TestRechargeWaffo_SettlesInvitationRebate(t *testing.T) {
	truncateTables(t)

	insertInviterAndInviteeForRebateTest(t, 501, 502)
	insertTopUpForPaymentGuardTest(t, "rebate-waffo-order", 502, PaymentProviderWaffo)

	topUp := GetTopUpByTradeNo("rebate-waffo-order")
	require.NotNil(t, topUp)
	topUp.Amount = 10
	topUp.Money = 20
	require.NoError(t, topUp.Update())

	require.NoError(t, RechargeWaffo("rebate-waffo-order", "127.0.0.1"))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 501)
	expectedQuota := CalculateInvitationRebateQuota(20)
	assert.Equal(t, expectedQuota, affQuota)
	assert.Equal(t, expectedQuota, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRebateCountForTest(t, 501))
}

func TestSyncInvitationRebatesForInviter_BackfillsOnlyOnce(t *testing.T) {
	truncateTables(t)

	insertInviterAndInviteeForRebateTest(t, 601, 602)

	topUp := &TopUp{
		UserId:          602,
		Amount:          10,
		Money:           12.5,
		TradeNo:         "rebate-epay-backfill",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, SyncInvitationRebatesForInviter(601))
	require.NoError(t, SyncInvitationRebatesForInviter(601))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 601)
	expectedQuota := CalculateInvitationRebateQuota(12.5)
	assert.Equal(t, expectedQuota, affQuota)
	assert.Equal(t, expectedQuota, affHistoryQuota)
	assert.Equal(t, int64(1), getInvitationRebateCountForTest(t, 601))
}

func TestSyncInvitationRebatesForInviter_ExcludesSubscriptionTopUps(t *testing.T) {
	truncateTables(t)

	insertInviterAndInviteeForRebateTest(t, 701, 702)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 801)
	insertSubscriptionOrderForPaymentGuardTest(t, "rebate-subscription-order", 702, plan.Id, PaymentProviderStripe)

	require.NoError(t, CompleteSubscriptionOrder("rebate-subscription-order", `{"provider":"stripe"}`, PaymentProviderStripe, PaymentMethodStripe))
	require.NoError(t, SyncInvitationRebatesForInviter(701))

	affQuota, affHistoryQuota := getInvitationQuotaForTest(t, 701)
	assert.Zero(t, affQuota)
	assert.Zero(t, affHistoryQuota)
	assert.Equal(t, int64(0), getInvitationRebateCountForTest(t, 701))
}
