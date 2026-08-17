package model

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrUserRefundHeld = errors.New("user funding is suspended for refund recovery")

const durableUserRefundHoldFenceToken = "durable"

// A user may be touched by more than one refund transaction at the same time.
// Keep every in-flight lease instead of letting the newest transaction replace
// the previous one. The Redis side mirrors this map with a sorted set whose
// scores are lease expiry timestamps.
var localUserRefundHoldFenceLeases = map[int]map[string]time.Time{}
var localUserRefundHoldFenceLock sync.Mutex

type refundHoldFenceScope map[int]string

func newRefundHoldFenceScope() refundHoldFenceScope {
	return refundHoldFenceScope{}
}

// Ensure installs one stable lease per user for the lifetime of a refund
// operation. Repeated calls renew that lease instead of replacing it.
func (scope refundHoldFenceScope) Ensure(userId int) error {
	if token := scope[userId]; token != "" {
		return renewUserRefundHoldFenceLease(userId, token)
	}
	token, err := newUserRefundHoldFenceToken()
	if err != nil {
		return err
	}
	scope[userId] = token
	return renewUserRefundHoldFenceLease(userId, token)
}

// Reconcile publishes the committed database state, invalidates any stale
// cache snapshot, and releases only leases owned by this operation.
func (scope refundHoldFenceScope) Reconcile() error {
	var result error
	for userId, token := range scope {
		if err := reconcileOwnedUserRefundHoldFence(userId, token); err != nil {
			result = errors.Join(result, err)
			continue
		}
		delete(scope, userId)
	}
	return result
}

func reconcileOwnedUserRefundHoldFence(userId int, token string) error {
	// Keep an independent durable-state lease across cache invalidation. This
	// never replaces leases owned by concurrent operations.
	if err := RenewUserRefundHoldFence(userId); err != nil {
		return err
	}
	if common.RedisEnabled {
		if err := invalidateUserCache(userId); err != nil {
			wrapped := fmt.Errorf("invalidate user cache while reconciling refund hold for user %d: %w", userId, err)
			common.SysError(wrapped.Error())
			return wrapped
		}
	}

	var user User
	err := DB.Select("id", "refund_hold").Where("id = ?", userId).First(&user).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		wrapped := fmt.Errorf("verify reconciled refund hold for user %d: %w", userId, err)
		common.SysError(wrapped.Error())
		return wrapped
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || !user.RefundHold {
		if _, err := clearUserRefundHoldFenceLease(userId, durableUserRefundHoldFenceToken); err != nil {
			return err
		}
	}
	if _, err := clearUserRefundHoldFenceLease(userId, token); err != nil {
		return err
	}
	return nil
}

func userRefundHoldKey(userId int) string {
	return fmt.Sprintf("user_refund_hold:%d", userId)
}

// A pending refund fence must outlive every user-cache snapshot that may have
// been populated before the restrictive database transaction. The lease is
// nevertheless finite so a rolled-back transaction or crashed process cannot
// leave an account blocked forever.
func userRefundHoldFenceTTLSeconds() int {
	cacheTTL := userCacheTTLSeconds()
	extra := cacheTTL
	if extra < 60 {
		extra = 60
	}
	return cacheTTL + extra
}

func localUserRefundHoldActiveLocked(userId int, now time.Time) bool {
	leases := localUserRefundHoldFenceLeases[userId]
	for token, expiresAt := range leases {
		if now.Before(expiresAt) {
			continue
		}
		delete(leases, token)
	}
	if len(leases) > 0 {
		return true
	}
	delete(localUserRefundHoldFenceLeases, userId)
	return false
}

func setLocalUserRefundHoldFence(userId int, token string) {
	localUserRefundHoldFenceLock.Lock()
	defer localUserRefundHoldFenceLock.Unlock()
	leases := localUserRefundHoldFenceLeases[userId]
	if leases == nil {
		leases = map[string]time.Time{}
		localUserRefundHoldFenceLeases[userId] = leases
	}
	leases[token] = time.Now().Add(time.Duration(userRefundHoldFenceTTLSeconds()) * time.Second)
}

func clearLocalUserRefundHoldFence(userId int, token string) bool {
	localUserRefundHoldFenceLock.Lock()
	defer localUserRefundHoldFenceLock.Unlock()
	leases := localUserRefundHoldFenceLeases[userId]
	if _, exists := leases[token]; !exists {
		return false
	}
	delete(leases, token)
	if len(leases) == 0 {
		delete(localUserRefundHoldFenceLeases, userId)
	}
	return true
}

func isLocalUserRefundHeld(userId int) bool {
	localUserRefundHoldFenceLock.Lock()
	exists := localUserRefundHoldActiveLocked(userId, time.Now())
	localUserRefundHoldFenceLock.Unlock()
	return exists
}

func newUserRefundHoldFenceToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate refund hold fence token: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func setUserRefundHoldFenceLease(userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid user id")
	}
	token, err := newUserRefundHoldFenceToken()
	if err != nil {
		return "", err
	}
	return token, renewUserRefundHoldFenceLease(userId, token)
}

func renewUserRefundHoldFenceLease(userId int, token string) error {
	if userId <= 0 || token == "" {
		return errors.New("invalid refund hold fence lease")
	}
	setLocalUserRefundHoldFence(userId, token)
	if !common.RedisEnabled {
		return nil
	}
	if common.RDB == nil {
		return errors.New("redis refund hold fence is enabled but the client is not initialized")
	}
	const addLease = `
local expires_at = tonumber(ARGV[2]) + tonumber(ARGV[3])
redis.call('ZADD', KEYS[1], expires_at, ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1`
	ttlMilliseconds := int64(userRefundHoldFenceTTLSeconds()) * int64(time.Second/time.Millisecond)
	err := common.RDB.Eval(context.Background(), addLease, []string{userRefundHoldKey(userId)},
		token, time.Now().UnixMilli(), ttlMilliseconds).Err()
	if err != nil {
		wrapped := fmt.Errorf("renew refund hold fence for user %d: %w", userId, err)
		common.SysError(wrapped.Error())
		// Keep the local lease fail-closed. It expires automatically if the
		// surrounding database transaction rolls back and no retry renews it.
		return wrapped
	}
	return nil
}

func clearUserRefundHoldFenceLease(userId int, token string) (bool, error) {
	if userId <= 0 || token == "" {
		return false, errors.New("invalid refund hold fence lease")
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			return false, errors.New("redis refund hold fence is enabled but the client is not initialized")
		}
		const removeLease = `
local removed = redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[2])
if redis.call('ZCARD', KEYS[1]) == 0 then
  redis.call('DEL', KEYS[1])
end
return removed`
		removed, err := common.RDB.Eval(context.Background(), removeLease,
			[]string{userRefundHoldKey(userId)}, token, time.Now().UnixMilli(),
		).Int()
		if err != nil {
			return false, fmt.Errorf("clear refund hold fence for user %d: %w", userId, err)
		}
		localRemoved := clearLocalUserRefundHoldFence(userId, token)
		return removed == 1 || localRemoved, nil
	}
	return clearLocalUserRefundHoldFence(userId, token), nil
}

// SetUserRefundHoldFence publishes or renews the lease representing the
// durable User.RefundHold state. Refund transactions use their own scoped
// tokens so concurrent work cannot clear another transaction's pre-commit
// protection.
func SetUserRefundHoldFence(userId int) error {
	return renewUserRefundHoldFenceLease(userId, durableUserRefundHoldFenceToken)
}

// RenewUserRefundHoldFence extends an in-flight refund operation's lease.
// SetUserRefundHoldFence has the same renewal semantics; this name makes the
// intent explicit for longer-running recovery jobs.
func RenewUserRefundHoldFence(userId int) error {
	return SetUserRefundHoldFence(userId)
}

// ClearUserRefundHoldFence publishes an unfreeze safely. It verifies the
// durable state, keeps a live fence while invalidating the cached snapshot,
// and only then removes the distributed and local fences. Every failure leaves
// a retryable fence in place and is logged even when a best-effort caller drops
// the returned error.
func ClearUserRefundHoldFence(userId int) error {
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	var user User
	err := DB.Select("id", "refund_hold").Where("id = ?", userId).First(&user).Error
	if err == nil && user.RefundHold {
		renewErr := RenewUserRefundHoldFence(userId)
		if renewErr != nil {
			return errors.Join(ErrUserRefundHeld, renewErr)
		}
		common.SysError(fmt.Sprintf("refused to clear refund hold fence for user %d while the durable hold is active", userId))
		return ErrUserRefundHeld
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		wrapped := fmt.Errorf("verify durable refund hold for user %d: %w", userId, err)
		common.SysError(wrapped.Error())
		fenceErr := RenewUserRefundHoldFence(userId)
		return errors.Join(wrapped, fenceErr)
	}

	// The durable token closes the cache-invalidation window without replacing
	// leases owned by concurrent refund transactions.
	if err := RenewUserRefundHoldFence(userId); err != nil {
		return err
	}
	if common.RedisEnabled {
		if err := invalidateUserCache(userId); err != nil {
			wrapped := fmt.Errorf("invalidate user cache before clearing refund hold fence for user %d: %w", userId, err)
			common.SysError(wrapped.Error())
			return wrapped
		}
	}

	// Verify the durable state again after cache invalidation. A concurrent
	// refund may have committed after the first read.
	var latest User
	err = DB.Select("id", "refund_hold").Where("id = ?", userId).First(&latest).Error
	if err == nil && latest.RefundHold {
		return errors.Join(ErrUserRefundHeld, RenewUserRefundHoldFence(userId))
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		wrapped := fmt.Errorf("recheck durable refund hold for user %d: %w", userId, err)
		common.SysError(wrapped.Error())
		return wrapped
	}

	_, err = clearUserRefundHoldFenceLease(userId, durableUserRefundHoldFenceToken)
	if err != nil {
		common.SysError(err.Error())
		return err
	}
	active, err := userRefundHoldFenceActive(userId)
	if err != nil {
		return err
	}
	if active {
		// An in-flight owner still protects this user. Unowned reconciliation
		// must never remove that lease.
		return ErrUserRefundHeld
	}
	return nil
}

// ReconcileUserRefundHoldFenceState recovers or releases a lease from the
// database source of truth. Startup repair and retry workers may call it after
// a crash; active holds are renewed, while released/deleted users follow the
// cache-before-fence clearing protocol above.
func ReconcileUserRefundHoldFenceState(userId int) error {
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	var user User
	err := DB.Select("id", "refund_hold").Where("id = ?", userId).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ClearUserRefundHoldFence(userId)
	}
	if err != nil {
		wrapped := fmt.Errorf("reconcile refund hold fence for user %d: %w", userId, err)
		common.SysError(wrapped.Error())
		return wrapped
	}
	if user.RefundHold {
		if err := RenewUserRefundHoldFence(userId); err != nil {
			return err
		}
		if common.RedisEnabled {
			return invalidateUserCache(userId)
		}
		return nil
	}
	return ClearUserRefundHoldFence(userId)
}

func userRefundHoldFenceActive(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid user id")
	}
	if isLocalUserRefundHeld(userId) {
		return true, nil
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			return false, errors.New("redis refund hold fence is enabled but the client is not initialized")
		}
		const activeLeases = `
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
return redis.call('ZCARD', KEYS[1])`
		count, err := common.RDB.Eval(context.Background(), activeLeases,
			[]string{userRefundHoldKey(userId)}, time.Now().UnixMilli()).Int()
		if err != nil {
			return false, fmt.Errorf("check refund hold fence for user %d: %w", userId, err)
		}
		return count > 0, nil
	}
	return false, nil
}

func IsUserRefundHeld(userId int) (bool, error) {
	fenced, err := userRefundHoldFenceActive(userId)
	if err != nil || fenced {
		return fenced, err
	}
	user, err := GetUserCache(userId)
	if err != nil {
		return false, err
	}
	return user.RefundHold, nil
}
