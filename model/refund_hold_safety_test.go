package model

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refundHoldCacheDeleteFailureHook struct {
	cacheKey string
	fail     atomic.Bool
}

func (hook *refundHoldCacheDeleteFailureHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if hook.fail.Load() && cmd.Name() == "del" {
		args := cmd.Args()
		if len(args) > 1 && fmt.Sprint(args[1]) == hook.cacheKey {
			return ctx, errors.New("forced user cache invalidation failure")
		}
	}
	return ctx, nil
}

func (hook *refundHoldCacheDeleteFailureHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *refundHoldCacheDeleteFailureHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook *refundHoldCacheDeleteFailureHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestRefundHoldNullAllowsDebitDuringRollingMigration(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Exec("UPDATE users SET refund_hold = NULL WHERE id = ?", user.Id).Error)

	reserved, err := TryReserveUserQuota(user.Id, 25)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 75, getUserQuotaFromDB(t, user.Id))

	require.NoError(t, DB.Exec("UPDATE users SET refund_hold = NULL WHERE id = ?", user.Id).Error)
	require.NoError(t, persistUserQuotaDelta(user.Id, -10))
	assert.Equal(t, 65, getUserQuotaFromDB(t, user.Id))
}

func TestBackfillNullUserRefundHolds(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Exec("UPDATE users SET refund_hold = NULL WHERE id = ?", user.Id).Error)

	require.NoError(t, BackfillNullUserRefundHolds())

	var nullCount int64
	require.NoError(t, DB.Model(&User{}).Where("id = ? AND refund_hold IS NULL", user.Id).Count(&nullCount).Error)
	assert.Zero(t, nullCount)
	var stored User
	require.NoError(t, DB.Select("id", "refund_hold").First(&stored, user.Id).Error)
	assert.False(t, stored.RefundHold)
}

func TestRefundHoldFenceLeaseIsFiniteAndRenewable(t *testing.T) {
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	const userID = 7101

	require.NoError(t, SetUserRefundHoldFence(userID))
	ttl, err := common.RDB.TTL(t.Context(), userRefundHoldKey(userID)).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl)
	assert.Greater(t, ttl, time.Duration(userCacheTTLSeconds())*time.Second)

	server.FastForward(ttl / 2)
	require.NoError(t, RenewUserRefundHoldFence(userID))
	renewedTTL, err := common.RDB.TTL(t.Context(), userRefundHoldKey(userID)).Result()
	require.NoError(t, err)
	assert.Greater(t, renewedTTL, ttl/2)

	server.FastForward(time.Duration(userRefundHoldFenceTTLSeconds()+1) * time.Second)
	assert.False(t, server.Exists(userRefundHoldKey(userID)))

	localUserRefundHoldFenceLock.Lock()
	for token := range localUserRefundHoldFenceLeases[userID] {
		localUserRefundHoldFenceLeases[userID][token] = time.Now().Add(-time.Second)
	}
	localUserRefundHoldFenceLock.Unlock()
	assert.False(t, isLocalUserRefundHeld(userID))
}

func TestRefundHoldFenceOwnerCannotClearConcurrentFence(t *testing.T) {
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	const userID = 7103

	firstToken, err := setUserRefundHoldFenceLease(userID)
	require.NoError(t, err)
	secondToken, err := setUserRefundHoldFenceLease(userID)
	require.NoError(t, err)
	require.NotEqual(t, firstToken, secondToken)

	cleared, err := clearUserRefundHoldFenceLease(userID, firstToken)
	require.NoError(t, err)
	assert.True(t, cleared)
	members, err := common.RDB.ZRange(t.Context(), userRefundHoldKey(userID), 0, -1).Result()
	require.NoError(t, err)
	assert.Equal(t, []string{secondToken}, members)
	assert.True(t, isLocalUserRefundHeld(userID))
	assert.True(t, server.Exists(userRefundHoldKey(userID)))

	cleared, err = clearUserRefundHoldFenceLease(userID, secondToken)
	require.NoError(t, err)
	assert.True(t, cleared)
	assert.False(t, server.Exists(userRefundHoldKey(userID)))
	assert.False(t, isLocalUserRefundHeld(userID))
}

func TestRefundHoldFenceScopeRenewsOneLeasePerUser(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	scope := newRefundHoldFenceScope()

	require.NoError(t, scope.Ensure(user.Id))
	firstToken := scope[user.Id]
	require.NoError(t, scope.Ensure(user.Id))
	assert.Equal(t, firstToken, scope[user.Id])
	assert.Equal(t, int64(1), common.RDB.ZCard(t.Context(), userRefundHoldKey(user.Id)).Val())

	require.NoError(t, scope.Reconcile())
	assert.False(t, isLocalUserRefundHeld(user.Id))
}

func TestRefundHoldFenceScopesReleaseOnlyTheirOwnLease(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	first := newRefundHoldFenceScope()
	second := newRefundHoldFenceScope()
	require.NoError(t, first.Ensure(user.Id))
	require.NoError(t, second.Ensure(user.Id))

	require.NoError(t, first.Reconcile())
	assert.True(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.True(t, isLocalUserRefundHeld(user.Id))
	assert.Equal(t, int64(1), common.RDB.ZCard(t.Context(), userRefundHoldKey(user.Id)).Val())

	require.NoError(t, second.Reconcile())
	assert.False(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.False(t, isLocalUserRefundHeld(user.Id))
}

func TestRefundHoldFenceScopeClearsMissingRedisLeaseWithoutClearingConcurrentLocalLease(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	first := newRefundHoldFenceScope()
	second := newRefundHoldFenceScope()
	require.NoError(t, first.Ensure(user.Id))
	require.NoError(t, second.Ensure(user.Id))
	require.NoError(t, common.RDB.ZRem(t.Context(), userRefundHoldKey(user.Id), first[user.Id]).Err())

	require.NoError(t, first.Reconcile())
	assert.True(t, isLocalUserRefundHeld(user.Id))
	assert.Equal(t, int64(1), common.RDB.ZCard(t.Context(), userRefundHoldKey(user.Id)).Val())

	require.NoError(t, second.Reconcile())
	assert.False(t, isLocalUserRefundHeld(user.Id))
}

func TestUnownedRefundHoldReconciliationPreservesInflightLease(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	scope := newRefundHoldFenceScope()
	require.NoError(t, scope.Ensure(user.Id))

	err := ReconcileUserRefundHoldFenceState(user.Id)
	require.ErrorIs(t, err, ErrUserRefundHeld)
	assert.True(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.True(t, isLocalUserRefundHeld(user.Id))

	require.NoError(t, scope.Reconcile())
	assert.False(t, server.Exists(userRefundHoldKey(user.Id)))
}

func TestClearRefundHoldFenceKeepsFenceUntilCacheInvalidationSucceeds(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, SetUserRefundHoldFence(user.Id))
	hook := &refundHoldCacheDeleteFailureHook{cacheKey: getUserCacheKey(user.Id)}
	hook.fail.Store(true)
	common.RDB.AddHook(hook)

	err := ClearUserRefundHoldFence(user.Id)
	require.Error(t, err)
	assert.True(t, isLocalUserRefundHeld(user.Id))
	assert.True(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.True(t, server.Exists(getUserCacheKey(user.Id)))

	hook.fail.Store(false)
	require.NoError(t, ClearUserRefundHoldFence(user.Id))
	assert.False(t, server.Exists(getUserCacheKey(user.Id)))
	assert.False(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.False(t, isLocalUserRefundHeld(user.Id))
}

func TestReconcileRefundHoldFenceRecoversDurableState(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", true).Error)

	require.NoError(t, ReconcileUserRefundHoldFenceState(user.Id))
	assert.True(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.True(t, isLocalUserRefundHeld(user.Id))
	require.ErrorIs(t, ClearUserRefundHoldFence(user.Id), ErrUserRefundHeld)
	assert.True(t, server.Exists(userRefundHoldKey(user.Id)))

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", false).Error)
	require.NoError(t, ReconcileUserRefundHoldFenceState(user.Id))
	assert.False(t, server.Exists(userRefundHoldKey(user.Id)))
	assert.False(t, isLocalUserRefundHeld(user.Id))
}

func TestUserCacheIncludesRefundRecoveryState(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	user.RefundDebtQuota = 45
	user.RefundHold = true
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"refund_debt_quota": user.RefundDebtQuota,
		"refund_hold":       user.RefundHold,
	}).Error)

	require.NoError(t, populateUserCache(user))
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, user.RefundDebtQuota, cached.RefundDebtQuota)
	assert.True(t, cached.RefundHold)
}

func TestRefundHoldFenceRejectsStaleUserCacheWrite(t *testing.T) {
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	const userID = 7102
	require.NoError(t, SetUserRefundHoldFence(userID))

	err := writeUserCache(&UserBase{
		Id: userID, Group: "default", Username: "stale", AuthVersion: 1,
	}, true)

	require.ErrorIs(t, err, ErrUserRefundHeld)
	assert.False(t, server.Exists(getUserCacheKey(userID)))
}

func TestGetUserCacheTreatsRefundFenceAsHeld(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))

	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.False(t, cached.RefundHold)
	require.NoError(t, SetUserRefundHoldFence(user.Id))

	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.True(t, cached.RefundHold)
}
