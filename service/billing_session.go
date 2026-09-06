package service

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo          *relaycommon.RelayInfo
	funding            FundingSource
	preConsumedQuota   int  // 实际预扣额度（信任用户可能为 0）
	tokenConsumed      int  // 令牌额度实际扣减量
	trusted            bool // 是否命中信任额度旁路
	fundingSettled     bool // funding.Settle 已成功，资金来源已提交
	settled            bool // Settle 全部完成（资金 + 令牌）
	refunding          bool // funding.Refund 正在同步执行
	refunded           bool // funding.Refund 已成功
	initialReservation *model.BillingAdjustmentJournal
	pendingDispatch    []*model.BillingAdjustmentJournal
	pendingUsage       []*model.BillingAdjustmentJournal
	dispatchOccurred   bool
	mu                 sync.Mutex
}

// Settle 根据实际消耗额度进行结算。资金来源和令牌额度通过持久化
// adjustment journal 分步提交；任一侧失败都可由 system task 幂等恢复。
func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	if s.refunded || s.refunding {
		return errors.New("billing session refund is already in progress or completed")
	}
	if err := s.confirmDispatchLocked(); err != nil {
		return err
	}
	if err := s.reconcilePendingUsageLocked(); err != nil {
		return err
	}
	delta := actualQuota - s.preConsumedQuota
	fundingRequired := delta != 0 || s.funding.Source() == BillingSourceSubscription
	tokenRequired := delta != 0 && !s.relayInfo.IsPlayground
	adjustment, err := createBillingAdjustment(
		s.relayInfo,
		s.funding,
		model.BillingAdjustmentKindSettle,
		"settle:"+s.funding.Source(),
		delta,
		actualQuota,
		fundingRequired,
		delta,
		tokenRequired,
		false,
		false,
	)
	if err != nil {
		return err
	}

	// Once the final settlement intent is durable, Refund must not race it.
	s.settled = true
	if _, err := applyBillingFunding(adjustment, s.funding); err != nil {
		recordAndQueueBillingRecovery(adjustment.OperationKey, err)
		return err
	}
	s.fundingSettled = true
	if delta != 0 && s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}

	if err := applyBillingToken(adjustment); err != nil {
		recordAndQueueBillingRecovery(adjustment.OperationKey, err)
		common.SysLog(fmt.Sprintf("error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s",
			s.relayInfo.UserId, s.relayInfo.TokenId, delta, err.Error()))
		return err
	}

	return nil
}

// Refund synchronously returns the durable funding reservation. An
// undispatched authorization restores its token quota in the same call;
// dispatched requests keep the lower-latency asynchronous token hand-off.
// Both paths are journaled before either mutation, so process loss and
// commit-unknown retries are recoverable.
func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	if s.settled || s.refunded || s.refunding || !s.needsRefundLocked() {
		s.mu.Unlock()
		return
	}
	s.refunding = true
	initialReservation := s.initialReservation
	s.initialReservation = nil
	pendingDispatch := append([]*model.BillingAdjustmentJournal(nil), s.pendingDispatch...)
	s.pendingDispatch = nil
	s.mu.Unlock()

	for index, adjustment := range pendingDispatch {
		canceled, refund, err := refundUndispatchedBillingReservation(
			adjustment,
			s.funding,
			errors.New("request ended before the upstream attempt was authorized"),
		)
		if err == nil && !canceled {
			// A commit-unknown result may have completed the reservation before
			// this refund acquired the row. Replay it to rebuild the in-memory
			// cumulative target so the final refund returns the full amount.
			s.mu.Lock()
			err = s.applyReservationLocked(adjustment)
			s.mu.Unlock()
		}
		if err != nil {
			s.mu.Lock()
			s.refunding = false
			s.initialReservation = initialReservation
			s.pendingDispatch = append(pendingDispatch[index:], s.pendingDispatch...)
			s.mu.Unlock()
			common.SysLog("error canceling pending billing reservation: " + err.Error())
			return
		}
		if refund != nil {
			s.mu.Lock()
			refundedFunding := int(-refund.FundingDelta)
			refundedToken := int(-refund.TokenDelta)
			if wallet, ok := s.funding.(*WalletFunding); ok {
				wallet.consumed -= refundedFunding
				if wallet.consumed < 0 {
					wallet.consumed = 0
				}
				s.preConsumedQuota -= refundedFunding
				if s.preConsumedQuota < 0 {
					s.preConsumedQuota = 0
				}
			} else if subscription, ok := s.funding.(*SubscriptionFunding); ok && refund.FundingRequired {
				// Subscription refunds finalize the whole request lifecycle, not
				// only this top-up delta.
				subscription.preConsumed = 0
			}
			s.tokenConsumed -= refundedToken
			if s.tokenConsumed < 0 {
				s.tokenConsumed = 0
			}
			s.syncRelayInfo()
			s.mu.Unlock()
		}
	}

	s.mu.Lock()
	hasAppliedReservation := s.hasAppliedReservationLocked()
	s.mu.Unlock()
	if !hasAppliedReservation && initialReservation == nil {
		s.mu.Lock()
		s.refunding = false
		s.refunded = true
		s.mu.Unlock()
		return
	}

	if hasAppliedReservation {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费（token_quota=%s, funding=%s）",
			s.relayInfo.UserId,
			logger.FormatQuota(s.tokenConsumed),
			s.funding.Source(),
		))
	}

	refundedFunding := s.preConsumedQuota
	if wallet, ok := s.funding.(*WalletFunding); ok {
		refundedFunding = wallet.consumed
	} else if subscription, ok := s.funding.(*SubscriptionFunding); ok {
		refundedFunding = int(subscription.preConsumed)
	}
	var adjustment *model.BillingAdjustmentJournal
	var err error
	undispatchedRefund := initialReservation != nil && initialReservation.DispatchRequired && !initialReservation.DispatchConfirmed
	if undispatchedRefund {
		adjustment, err = model.CancelUndispatchedBillingReservationWithRefund(
			initialReservation.OperationKey,
			billingAdjustmentOperationKey(initialReservation.RequestId, "undispatched_refund:"+strconv.FormatInt(initialReservation.ID, 10)),
			errors.New("request ended before the upstream attempt was authorized"),
		)
		if errors.Is(err, model.ErrBillingAdjustmentDispatchConflict) {
			stored, loadErr := model.GetBillingAdjustment(initialReservation.OperationKey)
			if loadErr != nil {
				err = loadErr
			} else if stored != nil && stored.DispatchConfirmed && stored.Status != model.BillingAdjustmentStatusCanceled {
				initialReservation = stored
				undispatchedRefund = false
				err = nil
			}
		}
	}
	if !undispatchedRefund && err == nil && adjustment == nil {
		fundingRequired := refundedFunding > 0
		tokenRequired := s.tokenConsumed > 0 && !s.relayInfo.IsPlayground
		adjustment, err = createBillingAdjustment(
			s.relayInfo,
			s.funding,
			model.BillingAdjustmentKindRefund,
			"refund:"+s.funding.Source(),
			-refundedFunding,
			0,
			fundingRequired,
			-s.tokenConsumed,
			tokenRequired,
			false,
			false,
		)
	}
	if err != nil {
		s.mu.Lock()
		s.refunding = false
		s.initialReservation = initialReservation
		s.mu.Unlock()
		common.SysLog("error creating billing refund adjustment: " + err.Error())
		return
	}
	if adjustment == nil {
		s.mu.Lock()
		s.refunding = false
		s.refunded = true
		s.mu.Unlock()
		return
	}
	tokenRequired := adjustment.TokenRequired

	// Funds are refunded synchronously. Wallet credit and its applied marker
	// share one transaction; subscription refund and marker do too.
	if _, err := applyBillingFunding(adjustment, s.funding); err != nil {
		s.mu.Lock()
		s.refunding = false
		s.mu.Unlock()
		recordAndQueueBillingRecovery(adjustment.OperationKey, err)
		common.SysLog("error refunding billing source: " + err.Error())
		return
	}
	s.mu.Lock()
	s.refunding = false
	s.refunded = true
	s.mu.Unlock()

	if !tokenRequired {
		return
	}
	if undispatchedRefund {
		if err := applyBillingToken(adjustment); err != nil {
			recordAndQueueBillingRecovery(adjustment.OperationKey, err)
			common.SysLog("error refunding token quota: " + err.Error())
		}
		return
	}
	// Make the hand-off immediately recoverable before launching the best-effort
	// low-latency token refund goroutine.
	recordAndQueueBillingRecovery(adjustment.OperationKey, errors.New("token refund is awaiting asynchronous application"))
	gopool.Go(func() {
		if err := applyBillingToken(adjustment); err != nil {
			recordAndQueueBillingRecovery(adjustment.OperationKey, err)
			common.SysLog("error refunding token quota: " + err.Error())
		}
	})
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.refunding || s.fundingSettled {
		// fundingSettled 时资金来源已提交结算，不能再退预扣费
		return false
	}
	if s.tokenConsumed > 0 {
		return true
	}
	if s.initialReservation != nil {
		return true
	}
	if len(s.pendingDispatch) > 0 {
		return true
	}
	// 订阅可能在 tokenConsumed=0 时仍预扣了额度
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	return false
}

func (s *BillingSession) hasAppliedReservationLocked() bool {
	if s.preConsumedQuota > 0 || s.tokenConsumed > 0 {
		return true
	}
	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		return sub.preConsumed > 0
	}
	return false
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled || s.refunded || s.refunding || s.trusted {
		return nil
	}

	reservedTarget := s.preConsumedQuota
	if count := len(s.pendingDispatch); count > 0 {
		pendingTarget := int(s.pendingDispatch[count-1].FundingTarget)
		if pendingTarget > reservedTarget {
			reservedTarget = pendingTarget
		}
	}
	if targetQuota <= reservedTarget {
		return nil
	}
	delta := targetQuota - reservedTarget
	if delta <= 0 {
		return nil
	}
	tokenRequired := !s.relayInfo.IsPlayground
	adjustment, err := createBillingAdjustment(
		s.relayInfo,
		s.funding,
		model.BillingAdjustmentKindReserve,
		fmt.Sprintf("reserve:%s:%d", s.funding.Source(), targetQuota),
		delta,
		targetQuota,
		true,
		delta,
		tokenRequired,
		true,
		false,
	)
	if err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	s.pendingDispatch = append(s.pendingDispatch, adjustment)
	return nil
}

// ConfirmDispatch pre-applies every reservation prepared for the selected
// upstream attempt, then atomically records their dispatch authorization as
// the final database step. Until that marker commits, recovery reverses the
// provisional mutations; after it commits, recovery preserves/completes them.
// Callers enter the upstream network operation immediately after this returns.
func (s *BillingSession) ConfirmDispatch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.confirmDispatchLocked()
}

func (s *BillingSession) confirmDispatchLocked() error {
	if s.settled || s.refunded || s.refunding {
		return errors.New("billing session cannot authorize another upstream attempt")
	}
	// Apply every provisional reservation first. Until the final batch marker
	// commits, recovery treats these mutations as refundable pre-authorizations.
	// This keeps the irreducible DB-to-network crash window to one short marker
	// transaction immediately before the caller enters DoRequest.
	if s.initialReservation != nil {
		if err := s.applyReservationLocked(s.initialReservation); err != nil {
			return err
		}
	}
	for _, adjustment := range s.pendingDispatch {
		if err := s.applyReservationLocked(adjustment); err != nil {
			return err
		}
	}

	operationKeys := make([]string, 0, len(s.pendingDispatch)+1)
	if s.initialReservation != nil {
		operationKeys = append(operationKeys, s.initialReservation.OperationKey)
	}
	for _, adjustment := range s.pendingDispatch {
		operationKeys = append(operationKeys, adjustment.OperationKey)
	}
	if err := model.MarkBillingAdjustmentsDispatchConfirmed(operationKeys); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	s.initialReservation = nil
	s.pendingDispatch = nil
	return nil
}

// ReserveUsage extends a long-lived request after authoritative usage already
// exists. Unlike a pre-dispatch reservation, recovery must finish this charge.
func (s *BillingSession) ReserveUsage(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled || s.refunded || s.refunding || s.trusted {
		return nil
	}
	if err := s.confirmDispatchLocked(); err != nil {
		return err
	}
	if err := s.reconcilePendingUsageLocked(); err != nil {
		return err
	}
	if targetQuota <= s.preConsumedQuota {
		return nil
	}
	delta := targetQuota - s.preConsumedQuota
	tokenRequired := !s.relayInfo.IsPlayground
	adjustment, err := createBillingAdjustment(
		s.relayInfo,
		s.funding,
		model.BillingAdjustmentKindUsageReserve,
		fmt.Sprintf("usage_reserve:%s:%d", s.funding.Source(), targetQuota),
		delta,
		targetQuota,
		true,
		delta,
		tokenRequired,
		true,
		false,
	)
	if err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	s.pendingUsage = append(s.pendingUsage, adjustment)
	if err := s.applyReservationLocked(adjustment); err != nil {
		return err
	}
	s.pendingUsage = s.pendingUsage[:len(s.pendingUsage)-1]
	return nil
}

// reconcilePendingUsageLocked resolves a Realtime reservation whose database
// result was unknown to the caller. Settlement must not compute its delta from
// stale in-memory state while the same durable usage charge is still pending,
// otherwise recovery and final settlement could both charge that usage.
func (s *BillingSession) reconcilePendingUsageLocked() error {
	for len(s.pendingUsage) > 0 {
		pending := s.pendingUsage[0]
		stored, err := model.GetBillingAdjustment(pending.OperationKey)
		if err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if stored == nil {
			return types.NewError(errors.New("pending usage reservation is missing"), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if stored.Status == model.BillingAdjustmentStatusCanceled {
			s.pendingUsage = s.pendingUsage[1:]
			continue
		}
		if err := s.applyReservationLocked(stored); err != nil {
			latest, loadErr := model.GetBillingAdjustment(stored.OperationKey)
			if loadErr != nil {
				return types.NewError(loadErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			if latest != nil && latest.Status == model.BillingAdjustmentStatusCanceled {
				s.pendingUsage = s.pendingUsage[1:]
				continue
			}
			return err
		}
		s.pendingUsage = s.pendingUsage[1:]
	}
	return nil
}

func (s *BillingSession) applyReservationLocked(adjustment *model.BillingAdjustmentJournal) error {
	if adjustment == nil {
		return types.NewError(errors.New("billing reservation adjustment is missing"), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	delta := int(adjustment.FundingDelta)
	if err := applyBillingToken(adjustment); err != nil {
		_, cancelErr := cancelBillingReservation(adjustment, err)
		if cancelErr != nil {
			return types.NewError(cancelErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		return s.tokenReservationError(err, delta)
	}

	if _, err := applyBillingFunding(adjustment, s.funding); err != nil {
		canceled, cancelErr := cancelBillingReservation(adjustment, err)
		if cancelErr != nil {
			return types.NewError(cancelErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if canceled {
			return s.reserveFundingError(err)
		}
		if _, replayErr := applyBillingFunding(adjustment, s.funding); replayErr != nil {
			return types.NewError(replayErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}

	if wallet, ok := s.funding.(*WalletFunding); ok {
		wallet.consumed = int(adjustment.FundingTarget)
	}
	s.preConsumedQuota = int(adjustment.FundingTarget)
	if adjustment.TokenRequired {
		s.tokenConsumed = int(adjustment.FundingTarget)
	}
	s.syncRelayInfo()
	return nil
}

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣费入口（含信任额度旁路）
// ---------------------------------------------------------------------------

// preConsume 执行预扣费：信任检查 -> 令牌预扣 -> 资金来源预扣。
// 两侧通过 adjustment journal 幂等提交；资金来源失败时，令牌回滚也
// 是独立的持久化操作，可由 system task 在进程退出后继续完成。
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota

	// ---- 信任额度旁路 ----
	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, fmt.Sprintf("用户 %d 额度充足, 信任且不需要预扣费 (funding=%s)", s.relayInfo.UserId, s.funding.Source()))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 需要预扣费 %s (funding=%s)", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota), s.funding.Source()))
	}

	fundingRequired := effectiveQuota > 0
	tokenRequired := effectiveQuota > 0 && !s.relayInfo.IsPlayground
	adjustment, err := createBillingAdjustment(
		s.relayInfo,
		s.funding,
		model.BillingAdjustmentKindInitialReserve,
		"initial:"+s.funding.Source(),
		effectiveQuota,
		effectiveQuota,
		fundingRequired,
		effectiveQuota,
		tokenRequired,
		true,
		s.dispatchOccurred,
	)
	if err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}

	// ---- 1) 预扣令牌额度 ----
	if err := applyBillingToken(adjustment); err != nil {
		_, cancelErr := cancelBillingReservation(adjustment, err)
		if cancelErr != nil {
			return types.NewError(cancelErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		return s.tokenReservationError(err, effectiveQuota)
	}
	if tokenRequired {
		s.tokenConsumed = effectiveQuota
	}

	// ---- 2) 预扣资金来源 ----
	if _, err := applyBillingFunding(adjustment, s.funding); err != nil {
		canceled, cancelErr := cancelBillingReservation(adjustment, err)
		if cancelErr != nil {
			return types.NewError(cancelErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if canceled {
			s.tokenConsumed = 0
			return s.preConsumeFundingError(err)
		}
		if _, replayErr := applyBillingFunding(adjustment, s.funding); replayErr != nil {
			return types.NewError(replayErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}
	if wallet, ok := s.funding.(*WalletFunding); ok {
		wallet.consumed = effectiveQuota
	}

	s.preConsumedQuota = effectiveQuota
	s.initialReservation = adjustment

	// ---- 同步 RelayInfo 兼容字段 ----
	s.syncRelayInfo()

	return nil
}

func (s *BillingSession) tokenReservationError(err error, amount int) *types.NewAPIError {
	if errors.Is(err, model.ErrBillingAdjustmentTokenInsufficient) {
		remaining := 0
		if token, tokenErr := model.GetTokenById(s.relayInfo.TokenId); tokenErr == nil && token != nil {
			remaining = token.RemainQuota
		}
		err = fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(remaining), logger.FormatQuota(amount))
	}
	return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func (s *BillingSession) preConsumeFundingError(err error) *types.NewAPIError {
	if errors.Is(err, model.ErrUserRefundHeld) {
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeAccessDenied,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}
	if errors.Is(err, model.ErrBillingAdjustmentFundingInsufficient) || errors.Is(err, ErrInsufficientWalletQuota) {
		userQuota, quotaErr := model.GetUserQuota(s.relayInfo.UserId, false)
		if quotaErr != nil {
			userQuota = 0
		}
		return types.NewErrorWithStatusCode(
			fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") || strings.Contains(errMsg, "group unsupported") {
		return types.NewErrorWithStatusCode(fmt.Errorf("订阅额度不足或未配置订阅: %s", errMsg), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

func (s *BillingSession) reserveFundingError(err error) error {
	if s.funding.Source() == BillingSourceWallet &&
		(errors.Is(err, model.ErrUserRefundHeld) || errors.Is(err, model.ErrBillingAdjustmentFundingInsufficient) || errors.Is(err, ErrInsufficientWalletQuota)) {
		return s.preConsumeFundingError(err)
	}
	if s.funding.Source() == BillingSourceSubscription {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("订阅额度不足或未配置订阅: %s", err.Error()),
			types.ErrorCodeInsufficientUserQuota,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}
	return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

// shouldTrust 统一信任额度检查，适用于钱包和订阅。
func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	// A post-response charge is already authoritative. It must leave a durable
	// funding intent even when the account would normally qualify for the
	// pre-dispatch trust shortcut.
	if s.dispatchOccurred {
		return false
	}
	// 异步任务（ForcePreConsume=true）必须预扣全额，不允许信任旁路
	if s.relayInfo.ForcePreConsume {
		return false
	}
	// Realtime connections are long-lived and extend their reservation as
	// cumulative usage grows. They need a real initial reservation so Reserve
	// can keep enforcing wallet and token limits throughout the stream.
	if s.relayInfo.RelayFormat == types.RelayFormatOpenAIRealtime {
		return false
	}

	trustQuota := common.GetTrustQuota()
	if trustQuota <= 0 {
		return false
	}

	// 检查令牌是否充足
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenQuota := c.GetInt("token_quota")
		tokenTrusted = tokenQuota > trustQuota
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return s.relayInfo.UserQuota > trustQuota
	case BillingSourceSubscription:
		// 订阅不能启用信任旁路。原因：
		// 1. PreConsumeUserSubscription 要求 amount>0 来创建预扣记录并锁定订阅
		// 2. SubscriptionFunding.PreConsume 忽略参数，始终用 s.amount 预扣
		// 3. 若信任旁路将 effectiveQuota 设为 0，会导致 preConsumedQuota 与实际订阅预扣不一致
		return false
	default:
		return false
	}
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionPreConsumed = sub.preConsumed
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	return newBillingSession(c, relayInfo, preConsumedQuota, false)
}

// newBillingSession can also establish a charge after authoritative upstream
// usage has already occurred. In that mode the initial journal is born with
// its dispatch marker confirmed, so a process loss before Settle cannot make
// recovery mistake the charge for an unused pre-authorization.
func newBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int, dispatchOccurred bool) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)

	// 钱包路径需要先检查用户额度
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if userQuota-preConsumedQuota < 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo:        relayInfo,
			funding:          &WalletFunding{userId: relayInfo.UserId},
			dispatchOccurred: dispatchOccurred,
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo:        relayInfo,
			dispatchOccurred: dispatchOccurred,
			funding: &SubscriptionFunding{
				requestId:  relayInfo.RequestId,
				userId:     relayInfo.UserId,
				modelName:  relayInfo.GetBillingModelName(),
				usingGroup: relayInfo.UsingGroup,
				amount:     subConsume,
			},
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证 SubscriptionFunding.amount、
		// preConsume 参数和 FinalPreConsumedQuota 三者一致，避免订阅多扣费。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, err := tryWallet()
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				// 仅当用户的活跃订阅允许钱包回退时才回退到钱包，否则返回订阅额度不足错误
				allowOverflow, overflowErr := model.UserActiveSubscriptionsAllowWalletOverflow(relayInfo.UserId)
				if overflowErr != nil {
					return nil, types.NewError(overflowErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
				}
				if allowOverflow {
					return tryWallet()
				}
				return nil, apiErr
			}
			return nil, apiErr
		}
		return session, nil
	}
}
