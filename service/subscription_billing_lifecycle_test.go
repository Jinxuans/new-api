package service

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryableBillingRefundFunding struct {
	refundErr   error
	refundCalls int
}

func (*retryableBillingRefundFunding) Source() string       { return BillingSourceSubscription }
func (*retryableBillingRefundFunding) PreConsume(int) error { return nil }
func (*retryableBillingRefundFunding) Settle(int) error     { return nil }
func (f *retryableBillingRefundFunding) Refund() error {
	f.refundCalls++
	return f.refundErr
}

func createSubscriptionBillingLifecycleFixture(t *testing.T, userId int) *model.UserSubscription {
	t.Helper()
	seedUser(t, userId, 0)
	plan := &model.SubscriptionPlan{
		Title:            "Subscription billing lifecycle",
		DurationUnit:     model.SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      1_000,
		QuotaResetPeriod: model.SubscriptionResetNever,
		Enabled:          true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	now := time.Now().Unix()
	sub := &model.UserSubscription{
		UserId: userId, PlanId: plan.Id,
		AmountTotal: 1_000, StartTime: now - 60, EndTime: now + 3600,
		Status: "active", Source: "test",
	}
	require.NoError(t, model.DB.Create(sub).Error)
	return sub
}

func newSubscriptionBillingLifecycleSession(t *testing.T, userId int, requestId string, preConsumed int) (*BillingSession, *relaycommon.RelayInfo) {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RequestId:       requestId,
		UserId:          userId,
		OriginModelName: "test-model",
		UsingGroup:      "default",
		IsPlayground:    true,
		UserSetting: dto.UserSetting{
			BillingPreference: "subscription_only",
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, preConsumed, relayInfo))
	session, ok := relayInfo.Billing.(*BillingSession)
	require.True(t, ok)
	return session, relayInfo
}

func TestBillingSessionPersistsExtendedSubscriptionReservationInOneLifecycle(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userId = 811
	sub := createSubscriptionBillingLifecycleFixture(t, userId)

	session, relayInfo := newSubscriptionBillingLifecycleSession(t, userId, "billing-lifecycle-settle", 20)
	require.NoError(t, session.Reserve(50))
	require.NoError(t, session.ConfirmDispatch())
	assert.Equal(t, 50, session.GetPreConsumedQuota())
	assert.Equal(t, int64(50), relayInfo.SubscriptionPreConsumed)

	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", "billing-lifecycle-settle").First(&record).Error)
	assert.Equal(t, int64(50), record.PreConsumed)
	assert.Equal(t, model.SubscriptionPreConsumeStatusReserved, record.Status)

	require.NoError(t, session.Settle(30))
	require.NoError(t, model.DB.Where("request_id = ?", "billing-lifecycle-settle").First(&record).Error)
	assert.Equal(t, model.SubscriptionPreConsumeStatusSettled, record.Status)
	assert.Equal(t, int64(30), record.FinalConsumed)
	assert.Equal(t, int64(-20), relayInfo.SubscriptionPostDelta)
	var storedSub model.UserSubscription
	require.NoError(t, model.DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(30), storedSub.AmountUsed)

	// Even a zero-delta settlement must persist the request lifecycle.
	zeroDeltaSession, _ := newSubscriptionBillingLifecycleSession(t, userId, "billing-lifecycle-zero-delta", 20)
	require.NoError(t, zeroDeltaSession.Settle(20))
	record = model.SubscriptionPreConsumeRecord{}
	require.NoError(t, model.DB.Where("request_id = ?", "billing-lifecycle-zero-delta").First(&record).Error)
	assert.Equal(t, model.SubscriptionPreConsumeStatusSettled, record.Status)
	assert.Equal(t, int64(20), record.FinalConsumed)

	// An extended reservation is part of the same durable record, so one
	// funding refund returns the whole amount without a second quota mutation.
	refundSession, _ := newSubscriptionBillingLifecycleSession(t, userId, "billing-lifecycle-refund", 20)
	require.NoError(t, refundSession.Reserve(50))
	require.NoError(t, refundSession.ConfirmDispatch())
	refundFunding, ok := refundSession.funding.(*SubscriptionFunding)
	require.True(t, ok)
	require.NoError(t, refundFunding.Refund())
	record = model.SubscriptionPreConsumeRecord{}
	require.NoError(t, model.DB.Where("request_id = ?", "billing-lifecycle-refund").First(&record).Error)
	assert.Equal(t, model.SubscriptionPreConsumeStatusRefunded, record.Status)
	assert.Equal(t, int64(50), record.PreConsumed)
	require.NoError(t, model.DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(50), storedSub.AmountUsed)
}

func TestUndispatchedSubscriptionTopUpRefundsWholeRequestLifecycle(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userId = 812
	sub := createSubscriptionBillingLifecycleFixture(t, userId)
	session, relayInfo := newSubscriptionBillingLifecycleSession(t, userId, "billing-lifecycle-undispatched-top-up", 20)

	require.NoError(t, session.Reserve(50))
	require.Len(t, session.pendingDispatch, 1)
	require.NoError(t, session.applyReservationLocked(session.pendingDispatch[0]))

	var storedSub model.UserSubscription
	require.NoError(t, model.DB.First(&storedSub, sub.Id).Error)
	assert.Equal(t, int64(50), storedSub.AmountUsed)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	session.Refund(ctx)

	require.NoError(t, model.DB.First(&storedSub, sub.Id).Error)
	assert.Zero(t, storedSub.AmountUsed)
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", relayInfo.RequestId).First(&record).Error)
	assert.Equal(t, model.SubscriptionPreConsumeStatusRefunded, record.Status)
	assert.Equal(t, int64(50), record.PreConsumed)

	var refunds []model.BillingAdjustmentJournal
	require.NoError(t, model.DB.Where(
		"request_id = ? AND kind = ?",
		relayInfo.RequestId,
		model.BillingAdjustmentKindRefund,
	).Find(&refunds).Error)
	require.Len(t, refunds, 2)
	var fundingDelta int64
	for _, refund := range refunds {
		fundingDelta += refund.FundingDelta
		assert.Equal(t, model.BillingAdjustmentStatusCompleted, refund.Status)
	}
	assert.Equal(t, int64(-50), fundingDelta)
}

func TestBillingSessionRefundFailureRemainsSynchronouslyRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	funding := &retryableBillingRefundFunding{refundErr: errors.New("temporary refund failure")}
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{UserId: 812, IsPlayground: true},
		funding:          funding,
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	session.Refund(ctx)
	assert.Equal(t, 1, funding.refundCalls)
	assert.False(t, session.refunding)
	assert.False(t, session.refunded)

	funding.refundErr = nil
	session.Refund(ctx)
	assert.Equal(t, 2, funding.refundCalls)
	assert.False(t, session.refunding)
	assert.True(t, session.refunded)
}
