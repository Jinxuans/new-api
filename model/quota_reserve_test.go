package model

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createReserveTestUser(t *testing.T, quota int) User {
	t.Helper()
	user := User{
		Username:    "reserve-user-" + common.GetRandomString(6),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       quota,
		AffCode:     "reserve-aff-" + common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createReserveTestToken(t *testing.T, remainQuota int) Token {
	t.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, token.Insert())
	return token
}

func getUserQuotaFromDB(t *testing.T, id int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").First(&user, id).Error)
	return user.Quota
}

func getTokenFromDB(t *testing.T, id int) Token {
	t.Helper()
	var token Token
	require.NoError(t, DB.First(&token, id).Error)
	return token
}

func resetBatchUpdateTestState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}
	localUserRefundHoldFenceLock.Lock()
	localUserRefundHoldFenceLeases = make(map[int]map[string]time.Time)
	localUserRefundHoldFenceLock.Unlock()
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		for i := 0; i < BatchUpdateTypeCount; i++ {
			batchUpdateLocks[i].Lock()
			batchUpdateStores[i] = make(map[int]int)
			batchUpdateLocks[i].Unlock()
		}
		localUserRefundHoldFenceLock.Lock()
		localUserRefundHoldFenceLeases = make(map[int]map[string]time.Time)
		localUserRefundHoldFenceLock.Unlock()
	})
}

func TestTryReserveQuotaWithoutRedis(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	user := createReserveTestUser(t, 100)
	reserved, err := TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 41)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	token := createReserveTestToken(t, 80)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 55, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 55, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestRedisBatchReservePersistsUserWalletImmediately(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 10)
	reserved, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id), "a cross-instance wallet debit must not wait for the local batch updater")

	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved)
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, cachedUser.Quota)

	token := createReserveTestToken(t, 9)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	batchUpdate()
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 2, reloadedToken.RemainQuota)
	assert.Equal(t, 7, reloadedToken.UsedQuota)
}

func TestLocalBatchModePersistsUserWalletImmediately(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := createReserveTestUser(t, 20)
	reserved, err := TryReserveUserQuota(user.Id, 7)
	require.NoError(t, err)
	assert.True(t, reserved)
	require.NoError(t, IncreaseUserQuota(user.Id, 5, false))
	require.NoError(t, DecreaseUserQuota(user.Id, 3, false))

	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))

	batchUpdate()
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id), "a later batch pass must not change the wallet again")
}

func TestRedisCachedDurableRefundHoldBlocksReservationWithoutFence(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 20)
	user.RefundHold = true
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", true).Error)
	require.NoError(t, populateUserCache(user))
	assert.False(t, server.Exists(userRefundHoldKey(user.Id)))

	result, err := cacheTryReserveUserQuota(user.Id, 5)
	require.NoError(t, err)
	assert.Equal(t, cacheQuotaHeld, result)
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.True(t, cached.RefundHold)
	assert.Equal(t, 20, cached.Quota)
}

func TestBatchUpdateAccumulatorSaturatesOverflow(t *testing.T) {
	resetBatchUpdateTestState(t)

	addNewRecord(BatchUpdateTypeUsedQuota, 1, math.MaxInt)
	addNewRecord(BatchUpdateTypeUsedQuota, 1, 1)
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	assert.Equal(t, math.MaxInt, batchUpdateStores[BatchUpdateTypeUsedQuota][1])
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()

	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	batchUpdateStores[BatchUpdateTypeUsedQuota] = make(map[int]int)
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()
	addNewRecord(BatchUpdateTypeUsedQuota, 1, math.MinInt)
	addNewRecord(BatchUpdateTypeUsedQuota, 1, -1)
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	assert.Equal(t, math.MinInt, batchUpdateStores[BatchUpdateTypeUsedQuota][1])
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()
}

func TestReserveFallsBackToDatabaseWhenRedisIsUnavailable(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 20)
	require.NoError(t, populateUserCache(user))
	server.Close()

	// Redis 故障时降级为数据库条件更新：服务保持可用且不会超扣。
	reserved, err := TryReserveUserQuota(user.Id, 5)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 16)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))
}

func TestSynchronousReserveCompensatesCacheWhenPersistenceFails(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DB.Delete(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 6)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cached, cacheErr := cacheGetUserBase(user.Id)
	require.NoError(t, cacheErr)
	assert.Equal(t, 10, cached.Quota)

	token := createReserveTestToken(t, 12)
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	require.NoError(t, DB.Delete(&token).Error)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cachedToken, cacheErr := cacheGetTokenByKey(token.Key)
	require.NoError(t, cacheErr)
	assert.Equal(t, 12, cachedToken.RemainQuota)
	assert.Zero(t, cachedToken.UsedQuota)
}

func TestTryReserveUserQuotaRejectsCacheBalanceAboveDatabase(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 40)
	stale := *user.ToBaseUser()
	stale.Quota = 100
	require.NoError(t, writeUserCache(&stale, true))

	reserved, err := TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))
	assert.False(t, server.Exists(getUserCacheKey(user.Id)), "the stale cache must be discarded after the database CAS rejects it")

	reserved, err = TryReserveUserQuota(user.Id, 40)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 0, getUserQuotaFromDB(t, user.Id))
}

func TestDelayedDatabaseCreditCacheWorkCannotRestoreRefundedQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 0)
	require.NoError(t, populateUserCache(user))

	// The credit commits, but its cache work is delayed. A refund then restores
	// the database balance and publishes that state through cache invalidation.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).
		Update("quota", gorm.Expr("quota + ?", 100)).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).
		Update("quota", gorm.Expr("quota - ?", 100)).Error)
	require.NoError(t, invalidateUserCache(user.Id))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Zero(t, cached.Quota)
	assert.True(t, server.Exists(getUserCacheKey(user.Id)))

	// The old post-credit callback can only invalidate this newer snapshot; it
	// must never add the already-refunded credit to it.
	invalidateUserQuotaCacheAfterDBWrite(user.Id, "delayed topup")
	assert.False(t, server.Exists(getUserCacheKey(user.Id)))
	reserved, err := TryReserveUserQuota(user.Id, 1)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 0, getUserQuotaFromDB(t, user.Id))
}

func TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	token := createReserveTestToken(t, 100)
	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	stale := *loaded

	result, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -70)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)

	// 已存在的哈希只刷新 TTL：数据库快照不得覆盖已被原子预扣的余额。
	code, err := cacheInitToken(stale)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cached.RemainQuota)

	// 变更期间：fence 删除缓存并拦截并发读者手中的过期快照。
	require.NoError(t, invalidateTokenCacheForMutation(token.Key))
	code, err = cacheInitToken(stale)
	require.NoError(t, err)
	assert.Zero(t, code, "the pre-mutation snapshot must not be published while fenced")
	_, err = cacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	// fence 过期后可重新从数据库水合。
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	fresh, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, fresh.RemainQuota)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.RemainQuota)
}
