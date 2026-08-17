package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillTopUpPurposesSeparatesSubscriptionCompatibilityRows(t *testing.T) {
	truncateTables(t)
	user := insertUserForPaymentGuardTest(t, 991, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 992)
	const subscriptionTradeNo = "purpose-subscription"
	insertSubscriptionOrderForPaymentGuardTest(t, subscriptionTradeNo, user.Id, plan.Id, PaymentProviderStripe)

	rows := []*TopUp{
		{UserId: user.Id, TradeNo: subscriptionTradeNo, PaymentProvider: PaymentProviderStripe, Purpose: TopUpPurposeAPIBalance, Status: common.TopUpStatusSuccess},
		{UserId: user.Id, TradeNo: "purpose-api", Purpose: TopUpPurposeAPIBalance, Status: common.TopUpStatusSuccess},
		{UserId: user.Id, TradeNo: "purpose-explicit", Purpose: TopUpPurposeSubscription, Status: common.TopUpStatusSuccess},
	}
	require.NoError(t, DB.Create(&rows).Error)
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no IN ?", []string{subscriptionTradeNo, "purpose-api"}).Update("purpose", "").Error)

	require.NoError(t, BackfillTopUpPurposes())

	var stored []TopUp
	require.NoError(t, DB.Where("trade_no IN ?", []string{subscriptionTradeNo, "purpose-api", "purpose-explicit"}).Order("trade_no ASC").Find(&stored).Error)
	require.Len(t, stored, 3)
	purposes := make(map[string]string, len(stored))
	for _, topUp := range stored {
		purposes[topUp.TradeNo] = topUp.Purpose
	}
	assert.Equal(t, TopUpPurposeSubscription, purposes[subscriptionTradeNo])
	assert.Equal(t, TopUpPurposeAPIBalance, purposes["purpose-api"])
	assert.Equal(t, TopUpPurposeSubscription, purposes["purpose-explicit"])
}

func TestBackfillTopUpPurposesRequiresMatchingUserAndProvider(t *testing.T) {
	truncateTables(t)
	orderUser := insertUserForPaymentGuardTest(t, 993, 0)
	topUpUser := &User{Id: 994, Username: "purpose-topup-user", AffCode: "purpose-topup-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(topUpUser).Error)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 995)
	insertSubscriptionOrderForPaymentGuardTest(t, "purpose-other-user", orderUser.Id, plan.Id, PaymentProviderStripe)
	insertSubscriptionOrderForPaymentGuardTest(t, "purpose-other-provider", topUpUser.Id, plan.Id, PaymentProviderStripe)

	rows := []*TopUp{
		{UserId: topUpUser.Id, TradeNo: "purpose-other-user", PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess},
		{UserId: topUpUser.Id, TradeNo: "purpose-other-provider", PaymentProvider: PaymentProviderCreem, Status: common.TopUpStatusSuccess},
	}
	require.NoError(t, DB.Create(&rows).Error)
	require.NoError(t, DB.Model(&TopUp{}).Where("id IN ?", []int{rows[0].Id, rows[1].Id}).Update("purpose", "").Error)

	require.NoError(t, BackfillTopUpPurposes())

	var stored []TopUp
	require.NoError(t, DB.Where("id IN ?", []int{rows[0].Id, rows[1].Id}).Order("id ASC").Find(&stored).Error)
	require.Len(t, stored, 2)
	assert.Equal(t, TopUpPurposeAPIBalance, stored[0].Purpose)
	assert.Equal(t, TopUpPurposeAPIBalance, stored[1].Purpose)
}
