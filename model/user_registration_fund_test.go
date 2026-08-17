package model

import (
	"errors"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func configureRegistrationFundTest(t *testing.T, newUserQuota int, inviteeQuota int) {
	t.Helper()
	oldQuotaForNewUser := common.QuotaForNewUser
	oldQuotaForInvitee := common.QuotaForInvitee
	oldQuotaForInviter := common.QuotaForInviter
	paymentSetting := operation_setting.GetPaymentSetting()
	oldComplianceConfirmed := paymentSetting.ComplianceConfirmed
	oldComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		common.QuotaForNewUser = oldQuotaForNewUser
		common.QuotaForInvitee = oldQuotaForInvitee
		common.QuotaForInviter = oldQuotaForInviter
		paymentSetting.ComplianceConfirmed = oldComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = oldComplianceTermsVersion
	})

	common.QuotaForNewUser = newUserQuota
	common.QuotaForInvitee = inviteeQuota
	common.QuotaForInviter = 0
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func TestUserInsertRecordsRegistrationRewardsAsSeparateFundTransactions(t *testing.T) {
	truncateTables(t)
	configureRegistrationFundTest(t, 100, 1357)

	inviter := &User{
		Username: "registration_fund_inviter",
		AffCode:  "registration_fund_inviter",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(inviter).Error)

	invitee := &User{
		Username: "registration_fund_invitee",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, invitee.Insert(inviter.Id))
	assert.Equal(t, 1457, invitee.Quota)

	var transactions []PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").
		Where("user_id = ?", invitee.Id).
		Order("id ASC").
		Find(&transactions).Error)
	require.Len(t, transactions, 2)

	newUserReward := transactions[0]
	assert.Equal(t, "user_registration:"+strconv.Itoa(invitee.Id)+":new_user_reward", newUserReward.TransactionKey)
	assert.Equal(t, "new_user_registration_reward_issued", newUserReward.Kind)
	assert.Equal(t, "user_registration", newUserReward.SourceType)
	assert.Equal(t, invitee.Id, newUserReward.SourceId)
	assert.Equal(t, "user_registration:"+strconv.Itoa(invitee.Id), newUserReward.SourceKey)
	assert.Equal(t, "system", newUserReward.ActorType)
	assert.Equal(t, invitee.CreatedAt, newUserReward.OccurredAt)
	require.Len(t, newUserReward.Legs, 1)
	assert.Equal(t, PromotionFundAccountAPIBalance, newUserReward.Legs[0].Account)
	assert.Equal(t, PromotionFundAssetQuota, newUserReward.Legs[0].Asset)
	assert.Equal(t, int64(100), newUserReward.Legs[0].Amount)
	assert.Equal(t, "user_registration", newUserReward.Legs[0].SourceType)
	assert.Equal(t, invitee.Id, newUserReward.Legs[0].SourceId)
	require.NotNil(t, newUserReward.Legs[0].BalanceAfter)
	assert.Equal(t, int64(100), *newUserReward.Legs[0].BalanceAfter)

	inviteeReward := transactions[1]
	assert.Equal(t, "user_registration:"+strconv.Itoa(invitee.Id)+":invitee_reward", inviteeReward.TransactionKey)
	assert.Equal(t, "invitee_registration_reward_issued", inviteeReward.Kind)
	assert.Equal(t, "invitation_registration", inviteeReward.SourceType)
	assert.Equal(t, invitee.Id, inviteeReward.SourceId)
	assert.Equal(t, "invitation_registration:"+strconv.Itoa(invitee.Id), inviteeReward.SourceKey)
	assert.Equal(t, "system", inviteeReward.ActorType)
	assert.Equal(t, invitee.CreatedAt, inviteeReward.OccurredAt)
	require.Len(t, inviteeReward.Legs, 1)
	assert.Equal(t, PromotionFundAccountAPIBalance, inviteeReward.Legs[0].Account)
	assert.Equal(t, PromotionFundAssetQuota, inviteeReward.Legs[0].Asset)
	assert.Equal(t, int64(1357), inviteeReward.Legs[0].Amount)
	assert.Equal(t, "invitation_registration", inviteeReward.Legs[0].SourceType)
	assert.Equal(t, invitee.Id, inviteeReward.Legs[0].SourceId)
	require.NotNil(t, inviteeReward.Legs[0].BalanceAfter)
	assert.Equal(t, int64(1457), *inviteeReward.Legs[0].BalanceAfter)
}

func TestUserInsertRollsBackWhenRegistrationFundJournalFails(t *testing.T) {
	truncateTables(t)
	configureRegistrationFundTest(t, 100, 0)

	expectedErr := errors.New("registration fund leg write failed")
	callbackName := "test:fail_registration_fund_leg_write"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "promotion_fund_legs" {
			tx.AddError(expectedErr)
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Create().Remove(callbackName)
	})

	user := &User{
		Username: "registration_fund_rollback",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}
	err := user.Insert(0)
	require.ErrorIs(t, err, expectedErr)

	var userCount int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", user.Username).Count(&userCount).Error)
	assert.Zero(t, userCount)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}
