package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errPromotionRefundWithdrawalLockOrderChanged = errors.New("promotion withdrawal association changed while acquiring refund locks")

// lockPromotionRefundIntakeUsersTx adds the administrator to the existing
// refund-side user lock order before a manual case creates durable actor
// evidence. Provider refunds have no local actor and keep the normal path.
func lockPromotionRefundIntakeUsersTx(tx *gorm.DB, topUp *TopUp, actorId int, actorRole int) error {
	if tx == nil || actorId <= 0 || actorRole <= 0 {
		return errors.New("manual refund actor and transaction are required")
	}
	userIds := []int{actorId}
	if topUp != nil && topUp.Id > 0 && topUp.UserId > 0 {
		userIds = append(userIds, topUp.UserId)

		var rebate InvitationRebate
		if err := tx.Where("top_up_id = ?", topUp.Id).First(&rebate).Error; err == nil {
			if rebate.InviteeId == topUp.UserId && rebate.InviterId > 0 {
				userIds = append(userIds, rebate.InviterId)
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var rewardUserIds []int
		if err := tx.Model(&InvitationReward{}).
			Where("trigger_top_up_id = ? AND invitee_id = ?", topUp.Id, topUp.UserId).
			Pluck("inviter_id", &rewardUserIds).Error; err != nil {
			return err
		}
		for _, userId := range rewardUserIds {
			if userId > 0 {
				userIds = append(userIds, userId)
			}
		}
	}

	lockedUsers, err := lockUsersForFinancialWriteTx(tx, userIds...)
	if err != nil {
		return err
	}
	if lockedUsers[actorId].DeletedAt.Valid {
		return gorm.ErrRecordNotFound
	}
	if lockedUsers[actorId].Role != actorRole {
		return errors.New("manual refund actor role changed")
	}
	return nil
}

// HandlePromotionRefund applies a verified provider refund to the original
// API-balance credit and every promotion benefit derived from that payment.
// Any amount that cannot be recovered immediately becomes an explicit debt;
// the affected account remains unable to consume funds until an administrator
// completes a structured recovery action and releases the hold.
func HandlePromotionRefund(input PromotionRefundInput) (*PromotionRefundCase, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.TradeNo = strings.TrimSpace(input.TradeNo)
	input.RefundTradeNo = strings.TrimSpace(input.RefundTradeNo)
	refundTradeNos := make([]string, 0, len(input.EquivalentRefundTradeNos)+1)
	seenRefundTradeNos := make(map[string]struct{}, len(input.EquivalentRefundTradeNos)+1)
	for _, refundTradeNo := range append([]string{input.RefundTradeNo}, input.EquivalentRefundTradeNos...) {
		refundTradeNo = strings.TrimSpace(refundTradeNo)
		if refundTradeNo == "" {
			continue
		}
		if _, ok := seenRefundTradeNos[refundTradeNo]; ok {
			continue
		}
		seenRefundTradeNos[refundTradeNo] = struct{}{}
		refundTradeNos = append(refundTradeNos, refundTradeNo)
	}
	input.EquivalentRefundTradeNos = refundTradeNos
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Remark = strings.TrimSpace(input.Remark)
	input.adminIdempotencyKey = strings.TrimSpace(input.adminIdempotencyKey)
	input.intakeSource = strings.TrimSpace(input.intakeSource)
	if input.Provider == "" || input.TradeNo == "" || input.RefundTradeNo == "" {
		return nil, errors.New("refund provider, trade number, and refund number are required")
	}
	if input.adminIdempotencyKey != "" && (input.initiatorId <= 0 || input.initiatorRole <= 0 || input.intakeSource == "") {
		return nil, errors.New("manual refund intake metadata is incomplete")
	}
	if input.PaidAmountMinor < 0 || input.RefundedAmountMinor < 0 {
		return nil, errors.New("refund amounts cannot be negative")
	}
	if input.Currency != "" && !isISOCurrencyCode(input.Currency) {
		return nil, errors.New("invalid refund currency")
	}
	if input.Kind != PromotionRefundKindFull && input.Kind != PromotionRefundKindPartial && input.Kind != PromotionRefundKindDispute {
		return nil, errors.New("invalid promotion refund kind")
	}
	if input.Kind == PromotionRefundKindPartial && input.RefundedAmountMinor <= 0 {
		return nil, errors.New("partial refund amount must be positive")
	}
	if input.PaidAmountMinor > 0 && input.RefundedAmountMinor > input.PaidAmountMinor && input.AmountIsCumulative {
		return nil, errors.New("refunded amount exceeds paid amount")
	}
	payloadHash, err := promotionRefundPayloadHash(input, input.RefundTradeNo)
	if err != nil {
		return nil, err
	}

	eventIdentity := input.TradeNo + ":" + input.RefundTradeNo
	if input.AmountIsCumulative {
		// Some providers reuse one refund object while increasing its cumulative
		// refunded amount. Treat each new cumulative target as a distinct business
		// event, while repeated delivery of the same target remains idempotent.
		eventIdentity = fmt.Sprintf("%s:%d", eventIdentity, input.RefundedAmountMinor)
	}
	eventKey := fmt.Sprintf("%s:%s", input.Provider, common.Sha1([]byte(eventIdentity)))
	if input.adminIdempotencyKey != "" {
		eventKey = fmt.Sprintf("admin_refund:%s", common.Sha1([]byte(input.adminIdempotencyKey)))
	}
	compatibleEventKeys := make([]string, 0, len(refundTradeNos)*2)
	seenEventKeys := map[string]struct{}{eventKey: {}}
	for _, refundTradeNo := range refundTradeNos {
		identity := input.TradeNo + ":" + refundTradeNo
		if input.AmountIsCumulative {
			identity = fmt.Sprintf("%s:%d", identity, input.RefundedAmountMinor)
		}
		keys := []string{
			fmt.Sprintf("%s:%s", input.Provider, common.Sha1([]byte(identity))),
			// Versions before the refund-accounting upgrade included kind in
			// the key and did not include a cumulative amount.
			fmt.Sprintf("%s:%s", input.Provider, common.Sha1([]byte(input.TradeNo+":"+refundTradeNo+":"+input.Kind))),
		}
		for _, key := range keys {
			if _, ok := seenEventKeys[key]; ok {
				continue
			}
			seenEventKeys[key] = struct{}{}
			compatibleEventKeys = append(compatibleEventKeys, key)
		}
	}

	now := common.GetTimestamp()
	intakeSource := PromotionRefundIntakeProviderWebhook
	intakeFingerprint := ""
	initiatorType := "provider"
	if input.intakeSource != "" {
		intakeSource = input.intakeSource
		intakeFingerprint = common.Sha1([]byte(input.intakeSource + "\x00" + input.Remark))
		initiatorType = "admin"
	}
	refundCaseValues := PromotionRefundCase{
		EventKey:            eventKey,
		PayloadHash:         payloadHash,
		Provider:            input.Provider,
		TradeNo:             input.TradeNo,
		RefundTradeNo:       input.RefundTradeNo,
		Kind:                input.Kind,
		PaidAmountMinor:     input.PaidAmountMinor,
		RefundedAmountMinor: input.RefundedAmountMinor,
		Currency:            input.Currency,
		Status:              PromotionRefundCaseStatusPendingReview,
		Reason:              input.Remark,
		IntakeSource:        intakeSource,
		IntakeFingerprint:   intakeFingerprint,
		InitiatorType:       initiatorType,
		InitiatorId:         input.initiatorId,
		InitiatorRole:       input.initiatorRole,
		CreatedAt:           now,
	}
	var refundCase *PromotionRefundCase
	fencedUserIds := newRefundHoldFenceScope()
	for attempt := 0; attempt < 3; attempt++ {
		currentRefundCase := refundCaseValues
		refundCase = &currentRefundCase
		err = DB.Transaction(func(tx *gorm.DB) error {
			// All refund/top-up flows use the same lock order: top-up, refund case,
			// downstream responsibility, then users. If the order does not exist yet,
			// its later Insert path will bind this case after creation.
			topUp := &TopUp{}
			topUpErr := lockForUpdate(tx).Where("trade_no = ?", input.TradeNo).First(topUp).Error
			if topUpErr != nil && !errors.Is(topUpErr, gorm.ErrRecordNotFound) {
				return topUpErr
			}
			existing, err := findCompatiblePromotionRefundCaseTx(tx, input, eventKey, compatibleEventKeys)
			if err != nil {
				return err
			}
			if existing != nil {
				refundCase = existing
				return loadPromotionRefundRecoveryTx(tx, refundCase)
			}
			var commissionWithdrawal *PromotionWithdrawal
			if topUpErr == nil {
				commissionWithdrawal, err = lockPromotionRefundCommissionWithdrawalTx(tx, topUp)
				if err != nil {
					return err
				}
			}
			if input.adminIdempotencyKey != "" {
				if err := lockPromotionRefundIntakeUsersTx(tx, topUp, input.initiatorId, input.initiatorRole); err != nil {
					return err
				}
			}

			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "event_key"}},
				DoNothing: true,
			}).Create(refundCase)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 || refundCase.Id == 0 {
				if err := tx.Where("event_key = ?", eventKey).First(refundCase).Error; err != nil {
					return err
				}
				compatible, err := promotionRefundCaseMatchesInput(refundCase, input)
				if err != nil {
					return err
				}
				if !compatible {
					return ErrPromotionRefundEventConflict
				}
				if input.adminIdempotencyKey != "" &&
					(refundCase.InitiatorType != "admin" || refundCase.InitiatorId != input.initiatorId ||
						refundCase.InitiatorRole != input.initiatorRole) {
					return ErrPromotionRefundEventConflict
				}
				return loadPromotionRefundRecoveryTx(tx, refundCase)
			}

			if errors.Is(topUpErr, gorm.ErrRecordNotFound) {
				refundCase.Reason = "local top-up order not found; manual review required"
				return placeRefundCaseOnHoldTx(tx, refundCase, nil, fencedUserIds)
			}
			refundCase.TopUpId = topUp.Id
			refundCase.UserId = topUp.UserId
			if topUp.PaymentProvider != input.Provider {
				refundCase.Reason = fmt.Sprintf("payment provider mismatch: stored=%s callback=%s", topUp.PaymentProvider, input.Provider)
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			if topUp.Purpose == TopUpPurposeSubscription {
				refundCase.Reason = "subscription payment refund requires subscription recovery"
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			if topUp.Status != common.TopUpStatusSuccess {
				refundCase.Reason = "top-up was not completed; manual review required"
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			if !topUp.PaidAmountVerified || topUp.PaidAmountMinor <= 0 || topUp.PaidCurrency == "" {
				refundCase.Reason = "verified payment snapshot is missing; manual recovery amount is required"
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			refundCase.PaidAmountMinor = topUp.PaidAmountMinor
			refundCase.Currency = topUp.PaidCurrency
			if input.PaidAmountMinor > 0 && input.PaidAmountMinor != topUp.PaidAmountMinor {
				refundCase.Reason = "provider paid amount does not match the stored payment snapshot"
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			if input.Currency != "" && input.Currency != topUp.PaidCurrency {
				refundCase.Reason = fmt.Sprintf("refund currency mismatch: stored=%s callback=%s", topUp.PaidCurrency, input.Currency)
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			if topUp.CreditedQuota <= 0 {
				refundCase.Reason = "credited quota snapshot is missing; manual recovery amount is required"
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}

			targetRefundedMinor := input.RefundedAmountMinor
			if input.Kind == PromotionRefundKindFull || input.Kind == PromotionRefundKindDispute {
				targetRefundedMinor = topUp.PaidAmountMinor
			} else if !input.AmountIsCumulative {
				if input.RefundedAmountMinor > math.MaxInt64-topUp.RefundedAmountMinor {
					return errors.New("cumulative refund amount overflow")
				}
				targetRefundedMinor = topUp.RefundedAmountMinor + input.RefundedAmountMinor
			}
			if targetRefundedMinor <= 0 || targetRefundedMinor > topUp.PaidAmountMinor {
				refundCase.Reason = "refunded amount exceeds the verified payment snapshot"
				return placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds)
			}
			if targetRefundedMinor < topUp.RefundedAmountMinor {
				refundCase.Reason = "stale cumulative refund notification"
				refundCase.RefundedAmountMinor = 0
				refundCase.Status = PromotionRefundCaseStatusResolved
				refundCase.ResolvedAt = now
				if _, err := bindPromotionRefundCaseToTopUpTx(tx, refundCase, topUp, fencedUserIds, false); err != nil {
					return err
				}
				return tx.Model(refundCase).Updates(map[string]interface{}{
					"top_up_id": topUp.Id, "user_id": topUp.UserId, "paid_amount_minor": refundCase.PaidAmountMinor,
					"currency": refundCase.Currency, "refunded_amount_minor": int64(0), "reason": refundCase.Reason,
					"status": refundCase.Status, "resolved_at": refundCase.ResolvedAt,
					"invitation_rebate_id": refundCase.InvitationRebateId, "commission_ledger_id": refundCase.CommissionLedgerId,
					"responsibility_fingerprint": refundCase.ResponsibilityFingerprint,
					"requires_root_review":       refundCase.RequiresRootReview,
				}).Error
			}

			targetRefundedQuota := topUp.CreditedQuota
			if targetRefundedMinor < topUp.PaidAmountMinor {
				quotaDecimal := decimal.NewFromInt(int64(topUp.CreditedQuota)).
					Mul(decimal.NewFromInt(targetRefundedMinor)).
					Div(decimal.NewFromInt(topUp.PaidAmountMinor)).Floor()
				var quotaErr error
				targetRefundedQuota, quotaErr = common.QuotaFromDecimalStrict(quotaDecimal)
				if quotaErr != nil {
					return quotaErr
				}
			}
			quotaDelta := targetRefundedQuota - topUp.RefundedQuota
			if quotaDelta < 0 {
				return errors.New("stored refunded quota exceeds target refund quota")
			}
			refundCase.RefundedAmountMinor = targetRefundedMinor - topUp.RefundedAmountMinor
			refundCase.QuotaAmount = quotaDelta

			walletDebit, debt, err := applyTopUpPrincipalRefundTx(tx, refundCase, topUp, quotaDelta, input.Kind == PromotionRefundKindDispute, fencedUserIds)
			if err != nil {
				return err
			}
			refundCase.WalletDebitedQuota = walletDebit
			refundCase.DebtCreatedQuota += debt

			reverseFixedRewards := input.Kind == PromotionRefundKindDispute || targetRefundedMinor == topUp.PaidAmountMinor
			promotionDebtQuota, promotionDebtCash, err := reversePromotionBenefitsForRefundTx(tx, refundCase, topUp, reverseFixedRewards, commissionWithdrawal, fencedUserIds)
			if err != nil {
				return err
			}
			refundCase.DebtCreatedQuota += promotionDebtQuota
			refundCase.CashDebtCreatedMinor += promotionDebtCash

			topUp.RefundedAmountMinor = targetRefundedMinor
			topUp.RefundedQuota = targetRefundedQuota
			topUp.RefundedAt = now
			switch {
			case input.Kind == PromotionRefundKindDispute:
				topUp.RefundStatus = TopUpRefundStatusDisputed
			case targetRefundedMinor == topUp.PaidAmountMinor:
				topUp.RefundStatus = TopUpRefundStatusFull
			default:
				topUp.RefundStatus = TopUpRefundStatusPartial
			}
			if err := tx.Save(topUp).Error; err != nil {
				return err
			}

			var openObligations int64
			if err := tx.Model(&PromotionRefundObligation{}).
				Where("refund_case_id = ? AND status = ?", refundCase.Id, PromotionRefundObligationStatusOpen).
				Count(&openObligations).Error; err != nil {
				return err
			}
			updates := map[string]interface{}{
				"top_up_id": topUp.Id, "user_id": topUp.UserId, "paid_amount_minor": refundCase.PaidAmountMinor,
				"refunded_amount_minor": refundCase.RefundedAmountMinor, "currency": refundCase.Currency,
				"quota_amount": refundCase.QuotaAmount, "wallet_debited_quota": refundCase.WalletDebitedQuota,
				"debt_created_quota": refundCase.DebtCreatedQuota, "cash_debt_created_minor": refundCase.CashDebtCreatedMinor,
			}
			if openObligations == 0 && input.Kind != PromotionRefundKindDispute && !refundCase.RequiresRootReview {
				refundCase.Status = PromotionRefundCaseStatusResolved
				refundCase.ResolvedAt = now
				updates["status"] = refundCase.Status
				updates["resolved_at"] = now
			} else {
				if !refundCase.RequiresRootReview {
					refundCase.Reason = "refund recovery is incomplete; complete the listed obligations before releasing the hold"
				}
				updates["reason"] = refundCase.Reason
			}
			if _, err := bindPromotionRefundCaseToTopUpTx(tx, refundCase, topUp, fencedUserIds, false); err != nil {
				return err
			}
			updates["invitation_rebate_id"] = refundCase.InvitationRebateId
			updates["commission_ledger_id"] = refundCase.CommissionLedgerId
			updates["responsibility_fingerprint"] = refundCase.ResponsibilityFingerprint
			updates["requires_root_review"] = refundCase.RequiresRootReview
			if err := tx.Model(refundCase).Updates(updates).Error; err != nil {
				return err
			}
			return loadPromotionRefundRecoveryTx(tx, refundCase)
		})
		if !errors.Is(err, errPromotionRefundWithdrawalLockOrderChanged) {
			break
		}
	}

	reconcileErr := reconcilePromotionRefundHoldFences(fencedUserIds)
	if err != nil {
		return nil, errors.Join(err, reconcileErr)
	}
	if reconcileErr != nil {
		return nil, reconcileErr
	}
	return refundCase, nil
}

func promotionRefundPayloadHash(input PromotionRefundInput, refundTradeNo string) (string, error) {
	payload, err := common.Marshal(struct {
		Provider            string `json:"provider"`
		TradeNo             string `json:"trade_no"`
		RefundTradeNo       string `json:"refund_trade_no"`
		Kind                string `json:"kind"`
		PaidAmountMinor     int64  `json:"paid_amount_minor"`
		RefundedAmountMinor int64  `json:"refunded_amount_minor"`
		Currency            string `json:"currency"`
		AmountIsCumulative  bool   `json:"amount_is_cumulative"`
	}{
		Provider: input.Provider, TradeNo: input.TradeNo, RefundTradeNo: refundTradeNo,
		Kind: input.Kind, PaidAmountMinor: input.PaidAmountMinor, RefundedAmountMinor: input.RefundedAmountMinor,
		Currency: input.Currency, AmountIsCumulative: input.AmountIsCumulative,
	})
	if err != nil {
		return "", err
	}
	return common.Sha1(payload), nil
}

func applyPromotionRefundFundActor(transaction *PromotionFundTransaction, refundCase *PromotionRefundCase) {
	if transaction == nil || refundCase == nil {
		return
	}
	if refundCase.InitiatorType == "admin" && refundCase.InitiatorId > 0 {
		transaction.ActorType = "admin"
		transaction.ActorId = refundCase.InitiatorId
		transaction.ActorRef = ""
		return
	}
	transaction.ActorType = "provider"
	transaction.ActorRef = refundCase.Provider
}

func findCompatiblePromotionRefundCaseTx(tx *gorm.DB, input PromotionRefundInput, canonicalEventKey string, aliasEventKeys []string) (*PromotionRefundCase, error) {
	var canonical PromotionRefundCase
	err := tx.Where("event_key = ?", canonicalEventKey).First(&canonical).Error
	if err == nil {
		compatible, matchErr := promotionRefundCaseMatchesInput(&canonical, input)
		if matchErr != nil {
			return nil, matchErr
		}
		if !compatible {
			return nil, ErrPromotionRefundEventConflict
		}
		if input.adminIdempotencyKey != "" && (canonical.IntakeSource != input.intakeSource ||
			canonical.IntakeFingerprint != common.Sha1([]byte(input.intakeSource+"\x00"+input.Remark)) ||
			canonical.InitiatorType != "admin" || canonical.InitiatorId != input.initiatorId ||
			canonical.InitiatorRole != input.initiatorRole) {
			return nil, ErrPromotionRefundEventConflict
		}
		return &canonical, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if len(aliasEventKeys) > 0 {
		var candidates []*PromotionRefundCase
		if err := tx.Where("event_key IN ?", aliasEventKeys).Order("id ASC").Find(&candidates).Error; err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			compatible, err := promotionRefundCaseMatchesInput(candidate, input)
			if err != nil {
				return nil, err
			}
			if compatible {
				return candidate, nil
			}
			// A cumulative refund deliberately advances through multiple economic
			// targets. Old versions used an amount-independent alias key, so a row
			// for an earlier target must not block the new canonical amount key.
			if !input.AmountIsCumulative {
				return nil, ErrPromotionRefundEventConflict
			}
		}
	}

	// Root-created missed-callback cases use an administrator idempotency key
	// as their canonical event key. Match the verified provider identity too,
	// so a delayed webhook or a second Root report cannot apply the same refund
	// twice merely because it arrived through a different intake path.
	refundTradeNos := input.EquivalentRefundTradeNos
	if len(refundTradeNos) == 0 {
		refundTradeNos = []string{input.RefundTradeNo}
	}
	var identityCandidates []*PromotionRefundCase
	if err := tx.Where("provider = ? AND trade_no = ? AND refund_trade_no IN ?", input.Provider, input.TradeNo, refundTradeNos).
		Order("id ASC").Find(&identityCandidates).Error; err != nil {
		return nil, err
	}
	for _, candidate := range identityCandidates {
		compatible, err := promotionRefundCaseMatchesInput(candidate, input)
		if err != nil {
			return nil, err
		}
		if compatible {
			return candidate, nil
		}
		if !input.AmountIsCumulative {
			return nil, ErrPromotionRefundEventConflict
		}
	}
	return nil, nil
}

func promotionRefundCaseMatchesInput(refundCase *PromotionRefundCase, input PromotionRefundInput) (bool, error) {
	if refundCase.Provider != input.Provider || refundCase.TradeNo != input.TradeNo || refundCase.Kind != input.Kind {
		return false, nil
	}
	referenceMatches := false
	for _, refundTradeNo := range input.EquivalentRefundTradeNos {
		if refundCase.RefundTradeNo == refundTradeNo {
			referenceMatches = true
			break
		}
	}
	if !referenceMatches {
		return false, nil
	}

	if refundCase.PayloadHash != "" {
		for _, refundTradeNo := range input.EquivalentRefundTradeNos {
			payloadHash, err := promotionRefundPayloadHash(input, refundTradeNo)
			if err != nil {
				return false, err
			}
			if refundCase.PayloadHash == payloadHash {
				return true, nil
			}
		}
		return false, nil
	}

	// Legacy rows have no payload hash. Their persisted core fields are the
	// only proof available, so require exact equality and deliberately leave
	// PayloadHash empty. Backfilling today's hash would assert facts (such as
	// cumulative semantics) that the old row never recorded.
	return refundCase.PaidAmountMinor == input.PaidAmountMinor &&
		refundCase.RefundedAmountMinor == input.RefundedAmountMinor &&
		refundCase.Currency == input.Currency, nil
}

func loadPromotionRefundRecoveryTx(tx *gorm.DB, refundCase *PromotionRefundCase) error {
	if tx == nil || refundCase == nil || refundCase.Id <= 0 {
		return nil
	}
	if err := tx.Where("refund_case_id = ?", refundCase.Id).Order("id ASC").Find(&refundCase.Obligations).Error; err != nil {
		return err
	}
	return tx.Where("refund_case_id = ?", refundCase.Id).Order("id ASC").Find(&refundCase.Actions).Error
}

func placeRefundCaseOnHoldTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp, fencedUserIds refundHoldFenceScope) error {
	refundCase.RequiresRootReview = true
	responsibleUserIds := make(map[int]struct{}, 2)
	if topUp != nil && topUp.Id > 0 {
		refundCase.TopUpId = topUp.Id
		refundCase.UserId = topUp.UserId
		if topUp.UserId > 0 {
			responsibleUserIds[topUp.UserId] = struct{}{}
		}

		rebate, ledger, err := loadPromotionRefundLinkedCommissionTx(tx, refundCase, topUp)
		if err != nil {
			linkageReason := fmt.Sprintf("linked promotion responsibility is inconsistent: %v", err)
			if refundCase.Reason == "" {
				refundCase.Reason = linkageReason
			} else {
				refundCase.Reason += "; " + linkageReason
			}
		} else {
			if rebate != nil {
				refundCase.InvitationRebateId = rebate.Id
				if rebate.InviterId > 0 {
					responsibleUserIds[rebate.InviterId] = struct{}{}
				}
			}
			if ledger != nil {
				refundCase.CommissionLedgerId = ledger.Id
				if ledger.UserId > 0 {
					responsibleUserIds[ledger.UserId] = struct{}{}
				}
			}
		}
	}
	if topUp != nil && topUp.PaymentProvider == refundCase.Provider {
		if _, err := bindPromotionRefundCaseToTopUpTx(tx, refundCase, topUp, fencedUserIds, false); err != nil {
			linkageReason := fmt.Sprintf("refund responsibility snapshot is incomplete: %v", err)
			if refundCase.Reason == "" {
				refundCase.Reason = linkageReason
			} else {
				refundCase.Reason += "; " + linkageReason
			}
		}
	}

	orderedUserIds := make([]int, 0, len(responsibleUserIds))
	for userId := range responsibleUserIds {
		orderedUserIds = append(orderedUserIds, userId)
	}
	sort.Ints(orderedUserIds)
	if len(orderedUserIds) > 0 {
		if err := recordPromotionRefundCaseUsersTx(tx, refundCase.Id, orderedUserIds...); err != nil {
			return err
		}
	}
	for _, userId := range orderedUserIds {
		if err := fencedUserIds.Ensure(userId); err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&User{}).Where("id = ?", userId).Update("refund_hold", true).Error; err != nil {
			return err
		}
	}
	return tx.Model(refundCase).Updates(map[string]interface{}{
		"top_up_id":                  refundCase.TopUpId,
		"user_id":                    refundCase.UserId,
		"invitation_rebate_id":       refundCase.InvitationRebateId,
		"commission_ledger_id":       refundCase.CommissionLedgerId,
		"responsibility_fingerprint": refundCase.ResponsibilityFingerprint,
		"paid_amount_minor":          refundCase.PaidAmountMinor,
		"currency":                   refundCase.Currency,
		"requires_root_review":       true,
		"reason":                     refundCase.Reason,
	}).Error
}

func applyTopUpPrincipalRefundTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp, quota int, forceHold bool, fencedUserIds refundHoldFenceScope) (int, int64, error) {
	if quota < 0 {
		return 0, 0, errors.New("refund quota cannot be negative")
	}
	originalCredit, err := ensureTopUpFundTransactionTx(tx, topUp)
	if err != nil {
		return 0, 0, err
	}
	if originalCredit == nil {
		return 0, 0, ErrTopUpFundTransactionInvalid
	}
	if err := fencedUserIds.Ensure(topUp.UserId); err != nil {
		return 0, 0, err
	}
	var user User
	if err := lockForUpdate(tx.Unscoped()).Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
		return 0, 0, err
	}
	walletDebit := quota
	if walletDebit > user.Quota {
		walletDebit = user.Quota
	}
	if walletDebit < 0 {
		walletDebit = 0
	}
	debt := int64(quota - walletDebit)
	user.Quota -= walletDebit
	if debt > 0 {
		if user.RefundDebtQuota > math.MaxInt64-debt {
			return 0, 0, errors.New("refund debt overflow")
		}
		user.RefundDebtQuota += debt
		obligation := &PromotionRefundObligation{
			ObligationKey: fmt.Sprintf("refund:%d:topup:%d:principal", refundCase.Id, topUp.Id),
			RefundCaseId:  refundCase.Id, UserId: user.Id, Account: PromotionFundAccountRefundDebt,
			Asset: PromotionFundAssetQuota, Amount: debt, SourceType: "top_ups", SourceId: topUp.Id,
		}
		if err := CreatePromotionRefundObligationTx(tx, obligation); err != nil {
			return 0, 0, err
		}
	}
	user.RefundHold = user.RefundHold || forceHold || debt > 0
	if err := tx.Unscoped().Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota": user.Quota, "refund_debt_quota": user.RefundDebtQuota, "refund_hold": user.RefundHold,
	}).Error; err != nil {
		return 0, 0, err
	}

	legs := make([]PromotionFundTransactionLeg, 0, 2)
	if walletDebit > 0 {
		balance := int64(user.Quota)
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: -int64(walletDebit),
			SourceType: "top_ups", SourceId: topUp.Id, BalanceAfter: &balance,
		})
	}
	if debt > 0 {
		balance := user.RefundDebtQuota
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: debt,
			SourceType: "top_ups", SourceId: topUp.Id, BalanceAfter: &balance,
		})
	}
	if len(legs) > 0 {
		transaction := &PromotionFundTransaction{
			TransactionKey: fmt.Sprintf("refund:%d:principal", refundCase.Id), Kind: PromotionFundKindReversal, UserId: user.Id,
			SourceType: "promotion_refund_cases", SourceId: refundCase.Id, SourceKey: refundCase.RefundTradeNo,
			ReversesTransactionId: originalCredit.Id,
			ExternalRef:           refundCase.RefundTradeNo, Remark: "reverse refunded top-up API balance",
		}
		applyPromotionRefundFundActor(transaction, refundCase)
		if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
			return 0, 0, err
		}
	}
	return walletDebit, debt, nil
}

// lockPromotionRefundCommissionWithdrawalTx establishes the refund-side lock
// order before any responsible user is locked: withdrawal, all of its ledgers,
// then users. A ledger can become attached to a newly-created withdrawal while
// this transaction waits for it; in that case the caller restarts the whole
// transaction instead of taking the newly-discovered withdrawal after a ledger.
func lockPromotionRefundCommissionWithdrawalTx(tx *gorm.DB, topUp *TopUp) (*PromotionWithdrawal, error) {
	var rebate InvitationRebate
	err := lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).First(&rebate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var discoveredLedger PromotionCommissionLedger
	err = tx.Where("source_type = ? AND source_id = ?", PromotionCommissionSourceTopUpRebate, rebate.Id).
		First(&discoveredLedger).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	needsWithdrawal := discoveredLedger.Status == PromotionCommissionStatusWithdrawing ||
		discoveredLedger.Status == PromotionCommissionStatusWithdrawn
	if !needsWithdrawal {
		var lockedLedger PromotionCommissionLedger
		if err := lockForUpdate(tx).Where("id = ?", discoveredLedger.Id).First(&lockedLedger).Error; err != nil {
			return nil, err
		}
		if lockedLedger.Status == PromotionCommissionStatusWithdrawing || lockedLedger.Status == PromotionCommissionStatusWithdrawn {
			return nil, errPromotionRefundWithdrawalLockOrderChanged
		}
		return nil, nil
	}

	var discoveredItem PromotionWithdrawalItem
	err = tx.Where("ledger_id = ?", discoveredLedger.Id).Order("id DESC").First(&discoveredItem).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var lockedLedger PromotionCommissionLedger
		if lockErr := lockForUpdate(tx).Where("id = ?", discoveredLedger.Id).First(&lockedLedger).Error; lockErr != nil {
			return nil, lockErr
		}
		if lockedLedger.Status != PromotionCommissionStatusWithdrawing && lockedLedger.Status != PromotionCommissionStatusWithdrawn {
			return nil, nil
		}
		if itemErr := tx.Where("ledger_id = ?", lockedLedger.Id).Order("id DESC").First(&discoveredItem).Error; itemErr == nil {
			return nil, errPromotionRefundWithdrawalLockOrderChanged
		} else if !errors.Is(itemErr, gorm.ErrRecordNotFound) {
			return nil, itemErr
		}
		return nil, errors.New("withdrawing or withdrawn commission has no withdrawal item")
	}
	if err != nil {
		return nil, err
	}

	withdrawal, err := LockPromotionWithdrawalTx(tx, discoveredItem.WithdrawalId)
	if err != nil {
		return nil, err
	}
	_, lockedLedgers, err := validatePromotionWithdrawalLedgerIntegrityTx(tx, withdrawal)
	if err != nil {
		return nil, err
	}
	targetLocked := false
	for i := range lockedLedgers {
		if lockedLedgers[i].Id == discoveredLedger.Id {
			targetLocked = true
			break
		}
	}
	if !targetLocked {
		return nil, errPromotionRefundWithdrawalLockOrderChanged
	}
	var latestItem PromotionWithdrawalItem
	if err := tx.Where("ledger_id = ?", discoveredLedger.Id).Order("id DESC").First(&latestItem).Error; err != nil {
		return nil, err
	}
	if latestItem.WithdrawalId != withdrawal.Id {
		return nil, errPromotionRefundWithdrawalLockOrderChanged
	}
	return withdrawal, nil
}

func lockedPromotionWithdrawalForRefundLedgerTx(tx *gorm.DB, ledger *PromotionCommissionLedger,
	lockedWithdrawal *PromotionWithdrawal,
) (*PromotionWithdrawal, error) {
	var item PromotionWithdrawalItem
	if err := tx.Where("ledger_id = ?", ledger.Id).Order("id DESC").First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("withdrawing or withdrawn commission has no withdrawal item")
		}
		return nil, err
	}
	if lockedWithdrawal == nil || lockedWithdrawal.Id != item.WithdrawalId {
		return nil, errPromotionRefundWithdrawalLockOrderChanged
	}
	return lockedWithdrawal, nil
}

func reversePromotionBenefitsForRefundTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp, reverseFixedRewards bool,
	commissionWithdrawal *PromotionWithdrawal, fencedUserIds refundHoldFenceScope,
) (int64, int64, error) {
	// Any refund invalidates the top-up commission in full so a promoter cannot
	// retain cash from a payment that was later reduced. Fixed invitation/task
	// rewards are reversed only after a full refund or dispute.
	quotaDebt, cashDebt, err := reverseRefundedCommissionWithDebtTx(tx, refundCase, topUp, commissionWithdrawal, fencedUserIds)
	if err != nil {
		return 0, 0, err
	}
	if !reverseFixedRewards {
		return quotaDebt, cashDebt, nil
	}

	fixedDebt, err := reverseInvitationRewardWithDebtTx(tx, refundCase, topUp, fencedUserIds)
	if err != nil {
		return 0, 0, err
	}
	growthDebt, err := reverseGrowthRewardWithDebtTx(tx, refundCase, topUp, fencedUserIds)
	if err != nil {
		return 0, 0, err
	}
	if fixedDebt > math.MaxInt64-growthDebt || quotaDebt > math.MaxInt64-fixedDebt-growthDebt {
		return 0, 0, errors.New("promotion refund debt overflow")
	}
	return quotaDebt + fixedDebt + growthDebt, cashDebt, nil
}

func reverseRefundedCommissionWithDebtTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp,
	commissionWithdrawal *PromotionWithdrawal, fencedUserIds refundHoldFenceScope,
) (int64, int64, error) {
	var rebate InvitationRebate
	err := lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).First(&rebate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	refundCase.InvitationRebateId = rebate.Id

	var ledger PromotionCommissionLedger
	err = lockForUpdate(tx).
		Where("source_type = ? AND source_id = ?", PromotionCommissionSourceTopUpRebate, rebate.Id).
		First(&ledger).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, 0, markInvitationRebateReversedTx(tx, &rebate, refundCase)
	}
	if err != nil {
		return 0, 0, err
	}
	refundCase.CommissionLedgerId = ledger.Id
	if ledger.Status == PromotionCommissionStatusReversed {
		return 0, 0, markInvitationRebateReversedTx(tx, &rebate, refundCase)
	}

	var cashDebt int64
	if ledger.Status == PromotionCommissionStatusWithdrawing {
		withdrawal, err := lockedPromotionWithdrawalForRefundLedgerTx(tx, &ledger, commissionWithdrawal)
		if err != nil {
			return 0, 0, err
		}
		paid, uncertainPayoutDebt, err := cancelUnpaidWithdrawalForRefundTx(tx, refundCase, withdrawal, fencedUserIds)
		if err != nil {
			return 0, 0, err
		}
		cashDebt = uncertainPayoutDebt
		if paid {
			ledger.Status = PromotionCommissionStatusWithdrawn
		} else {
			ledger.Status = PromotionCommissionStatusSettled
		}
	}

	var quotaDebt int64
	switch ledger.Status {
	case PromotionCommissionStatusPending, PromotionCommissionStatusSettled:
		account := PromotionFundAccountCommissionPending
		if ledger.Status == PromotionCommissionStatusSettled {
			account = PromotionFundAccountCommissionAvailable
		}
		if ledger.NetAmountCents > 0 {
			transaction := &PromotionFundTransaction{
				TransactionKey: fmt.Sprintf("refund:%d:commission:%d", refundCase.Id, ledger.Id), Kind: PromotionFundKindReversal, UserId: ledger.UserId,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id, SourceKey: refundCase.RefundTradeNo,
				ExternalRef: refundCase.RefundTradeNo, Remark: "reverse refunded cash commission",
			}
			applyPromotionRefundFundActor(transaction, refundCase)
			var accrual PromotionFundTransaction
			accrualErr := tx.Select("id").Where("transaction_key = ?", fmt.Sprintf("commission:%d:accrued", ledger.Id)).First(&accrual).Error
			if errors.Is(accrualErr, gorm.ErrRecordNotFound) {
				accrualErr = tx.Select("id").Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:accrued", ledger.Id)).First(&accrual).Error
			}
			if accrualErr != nil && !errors.Is(accrualErr, gorm.ErrRecordNotFound) {
				return 0, 0, accrualErr
			}
			if accrualErr == nil {
				transaction.ReversesTransactionId = accrual.Id
			}
			legs := []PromotionFundTransactionLeg{{
				Account: account, Asset: PromotionFundAssetCash, Currency: ledger.Currency, Amount: -ledger.NetAmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
			}}
			if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
				return 0, 0, err
			}
		}
	case PromotionCommissionStatusTransferred:
		_, debt, err := recoverRefundQuotaFromUserTx(
			tx, refundCase, ledger.UserId, ledger.QuotaEquivalent, 0, 0,
			"promotion_commission_ledgers", ledger.Id,
			fmt.Sprintf("refund:%d:commission:%d", refundCase.Id, ledger.Id),
			"reverse converted cash commission", fencedUserIds,
		)
		if err != nil {
			return 0, 0, err
		}
		quotaDebt = debt
	case PromotionCommissionStatusWithdrawn:
		withdrawal, err := lockedPromotionWithdrawalForRefundLedgerTx(tx, &ledger, commissionWithdrawal)
		if err != nil {
			return 0, 0, err
		}
		paidJournalExists, err := ValidatePromotionWithdrawalPaidTransactionTx(tx, withdrawal)
		if err != nil {
			return 0, 0, err
		}
		if !paidJournalExists {
			return 0, 0, errors.New("withdrawn commission has no confirmed payout journal")
		}
		if ledger.NetAmountCents > 0 {
			if err := createCashRefundDebtTx(tx, refundCase, ledger.UserId, ledger.Currency, ledger.NetAmountCents,
				"promotion_commission_ledgers", ledger.Id, fencedUserIds); err != nil {
				return 0, 0, err
			}
			cashDebt = ledger.NetAmountCents
		}
	default:
		reviewReason := fmt.Sprintf("commission ledger state %q requires Root assessment", ledger.Status)
		if refundCase.Reason == "" {
			refundCase.Reason = reviewReason
		} else {
			refundCase.Reason += "; " + reviewReason
		}
		if err := placeRefundCaseOnHoldTx(tx, refundCase, topUp, fencedUserIds); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}

	previousStatus := ledger.Status
	now := common.GetTimestamp()
	result := tx.Model(&PromotionCommissionLedger{}).
		Where("id = ? AND status = ?", ledger.Id, previousStatus).
		Updates(map[string]interface{}{
			"status": PromotionCommissionStatusReversed, "cashable": false,
			"refund_trade_no": refundCase.RefundTradeNo, "reversal_amount_cents": ledger.NetAmountCents,
			"reversal_quota": ledger.QuotaEquivalent, "reversed_at": now, "remark": refundCase.Reason,
		})
	if result.Error != nil {
		return 0, 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, 0, errors.New("commission status changed during refund")
	}
	ledger.Status = PromotionCommissionStatusReversed
	ledger.Cashable = false
	ledger.RefundTradeNo = refundCase.RefundTradeNo
	ledger.ReversalAmountCents = ledger.NetAmountCents
	ledger.ReversalQuota = ledger.QuotaEquivalent
	ledger.ReversedAt = now
	if err := CreatePromotionCommissionEventTx(tx, &ledger, PromotionEventTypeCommissionReversed); err != nil {
		return 0, 0, err
	}
	if err := markInvitationRebateReversedTx(tx, &rebate, refundCase); err != nil {
		return 0, 0, err
	}
	return quotaDebt, cashDebt, nil
}

func markInvitationRebateReversedTx(tx *gorm.DB, rebate *InvitationRebate, refundCase *PromotionRefundCase) error {
	if rebate == nil || rebate.Id <= 0 || rebate.Status == InvitationRebateStatusReversed {
		return nil
	}
	now := common.GetTimestamp()
	result := tx.Model(&InvitationRebate{}).
		Where("id = ? AND status = ?", rebate.Id, rebate.Status).
		Updates(map[string]interface{}{
			"status": InvitationRebateStatusReversed, "refund_trade_no": refundCase.RefundTradeNo,
			"reversal_quota": rebate.RebateQuota, "reversed_at": now, "remark": refundCase.Reason,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("invitation rebate status changed during refund")
	}
	return nil
}

func cancelUnpaidWithdrawalForRefundTx(tx *gorm.DB, refundCase *PromotionRefundCase, withdrawal *PromotionWithdrawal,
	fencedUserIds refundHoldFenceScope,
) (bool, int64, error) {
	if withdrawal == nil || withdrawal.Id <= 0 {
		return false, 0, errors.New("withdrawing commission has no locked withdrawal")
	}
	paidJournalExists, err := ValidatePromotionWithdrawalPaidTransactionTx(tx, withdrawal)
	if err != nil {
		return false, 0, err
	}
	if paidJournalExists {
		if withdrawal.Status != PromotionWithdrawalStatusPaid && withdrawal.Status != PromotionWithdrawalStatusProcessing {
			return false, 0, ErrPromotionFundTransactionConflict
		}
		return true, 0, nil
	}
	if withdrawal.Status == PromotionWithdrawalStatusPaid {
		return false, 0, errors.New("paid withdrawal has no confirmed payout journal")
	}
	if withdrawal.Status != PromotionWithdrawalStatusPendingReview &&
		withdrawal.Status != PromotionWithdrawalStatusApproved &&
		withdrawal.Status != PromotionWithdrawalStatusProcessing {
		return false, 0, fmt.Errorf("withdrawal is %s and cannot be cancelled automatically", withdrawal.Status)
	}
	wasProcessing := withdrawal.Status == PromotionWithdrawalStatusProcessing

	items, ledgers, err := validatePromotionWithdrawalLedgerIntegrityTx(tx, withdrawal)
	if err != nil {
		return false, 0, err
	}
	ledgerIds := make([]int, 0, len(items))
	for _, withdrawalItem := range items {
		ledgerIds = append(ledgerIds, withdrawalItem.LedgerId)
	}
	for i := range ledgers {
		if ledgers[i].Status != PromotionCommissionStatusWithdrawing {
			return false, 0, errors.New("withdrawal commission status changed during refund")
		}
	}

	now := common.GetTimestamp()
	failureReason := "cancelled automatically because a source payment was refunded before payout confirmation"
	if wasProcessing {
		failureReason = "cancelled automatically after source refund; initiated external payout result is unknown and requires recovery review"
	}
	result := tx.Model(&PromotionWithdrawal{}).
		Where("id = ? AND status = ?", withdrawal.Id, withdrawal.Status).
		Updates(map[string]interface{}{
			"status": PromotionWithdrawalStatusFailed, "review_note": failureReason,
			"reviewed_at": now,
		})
	if result.Error != nil {
		return false, 0, result.Error
	}
	if result.RowsAffected != 1 {
		return false, 0, errors.New("withdrawal status changed during refund")
	}
	withdrawal.Status = PromotionWithdrawalStatusFailed
	withdrawal.ReviewNote = failureReason
	withdrawal.ReviewedAt = now

	result = tx.Model(&PromotionCommissionLedger{}).
		Where("id IN ? AND status = ?", ledgerIds, PromotionCommissionStatusWithdrawing).
		Update("status", PromotionCommissionStatusSettled)
	if result.Error != nil {
		return false, 0, result.Error
	}
	if result.RowsAffected != int64(len(items)) {
		return false, 0, errors.New("withdrawal commission status changed during refund")
	}
	legs := make([]PromotionFundTransactionLeg, 0, len(items)*2)
	for _, withdrawalItem := range items {
		legs = append(legs,
			PromotionFundTransactionLeg{
				Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash,
				Currency: withdrawal.Currency, Amount: -withdrawalItem.AmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: withdrawalItem.LedgerId,
			},
			PromotionFundTransactionLeg{
				Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash,
				Currency: withdrawal.Currency, Amount: withdrawalItem.AmountCents,
				SourceType: "promotion_commission_ledgers", SourceId: withdrawalItem.LedgerId,
			},
		)
	}
	transaction := &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("withdrawal:%d:released", withdrawal.Id),
		Kind:           PromotionFundKindCommissionWithdrawalReleased,
		UserId:         withdrawal.UserId,
		SourceType:     "promotion_withdrawals",
		SourceId:       withdrawal.Id,
		SourceKey:      refundCase.RefundTradeNo,
		ExternalRef:    refundCase.RefundTradeNo,
		Remark:         "release unpaid withdrawal after source refund",
		OccurredAt:     now,
	}
	applyPromotionRefundFundActor(transaction, refundCase)
	var reserve PromotionFundTransaction
	reserveErr := tx.Select("id").Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:reserved", withdrawal.Id)).First(&reserve).Error
	if errors.Is(reserveErr, gorm.ErrRecordNotFound) {
		reserveErr = tx.Select("id").Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_withdrawals:%d:reserved", withdrawal.Id)).First(&reserve).Error
	}
	if reserveErr != nil && !errors.Is(reserveErr, gorm.ErrRecordNotFound) {
		return false, 0, reserveErr
	}
	if reserveErr == nil {
		transaction.ReversesTransactionId = reserve.Id
	}
	if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
		return false, 0, err
	}
	if err := CreatePromotionWithdrawalOperationTx(tx, &PromotionWithdrawalOperation{
		WithdrawalId:      withdrawal.Id,
		Action:            PromotionWithdrawalActionCancelledByRefund,
		ActorType:         PromotionWithdrawalActorSystem,
		Note:              withdrawal.ReviewNote,
		ExternalReference: refundCase.RefundTradeNo,
		CreatedAt:         now,
	}); err != nil {
		return false, 0, err
	}
	if err := createPromotionWithdrawalStatusEventTx(tx, withdrawal, PromotionEventTypeCommissionWithdrawFailed,
		"Cash withdrawal payout failed", PromotionEventDirectionStatus, withdrawal.NetAmountCents, now); err != nil {
		return false, 0, err
	}
	if !wasProcessing {
		return false, 0, nil
	}
	if withdrawal.NetAmountCents <= 0 {
		return false, 0, errors.New("processing withdrawal has an invalid payout amount")
	}
	if err := createCashRefundDebtTx(tx, refundCase, withdrawal.UserId, withdrawal.Currency, withdrawal.NetAmountCents,
		"promotion_withdrawals", withdrawal.Id, fencedUserIds); err != nil {
		return false, 0, err
	}
	return false, withdrawal.NetAmountCents, nil
}

func reverseInvitationRewardWithDebtTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp, fencedUserIds refundHoldFenceScope) (int64, error) {
	var reward InvitationReward
	err := lockForUpdate(tx).
		Where("trigger_top_up_id = ? AND reward_type = ? AND status = ?", topUp.Id, InvitationRewardTypeFirstTopUp, InvitationRewardStatusSettled).
		First(&reward).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if reward.TransferredQuota < 0 || reward.TransferredQuota > reward.RewardQuota {
		return 0, errors.New("invitation reward transfer allocation is inconsistent")
	}
	referralQuota := reward.RewardQuota - reward.TransferredQuota
	referralQuotaReserve, err := invitationRewardReferralReserveTx(tx, &reward)
	if err != nil {
		return 0, err
	}
	_, debt, err := recoverRefundQuotaFromUserTx(
		tx, refundCase, reward.InviterId, reward.RewardQuota, referralQuota, referralQuotaReserve,
		"invitation_rewards", reward.Id,
		fmt.Sprintf("refund:%d:invitation_reward:%d", refundCase.Id, reward.Id),
		"reverse fixed invitation reward", fencedUserIds,
	)
	if err != nil {
		return 0, err
	}
	result := tx.Model(&InvitationReward{}).
		Where("id = ? AND status = ?", reward.Id, InvitationRewardStatusSettled).
		Updates(map[string]interface{}{
			"status": InvitationRewardStatusReversed,
			"remark": fmt.Sprintf("reversed by refund %s", refundCase.RefundTradeNo),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, errors.New("invitation reward status changed during refund")
	}
	return debt, nil
}

func reverseGrowthRewardWithDebtTx(tx *gorm.DB, refundCase *PromotionRefundCase, topUp *TopUp, fencedUserIds refundHoldFenceScope) (int64, error) {
	var remainingTopUps int64
	if err := tx.Model(&TopUp{}).
		Where("user_id = ? AND id <> ? AND purpose = ? AND status = ?", topUp.UserId, topUp.Id, TopUpPurposeAPIBalance, common.TopUpStatusSuccess).
		Where("refund_status IS NULL OR refund_status NOT IN ?", []string{TopUpRefundStatusFull, TopUpRefundStatusDisputed}).
		Count(&remainingTopUps).Error; err != nil {
		return 0, err
	}
	if remainingTopUps > 0 {
		return 0, nil
	}
	var reward GrowthReward
	err := lockForUpdate(tx).
		Where("user_id = ? AND item_code = ? AND status IN ?", topUp.UserId, GrowthRewardItemFirstTopUp, []string{GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
		First(&reward).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	_, debt, err := recoverRefundQuotaFromUserTx(
		tx, refundCase, reward.UserId, reward.RewardQuota, 0, 0,
		"growth_rewards", reward.Id,
		fmt.Sprintf("refund:%d:growth_reward:%d", refundCase.Id, reward.Id),
		"reverse first top-up task reward", fencedUserIds,
	)
	if err != nil {
		return 0, err
	}
	result := tx.Model(&GrowthReward{}).
		Where("id = ? AND status IN ?", reward.Id, []string{GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
		Updates(map[string]interface{}{
			"status": GrowthRewardStatusReversed,
			"remark": fmt.Sprintf("reversed by refund %s", refundCase.RefundTradeNo),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, errors.New("growth reward status changed during refund")
	}
	return debt, nil
}

// recoverRefundQuotaFromUserTx recovers a quota benefit from referral credit
// first when requested, then from API balance, and records any shortfall as a
// user-specific obligation.
// invitationRewardReferralReserveTx protects source-precise invitation credit
// while reversing a legacy reward whose transfer allocation was never stored.
// Legacy transfers consume the aggregate pool before canonical rewards, so
// only referral credit above this reserve can belong to the legacy pool.
func invitationRewardReferralReserveTx(tx *gorm.DB, reward *InvitationReward) (int, error) {
	if tx == nil || reward == nil || reward.Id <= 0 || reward.InviterId <= 0 {
		return 0, errors.New("invitation reward is required")
	}
	var issuedCount int64
	if err := tx.Model(&PromotionFundTransaction{}).
		Where("transaction_key = ?", fmt.Sprintf("invitation_reward:%d:issued", reward.Id)).
		Count(&issuedCount).Error; err != nil {
		return 0, err
	}
	if issuedCount != 0 {
		return 0, nil
	}

	var rewards []InvitationReward
	if err := tx.Select("id", "reward_quota", "transferred_quota").
		Where("inviter_id = ? AND status = ? AND reward_quota > transferred_quota",
			reward.InviterId, InvitationRewardStatusSettled).
		Order("settled_at ASC, id ASC").Find(&rewards).Error; err != nil {
		return 0, err
	}
	if len(rewards) == 0 {
		return 0, nil
	}
	rewardIds := make([]int, 0, len(rewards))
	transactionKeys := make([]string, 0, len(rewards))
	for i := range rewards {
		rewardIds = append(rewardIds, rewards[i].Id)
		transactionKeys = append(transactionKeys, fmt.Sprintf("invitation_reward:%d:issued", rewards[i].Id))
	}
	var issuedTransactions []PromotionFundTransaction
	if err := tx.Select("source_id").
		Where("user_id = ? AND source_type = ? AND source_id IN ? AND kind = ? AND transaction_key IN ?",
			reward.InviterId, "invitation_rewards", rewardIds, PromotionFundKindInvitationRewardIssued, transactionKeys).
		Find(&issuedTransactions).Error; err != nil {
		return 0, err
	}
	canonicalRewardIds := make(map[int]struct{}, len(issuedTransactions))
	for i := range issuedTransactions {
		canonicalRewardIds[issuedTransactions[i].SourceId] = struct{}{}
	}
	reserve := 0
	for i := range rewards {
		if _, canonical := canonicalRewardIds[rewards[i].Id]; !canonical {
			continue
		}
		if rewards[i].TransferredQuota < 0 || rewards[i].TransferredQuota > rewards[i].RewardQuota {
			return 0, errors.New("invitation reward transfer allocation is inconsistent")
		}
		available := rewards[i].RewardQuota - rewards[i].TransferredQuota
		if reserve > common.MaxQuota-available {
			return 0, errors.New("invitation reward referral reserve overflow")
		}
		reserve += available
	}
	return reserve, nil
}

func recoverRefundQuotaFromUserTx(tx *gorm.DB, refundCase *PromotionRefundCase, userId int, quota int, referralQuotaLimit int,
	referralQuotaReserve int,
	sourceType string, sourceId int, transactionKey string, remark string, fencedUserIds refundHoldFenceScope,
) (int, int64, error) {
	if quota <= 0 {
		return 0, 0, nil
	}
	if err := fencedUserIds.Ensure(userId); err != nil {
		return 0, 0, err
	}
	var user User
	if err := lockForUpdate(tx.Unscoped()).Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, 0, err
	}
	if referralQuotaReserve < 0 || referralQuotaReserve > user.AffQuota {
		return 0, 0, errors.New("invitation reward referral reserve exceeds the available balance")
	}
	referralDebit := referralQuotaLimit
	if referralDebit > quota {
		referralDebit = quota
	}
	availableReferralQuota := user.AffQuota - referralQuotaReserve
	if referralDebit > availableReferralQuota {
		referralDebit = availableReferralQuota
	}
	if referralDebit < 0 {
		referralDebit = 0
	}
	remaining := quota - referralDebit
	walletDebit := remaining
	if walletDebit > user.Quota {
		walletDebit = user.Quota
	}
	if walletDebit < 0 {
		walletDebit = 0
	}
	debt := int64(remaining - walletDebit)
	user.AffQuota -= referralDebit
	if referralQuotaLimit > 0 {
		historyDebit := quota
		if historyDebit > user.AffHistoryQuota {
			historyDebit = user.AffHistoryQuota
		}
		if historyDebit > 0 {
			user.AffHistoryQuota -= historyDebit
		}
	}
	user.Quota -= walletDebit
	if debt > 0 {
		if user.RefundDebtQuota > math.MaxInt64-debt {
			return 0, 0, errors.New("refund debt overflow")
		}
		user.RefundDebtQuota += debt
		obligation := &PromotionRefundObligation{
			ObligationKey: transactionKey + ":debt", RefundCaseId: refundCase.Id, UserId: user.Id,
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: debt,
			SourceType: sourceType, SourceId: sourceId,
		}
		if err := CreatePromotionRefundObligationTx(tx, obligation); err != nil {
			return 0, 0, err
		}
		user.RefundHold = true
	}
	if err := tx.Unscoped().Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"aff_quota": user.AffQuota, "aff_history": user.AffHistoryQuota, "quota": user.Quota,
		"refund_debt_quota": user.RefundDebtQuota, "refund_hold": user.RefundHold,
	}).Error; err != nil {
		return 0, 0, err
	}

	legs := make([]PromotionFundTransactionLeg, 0, 3)
	if referralDebit > 0 {
		balance := int64(user.AffQuota)
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota, Amount: -int64(referralDebit),
			SourceType: sourceType, SourceId: sourceId, BalanceAfter: &balance,
		})
	}
	if walletDebit > 0 {
		balance := int64(user.Quota)
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: -int64(walletDebit),
			SourceType: sourceType, SourceId: sourceId, BalanceAfter: &balance,
		})
	}
	if debt > 0 {
		balance := user.RefundDebtQuota
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetQuota, Amount: debt,
			SourceType: sourceType, SourceId: sourceId, BalanceAfter: &balance,
		})
	}
	if len(legs) > 0 {
		transaction := &PromotionFundTransaction{
			TransactionKey: transactionKey, Kind: PromotionFundKindReversal, UserId: user.Id,
			SourceType: "promotion_refund_cases", SourceId: refundCase.Id, SourceKey: refundCase.RefundTradeNo,
			ExternalRef: refundCase.RefundTradeNo, Remark: remark,
		}
		applyPromotionRefundFundActor(transaction, refundCase)
		originalKey := ""
		backfillKey := ""
		switch sourceType {
		case "invitation_rewards":
			originalKey = fmt.Sprintf("invitation_reward:%d:issued", sourceId)
			backfillKey = fmt.Sprintf("pfb:invitation_rewards:%d:issued", sourceId)
		case "growth_rewards":
			originalKey = fmt.Sprintf("growth_reward:%d:issued", sourceId)
			backfillKey = fmt.Sprintf("pfb:growth_rewards:%d:issued", sourceId)
		case "promotion_commission_ledgers":
			originalKey = fmt.Sprintf("commission:%d:transferred", sourceId)
			backfillKey = fmt.Sprintf("pfb:promotion_commission_ledgers:%d:transferred", sourceId)
		}
		if originalKey != "" {
			var original PromotionFundTransaction
			originalErr := tx.Select("id").Where("transaction_key = ?", originalKey).First(&original).Error
			if errors.Is(originalErr, gorm.ErrRecordNotFound) && backfillKey != "" {
				originalErr = tx.Select("id").Where("transaction_key = ?", backfillKey).First(&original).Error
			}
			if originalErr != nil && !errors.Is(originalErr, gorm.ErrRecordNotFound) {
				return 0, 0, originalErr
			}
			if originalErr == nil {
				transaction.ReversesTransactionId = original.Id
			}
		}
		if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
			return 0, 0, err
		}
	}
	return walletDebit + referralDebit, debt, nil
}

func createCashRefundDebtTx(tx *gorm.DB, refundCase *PromotionRefundCase, userId int, currency string, amount int64,
	sourceType string, sourceId int, fencedUserIds refundHoldFenceScope,
) error {
	if amount <= 0 {
		return nil
	}
	if err := fencedUserIds.Ensure(userId); err != nil {
		return err
	}
	var user User
	if err := lockForUpdate(tx.Unscoped()).Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	obligation := &PromotionRefundObligation{
		ObligationKey: fmt.Sprintf("refund:%d:%s:%d:cash", refundCase.Id, sourceType, sourceId),
		RefundCaseId:  refundCase.Id, UserId: user.Id, Account: PromotionFundAccountRefundDebt,
		Asset: PromotionFundAssetCash, Currency: currency, Amount: amount, SourceType: sourceType, SourceId: sourceId,
	}
	if err := CreatePromotionRefundObligationTx(tx, obligation); err != nil {
		return err
	}
	if err := tx.Unscoped().Model(&User{}).Where("id = ?", user.Id).Update("refund_hold", true).Error; err != nil {
		return err
	}
	transaction := &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("refund:%d:%s:%d:cash_debt", refundCase.Id, sourceType, sourceId),
		Kind:           PromotionFundKindReversal, UserId: user.Id, SourceType: "promotion_refund_cases", SourceId: refundCase.Id,
		SourceKey: refundCase.RefundTradeNo, ExternalRef: refundCase.RefundTradeNo,
		Remark: "record paid commission recovery debt",
	}
	applyPromotionRefundFundActor(transaction, refundCase)
	if sourceType == "promotion_withdrawals" {
		transaction.Remark = "record unconfirmed withdrawal payout recovery obligation"
	}
	if sourceType == "promotion_commission_ledgers" {
		var withdrawalItem PromotionWithdrawalItem
		itemErr := tx.Select("withdrawal_id").Where("ledger_id = ?", sourceId).Order("id DESC").First(&withdrawalItem).Error
		if itemErr != nil && !errors.Is(itemErr, gorm.ErrRecordNotFound) {
			return itemErr
		}
		if itemErr == nil {
			var payout PromotionFundTransaction
			payoutErr := tx.Select("id").Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawalItem.WithdrawalId)).First(&payout).Error
			if errors.Is(payoutErr, gorm.ErrRecordNotFound) {
				payoutErr = tx.Select("id").Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_withdrawals:%d:paid", withdrawalItem.WithdrawalId)).First(&payout).Error
			}
			if payoutErr != nil && !errors.Is(payoutErr, gorm.ErrRecordNotFound) {
				return payoutErr
			}
			if payoutErr == nil {
				transaction.ReversesTransactionId = payout.Id
			}
		}
	}
	legs := []PromotionFundTransactionLeg{{
		Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash, Currency: currency, Amount: amount,
		SourceType: sourceType, SourceId: sourceId,
	}}
	return CreatePromotionFundTransactionTx(tx, transaction, legs)
}

func reconcilePromotionRefundHoldFences(fencedUserIds refundHoldFenceScope) error {
	return fencedUserIds.Reconcile()
}
