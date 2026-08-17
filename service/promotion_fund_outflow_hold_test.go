package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromotionFundOutflowsRejectRefundHeldUsers(t *testing.T) {
	t.Run("commission transfer", func(t *testing.T) {
		truncate(t)
		seedUser(t, 3050, 100)
		ledger := seedPromotionCommissionLedger(t, 3050, 1000, 5000)
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 3050).Update("refund_hold", true).Error)

		_, err := TransferAllSettledPromotionCommissionsToQuota(3050, promotionCommissionBalanceExpectation(t, 3050))
		require.ErrorIs(t, err, model.ErrUserRefundHeld)

		var user model.User
		require.NoError(t, model.DB.Where("id = ?", 3050).First(&user).Error)
		assert.Equal(t, 100, user.Quota)
		require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
		assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
	})

	t.Run("cash withdrawal", func(t *testing.T) {
		truncate(t)
		seedUser(t, 3051, 0)
		ledger := seedPromotionCommissionLedger(t, 3051, 1000, 5000)
		require.NoError(t, model.DB.Create(&model.PromotionRefundObligation{
			ObligationKey: "test-open-obligation-3051",
			RefundCaseId:  1,
			UserId:        3051,
			Account:       model.PromotionFundAccountRefundDebt,
			Asset:         model.PromotionFundAssetQuota,
			Amount:        1,
			Status:        model.PromotionRefundObligationStatusOpen,
		}).Error)

		_, err := CreatePromotionWithdrawal(3051, withPromotionCommissionBalanceExpectation(t, 3051, PromotionWithdrawalRequest{
			PayoutMethod: "bank", PayoutAccount: "account-3051",
		}))
		require.ErrorIs(t, err, model.ErrUserRefundHeld)

		require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
		assert.Equal(t, model.PromotionCommissionStatusSettled, ledger.Status)
		var count int64
		require.NoError(t, model.DB.Model(&model.PromotionWithdrawal{}).Where("user_id = ?", 3051).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("referral credit transfer", func(t *testing.T) {
		truncate(t)
		quota := int(common.QuotaPerUnit)
		seedUser(t, 3052, 0)
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 3052).Updates(map[string]interface{}{
			"aff_quota": quota, "refund_hold": true,
		}).Error)
		var user model.User
		require.NoError(t, model.DB.Where("id = ?", 3052).First(&user).Error)

		err := user.TransferAffQuotaToQuota(quota)
		require.ErrorIs(t, err, model.ErrUserRefundHeld)
		require.NoError(t, model.DB.Where("id = ?", 3052).First(&user).Error)
		assert.Equal(t, quota, user.AffQuota)
		assert.Zero(t, user.Quota)
	})
}

func TestPromotionWithdrawalAdminOutflowsRejectRefundHeldUser(t *testing.T) {
	truncate(t)
	for _, actorId := range []int{1, 2, 3} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	seedUser(t, 3053, 0)
	ledger := seedPromotionCommissionLedger(t, 3053, 1000, 5000)
	withdrawal, err := CreatePromotionWithdrawal(3053, withPromotionCommissionBalanceExpectation(t, 3053, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account-3053",
	}))
	require.NoError(t, err)

	setHold := func(value bool) {
		t.Helper()
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 3053).Update("refund_hold", value).Error)
	}
	setHold(true)
	_, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "approve"})
	require.ErrorIs(t, err, model.ErrUserRefundHeld)
	withdrawal, err = GetAdminPromotionWithdrawal(withdrawal.Id)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusPendingReview, withdrawal.Status)
	require.Len(t, withdrawal.Operations, 1)

	setHold(false)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 1, PromotionWithdrawalReviewRequest{ReviewNote: "approve"})
	require.NoError(t, err)
	setHold(true)
	_, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 2, PromotionWithdrawalReviewRequest{TradeNo: "payout-3053"})
	require.ErrorIs(t, err, model.ErrUserRefundHeld)
	withdrawal, err = GetAdminPromotionWithdrawal(withdrawal.Id)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusApproved, withdrawal.Status)
	require.Len(t, withdrawal.Operations, 2)

	setHold(false)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 2, PromotionWithdrawalReviewRequest{TradeNo: "payout-3053"})
	require.NoError(t, err)
	setHold(true)
	_, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 3, PromotionWithdrawalReviewRequest{ReviewNote: "paid"})
	require.ErrorIs(t, err, model.ErrUserRefundHeld)
	withdrawal, err = GetAdminPromotionWithdrawal(withdrawal.Id)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusProcessing, withdrawal.Status)
	require.Len(t, withdrawal.Operations, 3)

	require.NoError(t, model.DB.Where("id = ?", ledger.Id).First(ledger).Error)
	assert.Equal(t, model.PromotionCommissionStatusWithdrawing, ledger.Status)
	var payoutCount int64
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id)).
		Count(&payoutCount).Error)
	assert.Zero(t, payoutCount)

	// A persisted paid journal is proof that the reserved commission already
	// left the platform. Confirming its lagging processing status is audit-only
	// and remains allowed while recovery keeps the user on refund hold.
	var item model.PromotionWithdrawalItem
	require.NoError(t, model.DB.Where("withdrawal_id = ?", withdrawal.Id).First(&item).Error)
	require.NoError(t, model.DB.Model(&model.PromotionCommissionLedger{}).Where("id = ?", ledger.Id).
		Updates(map[string]interface{}{"status": model.PromotionCommissionStatusWithdrawn, "withdrawn_at": common.GetTimestamp()}).Error)
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.CreatePromotionFundTransactionTx(tx, &model.PromotionFundTransaction{
			TransactionKey: fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id),
			Kind:           model.PromotionFundKindCommissionWithdrawalPaid,
			UserId:         withdrawal.UserId,
			SourceType:     "promotion_withdrawals",
			SourceId:       withdrawal.Id,
			SourceKey:      fmt.Sprintf("promotion_withdrawals:%d", withdrawal.Id),
			ActorType:      "admin",
			ActorId:        withdrawal.ReviewerId,
			ExternalRef:    withdrawal.TradeNo,
		}, []model.PromotionFundTransactionLeg{{
			Account: model.PromotionFundAccountCommissionReserved, Asset: model.PromotionFundAssetCash,
			Currency: withdrawal.Currency, Amount: -item.AmountCents,
			SourceType: "promotion_commission_ledgers", SourceId: item.LedgerId,
		}})
	}))
	withdrawal, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 3, PromotionWithdrawalReviewRequest{ReviewNote: "paid"})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusPaid, withdrawal.Status)
	require.Len(t, withdrawal.Operations, 4)
	require.NoError(t, model.DB.Model(&model.PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id)).
		Count(&payoutCount).Error)
	assert.Equal(t, int64(1), payoutCount)
}
