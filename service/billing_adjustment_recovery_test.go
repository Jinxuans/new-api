package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createWalletBillingAdjustmentFixture(t *testing.T, requestId string, kind string, fundingDelta int, fundingTarget int, tokenDelta int, tokenEnforceBalance bool) (*model.BillingAdjustmentJournal, *WalletFunding) {
	t.Helper()
	const userId = 901
	const tokenId = 901
	funding := &WalletFunding{userId: userId}
	relayInfo := &relaycommon.RelayInfo{
		RequestId: requestId,
		UserId:    userId,
		TokenId:   tokenId,
	}
	adjustment, err := createBillingAdjustment(
		relayInfo,
		funding,
		kind,
		kind+":wallet",
		fundingDelta,
		fundingTarget,
		fundingDelta != 0,
		tokenDelta,
		tokenDelta != 0,
		tokenEnforceBalance,
		false,
	)
	require.NoError(t, err)
	return adjustment, funding
}

func makeBillingAdjustmentDue(t *testing.T, operationKey string) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentJournal{}).
		Where("operation_key = ?", operationKey).
		Update("recover_after", common.GetTimestamp()).Error)
}

func TestUndispatchedBillingSessionReserveRecoversWithoutCharging(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 900)
	seedToken(t, tokenId, userId, "billing-undispatched-reserve", 900)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("used_quota", 100).Error)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:             "billing-undispatched-reserve",
		UserId:                userId,
		TokenId:               tokenId,
		FinalPreConsumedQuota: 100,
	}
	session := &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userId, consumed: 100},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	require.NoError(t, session.Reserve(150))
	require.Len(t, session.pendingDispatch, 1)
	operationKey := session.pendingDispatch[0].OperationKey
	makeBillingAdjustmentDue(t, operationKey)

	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Canceled)
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 900, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 100, getTokenUsedQuota(t, tokenId))

	row, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCanceled, row.Status)
	assert.False(t, row.DispatchConfirmed)
}

func TestAppliedUndispatchedWalletTopUpRecoversExactDeltaAfterCrash(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 900)
	seedToken(t, tokenId, userId, "billing-applied-undispatched-top-up", 900)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("used_quota", 100).Error)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:             "billing-applied-undispatched-top-up",
		UserId:                userId,
		TokenId:               tokenId,
		FinalPreConsumedQuota: 100,
	}
	session := &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userId, consumed: 100},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	require.NoError(t, session.Reserve(150))
	require.Len(t, session.pendingDispatch, 1)
	topUp := session.pendingDispatch[0]
	require.NoError(t, session.applyReservationLocked(topUp))
	assert.Equal(t, 850, getUserQuota(t, userId))
	assert.Equal(t, 850, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 150, getTokenUsedQuota(t, tokenId))

	// Simulate process loss after both provisional mutations committed but
	// before the final dispatch-confirmation batch was written.
	makeBillingAdjustmentDue(t, topUp.OperationKey)
	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Canceled)
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 900, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 100, getTokenUsedQuota(t, tokenId))

	stored, err := model.GetBillingAdjustment(topUp.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCanceled, stored.Status)
	assert.False(t, stored.DispatchConfirmed)

	var refunds []model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where(
		"request_id = ? AND kind = ?",
		relayInfo.RequestId,
		model.BillingAdjustmentKindRefund,
	).Find(&refunds).Error)
	require.Len(t, refunds, 1)
	assert.Equal(t, int64(-50), refunds[0].FundingDelta)
	assert.Equal(t, int64(-50), refunds[0].TokenDelta)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, refunds[0].Status)
}

func TestBillingSessionRefundsAppliedTopUpWhenLaterProvisionalFails(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-provisional-partial-failure", 1_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RequestId: "billing-provisional-partial-failure",
		UserId:    userId,
		TokenId:   tokenId,
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding:   &WalletFunding{userId: userId},
	}
	require.Nil(t, session.preConsume(ctx, 100))
	require.NoError(t, session.Reserve(150))
	require.NoError(t, session.Reserve(1_100))
	require.Error(t, session.ConfirmDispatch())

	// The first top-up was provisionally applied, while the second failed its
	// balance guard. Refunding the failed request must unwind both the applied
	// top-up and the initial reservation exactly once.
	session.Refund(ctx)
	assert.Equal(t, 1_000, getUserQuota(t, userId))
	assert.Equal(t, 1_000, getTokenRemainQuota(t, tokenId))
	assert.Zero(t, getTokenUsedQuota(t, tokenId))

	var reservations []model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where(
		"request_id = ? AND kind IN ?",
		relayInfo.RequestId,
		[]string{model.BillingAdjustmentKindInitialReserve, model.BillingAdjustmentKindReserve},
	).Find(&reservations).Error)
	require.Len(t, reservations, 3)
	for _, reservation := range reservations {
		assert.Equal(t, model.BillingAdjustmentStatusCanceled, reservation.Status)
		assert.False(t, reservation.DispatchConfirmed)
	}
}

func TestConfirmedBillingSessionReserveRecoversChargeAfterCrash(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 900)
	seedToken(t, tokenId, userId, "billing-confirmed-reserve", 900)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("used_quota", 100).Error)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:             "billing-confirmed-reserve",
		UserId:                userId,
		TokenId:               tokenId,
		FinalPreConsumedQuota: 100,
	}
	session := &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userId, consumed: 100},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	require.NoError(t, session.Reserve(150))
	require.Len(t, session.pendingDispatch, 1)
	operationKey := session.pendingDispatch[0].OperationKey

	// Persist the dispatch authorization, then simulate process loss before
	// either balance mutation begins.
	require.NoError(t, model.MarkBillingAdjustmentDispatchConfirmed(operationKey))
	makeBillingAdjustmentDue(t, operationKey)

	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Completed)
	assert.Equal(t, 850, getUserQuota(t, userId))
	assert.Equal(t, 850, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 150, getTokenUsedQuota(t, tokenId))

	row, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, row.Status)
	assert.True(t, row.DispatchConfirmed)
}

func TestPostDispatchInitialReservationRecoversChargeAfterCrash(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-post-dispatch-initial", 1_000)

	relayInfo := &relaycommon.RelayInfo{
		RequestId: "billing-post-dispatch-initial",
		UserId:    userId,
		TokenId:   tokenId,
	}
	adjustment, err := createBillingAdjustment(
		relayInfo,
		&WalletFunding{userId: userId},
		model.BillingAdjustmentKindInitialReserve,
		"initial:wallet",
		100,
		100,
		true,
		100,
		true,
		true,
		true,
	)
	require.NoError(t, err)
	assert.True(t, adjustment.DispatchRequired)
	assert.True(t, adjustment.DispatchConfirmed)
	assert.Equal(t, model.BillingAdjustmentStatusPending, adjustment.Status)
	makeBillingAdjustmentDue(t, adjustment.OperationKey)

	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Completed)
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 900, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 100, getTokenUsedQuota(t, tokenId))

	stored, err := model.GetBillingAdjustment(adjustment.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, stored.Status)
	assert.True(t, stored.DispatchConfirmed)
}

func TestLazyPostResponseBillingPersistsConfirmedInitialIntent(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-lazy-post-response", 1_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RequestId: "billing-lazy-post-response",
		UserId:    userId,
		TokenId:   tokenId,
	}
	relayInfo.UserSetting.BillingPreference = "wallet_only"

	require.NoError(t, SettleBilling(ctx, relayInfo, 100))
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 900, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 100, getTokenUsedQuota(t, tokenId))

	var initial model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where(
		"request_id = ? AND kind = ?",
		relayInfo.RequestId,
		model.BillingAdjustmentKindInitialReserve,
	).First(&initial).Error)
	assert.True(t, initial.DispatchRequired)
	assert.True(t, initial.DispatchConfirmed)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, initial.Status)
}

func TestLazyPostResponseBillingFailsClosedWhenConfirmedIntentConflicts(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-lazy-conflicting-intent", 1_000)

	relayInfo := &relaycommon.RelayInfo{
		RequestId: "billing-lazy-conflicting-intent",
		UserId:    userId,
		TokenId:   tokenId,
		TokenKey:  "billing-lazy-conflicting-intent",
	}
	relayInfo.UserSetting.BillingPreference = "wallet_only"
	funding := &WalletFunding{userId: userId}
	confirmed, err := createBillingAdjustment(
		relayInfo,
		funding,
		model.BillingAdjustmentKindInitialReserve,
		"initial:wallet",
		90,
		90,
		true,
		90,
		true,
		true,
		true,
	)
	require.NoError(t, err)
	require.NoError(t, applyBillingToken(confirmed))
	_, err = applyBillingFunding(confirmed, funding)
	require.NoError(t, err)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	err = SettleBilling(ctx, relayInfo, 100)
	require.Error(t, err)
	assert.Equal(t, 910, getUserQuota(t, userId))
	assert.Equal(t, 910, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 90, getTokenUsedQuota(t, tokenId))
}

func TestUndispatchedInitialBillingReservationRecoversFullCharge(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-undispatched-initial", 1_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RequestId: "billing-undispatched-initial",
		UserId:    userId,
		TokenId:   tokenId,
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding:   &WalletFunding{userId: userId},
	}
	require.Nil(t, session.preConsume(ctx, 100))
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 900, getTokenRemainQuota(t, tokenId))

	var initial model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where("request_id = ? AND kind = ?", relayInfo.RequestId, model.BillingAdjustmentKindInitialReserve).First(&initial).Error)
	assert.True(t, initial.DispatchRequired)
	assert.False(t, initial.DispatchConfirmed)
	assert.Equal(t, model.BillingAdjustmentStatusPending, initial.Status)
	makeBillingAdjustmentDue(t, initial.OperationKey)

	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Canceled)
	assert.Equal(t, 1_000, getUserQuota(t, userId))
	assert.Equal(t, 1_000, getTokenRemainQuota(t, tokenId))
	assert.Zero(t, getTokenUsedQuota(t, tokenId))

	restoredInitial, err := model.GetBillingAdjustment(initial.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCanceled, restoredInitial.Status)
	var refund model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where("request_id = ? AND kind = ?", relayInfo.RequestId, model.BillingAdjustmentKindRefund).First(&refund).Error)
	assert.Equal(t, int64(-100), refund.FundingDelta)
	assert.Equal(t, int64(-100), refund.TokenDelta)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, refund.Status)
}

func TestBillingSessionRefundBeforeDispatchClosesInitialReservation(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-refund-before-dispatch", 1_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RequestId: "billing-refund-before-dispatch",
		UserId:    userId,
		TokenId:   tokenId,
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding:   &WalletFunding{userId: userId},
	}
	require.Nil(t, session.preConsume(ctx, 100))
	session.Refund(ctx)

	assert.Equal(t, 1_000, getUserQuota(t, userId))
	assert.Equal(t, 1_000, getTokenRemainQuota(t, tokenId))
	assert.Zero(t, getTokenUsedQuota(t, tokenId))
	var initial model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where("request_id = ? AND kind = ?", relayInfo.RequestId, model.BillingAdjustmentKindInitialReserve).First(&initial).Error)
	assert.Equal(t, model.BillingAdjustmentStatusCanceled, initial.Status)
	assert.False(t, initial.DispatchConfirmed)
}

func TestUsageReserveRecoveryCompletesAuthoritativeRealtimeCharge(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 900)
	seedToken(t, tokenId, userId, "billing-usage-reserve", 900)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("used_quota", 100).Error)

	adjustment, _ := createWalletBillingAdjustmentFixture(
		t,
		"billing-usage-reserve",
		model.BillingAdjustmentKindUsageReserve,
		50,
		150,
		50,
		true,
	)
	makeBillingAdjustmentDue(t, adjustment.OperationKey)

	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Completed)
	assert.Equal(t, 850, getUserQuota(t, userId))
	assert.Equal(t, 850, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 150, getTokenUsedQuota(t, tokenId))

	row, err := model.GetBillingAdjustment(adjustment.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, row.Status)
	assert.False(t, row.DispatchRequired)
}

func TestBillingSessionSettlementReconcilesUnknownUsageReserveWithoutDoubleCharge(t *testing.T) {
	tests := []struct {
		name           string
		fundingApplied bool
	}{
		{name: "token committed before unknown result"},
		{name: "both sides committed before unknown result", fundingApplied: true},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			const userId, tokenId = 901, 901
			seedUser(t, userId, 900)
			seedToken(t, tokenId, userId, "billing-usage-reconcile", 900)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("used_quota", 100).Error)

			requestId := fmt.Sprintf("billing-usage-reconcile-%d", index)
			adjustment, funding := createWalletBillingAdjustmentFixture(
				t,
				requestId,
				model.BillingAdjustmentKindUsageReserve,
				50,
				150,
				50,
				true,
			)
			require.NoError(t, applyBillingToken(adjustment))
			if test.fundingApplied {
				_, err := applyBillingFunding(adjustment, funding)
				require.NoError(t, err)
			}

			relayInfo := &relaycommon.RelayInfo{
				RequestId:             requestId,
				UserId:                userId,
				TokenId:               tokenId,
				FinalPreConsumedQuota: 100,
			}
			funding.consumed = 100
			session := &BillingSession{
				relayInfo:        relayInfo,
				funding:          funding,
				preConsumedQuota: 100,
				tokenConsumed:    100,
				pendingUsage:     []*model.BillingAdjustmentJournal{adjustment},
			}

			require.NoError(t, session.Settle(150))
			assert.Equal(t, 150, session.GetPreConsumedQuota())
			assert.Equal(t, 150, funding.consumed)
			assert.Equal(t, 850, getUserQuota(t, userId))
			assert.Equal(t, 850, getTokenRemainQuota(t, tokenId))
			assert.Equal(t, 150, getTokenUsedQuota(t, tokenId))
			assert.Empty(t, session.pendingUsage)
		})
	}
}

func TestBillingAdjustmentRecoversTokenRollbackAfterInitialFundingFailure(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 0)
	seedToken(t, tokenId, userId, "billing-recovery-initial", 1_000)

	adjustment, funding := createWalletBillingAdjustmentFixture(
		t,
		"billing-recovery-initial",
		model.BillingAdjustmentKindInitialReserve,
		100,
		100,
		100,
		true,
	)
	require.NoError(t, applyBillingToken(adjustment))
	require.ErrorIs(t, model.ApplyWalletBillingAdjustment(adjustment.OperationKey), model.ErrBillingAdjustmentFundingInsufficient)

	require.NoError(t, model.DB.Exec(`CREATE TRIGGER fail_billing_token_credit
		BEFORE UPDATE OF remain_quota ON tokens WHEN NEW.remain_quota > OLD.remain_quota
		BEGIN SELECT RAISE(ABORT, 'forced token rollback failure'); END`).Error)
	t.Cleanup(func() { _ = model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_token_credit").Error })

	canceled, rollbackErr := cancelBillingReservation(adjustment, model.ErrBillingAdjustmentFundingInsufficient)
	assert.True(t, canceled)
	require.Error(t, rollbackErr)
	assert.Equal(t, 900, getTokenRemainQuota(t, tokenId))
	assert.Zero(t, getUserQuota(t, userId))

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_token_credit").Error)
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentJournal{}).
		Where("status = ?", model.BillingAdjustmentStatusPending).
		Update("recover_after", common.GetTimestamp()).Error)
	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Completed)
	assert.Equal(t, 1_000, getTokenRemainQuota(t, tokenId))
	assert.Zero(t, getTokenUsedQuota(t, tokenId))
	assert.Zero(t, getUserQuota(t, userId))

	row, err := model.GetBillingAdjustment(adjustment.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCanceled, row.Status)
	assert.Zero(t, funding.consumed)
}

func TestBillingAdjustmentRecoversSettleAfterFundingCommitted(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "billing-recovery-settle", 500)

	adjustment, _ := createWalletBillingAdjustmentFixture(
		t,
		"billing-recovery-settle",
		model.BillingAdjustmentKindSettle,
		100,
		200,
		100,
		false,
	)
	require.NoError(t, model.ApplyWalletBillingAdjustment(adjustment.OperationKey))
	require.NoError(t, model.DB.Exec(`CREATE TRIGGER fail_billing_token_settle
		BEFORE UPDATE OF remain_quota ON tokens
		BEGIN SELECT RAISE(ABORT, 'forced token settle failure'); END`).Error)
	t.Cleanup(func() { _ = model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_token_settle").Error })

	tokenErr := applyBillingToken(adjustment)
	require.Error(t, tokenErr)
	require.NoError(t, model.RecordBillingAdjustmentError(adjustment.OperationKey, tokenErr))
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 500, getTokenRemainQuota(t, tokenId))

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_token_settle").Error)
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentJournal{}).
		Where("operation_key = ?", adjustment.OperationKey).
		Update("recover_after", common.GetTimestamp()).Error)
	_, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 400, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, 100, getTokenUsedQuota(t, tokenId))

	// Whole-operation replay after an unknown commit result is a no-op.
	require.NoError(t, model.ApplyWalletBillingAdjustment(adjustment.OperationKey))
	require.NoError(t, model.ApplyTokenBillingAdjustment(adjustment.OperationKey))
	assert.Equal(t, 900, getUserQuota(t, userId))
	assert.Equal(t, 400, getTokenRemainQuota(t, tokenId))
}

func TestBillingAdjustmentRefundRecoveryNeverDoubleCreditsWallet(t *testing.T) {
	truncate(t)
	const userId, tokenId = 901, 901
	seedUser(t, userId, 900)
	seedToken(t, tokenId, userId, "billing-recovery-refund", 400)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("used_quota", 100).Error)

	adjustment, _ := createWalletBillingAdjustmentFixture(
		t,
		"billing-recovery-refund",
		model.BillingAdjustmentKindRefund,
		-100,
		0,
		-100,
		false,
	)
	require.NoError(t, model.ApplyWalletBillingAdjustment(adjustment.OperationKey))
	require.NoError(t, model.DB.Exec(`CREATE TRIGGER fail_billing_token_refund
		BEFORE UPDATE OF remain_quota ON tokens
		BEGIN SELECT RAISE(ABORT, 'forced token refund failure'); END`).Error)
	t.Cleanup(func() { _ = model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_token_refund").Error })

	tokenErr := applyBillingToken(adjustment)
	require.Error(t, tokenErr)
	require.NoError(t, model.RecordBillingAdjustmentError(adjustment.OperationKey, tokenErr))
	assert.Equal(t, 1_000, getUserQuota(t, userId))

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_token_refund").Error)
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentJournal{}).
		Where("operation_key = ?", adjustment.OperationKey).
		Update("recover_after", common.GetTimestamp()).Error)
	_, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	_, err = RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	require.NoError(t, model.ApplyWalletBillingAdjustment(adjustment.OperationKey))

	assert.Equal(t, 1_000, getUserQuota(t, userId))
	assert.Equal(t, 500, getTokenRemainQuota(t, tokenId))
	assert.Zero(t, getTokenUsedQuota(t, tokenId))
}

func TestTaskBillingProjectionRecoversEverySideAndDeduplicatesLog(t *testing.T) {
	truncate(t)
	const userId, tokenId, channelId = 902, 902, 902
	const preConsumed, targetQuota = 100, 150
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "task-projection-recovery", 900)
	seedChannel(t, channelId)
	seedChargedAccounting(t, userId, channelId, tokenId, preConsumed, 1)
	task := makeTask(userId, channelId, preConsumed, tokenId, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	other := map[string]any{"task_id": task.TaskID, "pre_consumed_quota": preConsumed, "actual_quota": targetQuota}
	applied, err := model.ApplyTaskQuotaTransitionWithProjection(task.ID, preConsumed, targetQuota, model.TaskBillingProjectionInput{
		ModelName: taskModelName(task),
		Group:     task.Group,
		Content:   "projection recovery",
		Other:     common.MapToJsonStr(other),
	})
	require.NoError(t, err)
	assert.True(t, applied)
	operationKey := model.TaskBillingAdjustmentOperationKey(task.ID, preConsumed, targetQuota)

	// Simulate a process dying after token projection and after the log database
	// committed, but before usage/log applied markers were stored.
	row, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	require.NoError(t, model.ApplyTokenBillingAdjustment(operationKey))
	claimed, err := model.ClaimBillingAdjustmentLogProjection(operationKey, "crashed-worker", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.EnsureTaskBillingProjectionLog(row))
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentJournal{}).
		Where("operation_key = ?", operationKey).
		Updates(map[string]any{"log_claimed_until": 0, "recover_after": common.GetTimestamp()}).Error)

	result, err := RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Completed)
	assert.Equal(t, 950, getUserQuota(t, userId))
	assert.Equal(t, 850, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, targetQuota, getTokenUsedQuota(t, tokenId))
	usedQuota, requestCount := getUserUsageAccounting(t, userId)
	assert.Equal(t, targetQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(targetQuota), getChannelUsedQuota(t, channelId))
	assert.Equal(t, int64(1), countLogs(t))

	// Replaying the task event, including the commit-unknown log step, changes
	// neither accounting nor log cardinality.
	require.NoError(t, applyTaskBillingProjections(operationKey))
	_, err = RecoverPendingBillingAdjustments(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 950, getUserQuota(t, userId))
	assert.Equal(t, targetQuota, getTokenUsedQuota(t, tokenId))
	assert.Equal(t, int64(1), countLogs(t))

	finalRow, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	require.NotNil(t, finalRow)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, finalRow.Status)
	assert.True(t, finalRow.FundingApplied)
	assert.True(t, finalRow.TokenApplied)
	assert.True(t, finalRow.UsageApplied)
	assert.True(t, finalRow.LogApplied)
	assert.Empty(t, finalRow.LastError)
}

func TestTaskBillingProjectionSkipsTokenDeletedBeforeTransition(t *testing.T) {
	truncate(t)
	const userId, tokenId, channelId = 903, 903, 903
	const preConsumed, targetQuota = 100, 150
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "task-projection-deleted-before", 900)
	seedChannel(t, channelId)
	seedChargedAccounting(t, userId, channelId, tokenId, preConsumed, 1)
	require.NoError(t, model.DB.Delete(&model.Token{}, tokenId).Error)
	task := makeTask(userId, channelId, preConsumed, tokenId, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	applied, err := model.ApplyTaskQuotaTransitionWithProjection(task.ID, preConsumed, targetQuota, model.TaskBillingProjectionInput{
		ModelName: taskModelName(task),
		Group:     task.Group,
		Content:   "deleted token projection",
	})
	require.NoError(t, err)
	assert.True(t, applied)
	operationKey := model.TaskBillingAdjustmentOperationKey(task.ID, preConsumed, targetQuota)

	row, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.False(t, row.TokenRequired)
	assert.True(t, row.TokenApplied)
	require.NoError(t, applyTaskBillingProjections(operationKey))

	finalRow, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, finalRow.Status)
	var deletedToken model.Token
	require.NoError(t, model.DB.Unscoped().First(&deletedToken, tokenId).Error)
	assert.Equal(t, 900, deletedToken.RemainQuota)
	assert.Equal(t, preConsumed, deletedToken.UsedQuota)
}

func TestTaskBillingProjectionCompletesWhenTokenDeletedAfterJournalCreation(t *testing.T) {
	truncate(t)
	const userId, tokenId, channelId = 904, 904, 904
	const preConsumed, targetQuota = 100, 150
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "task-projection-deleted-after", 900)
	seedChannel(t, channelId)
	seedChargedAccounting(t, userId, channelId, tokenId, preConsumed, 1)
	task := makeTask(userId, channelId, preConsumed, tokenId, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	applied, err := model.ApplyTaskQuotaTransitionWithProjection(task.ID, preConsumed, targetQuota, model.TaskBillingProjectionInput{
		ModelName: taskModelName(task),
		Group:     task.Group,
		Content:   "deleted token race projection",
	})
	require.NoError(t, err)
	assert.True(t, applied)
	operationKey := model.TaskBillingAdjustmentOperationKey(task.ID, preConsumed, targetQuota)
	row, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.True(t, row.TokenRequired)
	assert.False(t, row.TokenApplied)

	require.NoError(t, model.DB.Delete(&model.Token{}, tokenId).Error)
	require.NoError(t, applyTaskBillingProjections(operationKey))

	finalRow, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, finalRow.Status)
	assert.True(t, finalRow.TokenApplied)
}

func TestTaskBillingProjectionMaintainsUnlimitedTokenAccounting(t *testing.T) {
	truncate(t)
	const userId, tokenId, channelId = 905, 905, 905
	const preConsumed, targetQuota = 100, 150
	seedUser(t, userId, 1_000)
	seedToken(t, tokenId, userId, "task-projection-unlimited", 900)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenId).Update("unlimited_quota", true).Error)
	seedChannel(t, channelId)
	seedChargedAccounting(t, userId, channelId, tokenId, preConsumed, 1)
	task := makeTask(userId, channelId, preConsumed, tokenId, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	applied, err := model.ApplyTaskQuotaTransitionWithProjection(task.ID, preConsumed, targetQuota, model.TaskBillingProjectionInput{
		ModelName: taskModelName(task),
		Group:     task.Group,
		Content:   "unlimited token projection",
	})
	require.NoError(t, err)
	assert.True(t, applied)
	operationKey := model.TaskBillingAdjustmentOperationKey(task.ID, preConsumed, targetQuota)
	row, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.True(t, row.TokenRequired)
	assert.True(t, row.TokenUnlimited)
	assert.False(t, row.TokenApplied)

	require.NoError(t, applyTaskBillingProjections(operationKey))
	assert.Equal(t, 850, getTokenRemainQuota(t, tokenId))
	assert.Equal(t, targetQuota, getTokenUsedQuota(t, tokenId))
	finalRow, err := model.GetBillingAdjustment(operationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingAdjustmentStatusCompleted, finalRow.Status)
	assert.True(t, finalRow.TokenApplied)
}
