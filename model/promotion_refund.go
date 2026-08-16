package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PromotionRefundKindFull    = "full_refund"
	PromotionRefundKindPartial = "partial_refund"
	PromotionRefundKindDispute = "dispute"

	PromotionRefundCaseStatusPendingReview = "pending_review"
	PromotionRefundCaseStatusResolved      = "resolved"

	TopUpRefundStatusPartial  = "partial"
	TopUpRefundStatusFull     = "full"
	TopUpRefundStatusDisputed = "disputed"
)

type PromotionRefundCase struct {
	Id                  int    `json:"id"`
	EventKey            string `json:"event_key" gorm:"type:varchar(96);uniqueIndex"`
	Provider            string `json:"provider" gorm:"type:varchar(50);index"`
	TradeNo             string `json:"trade_no" gorm:"type:varchar(255);index"`
	RefundTradeNo       string `json:"refund_trade_no" gorm:"type:varchar(255);index"`
	Kind                string `json:"kind" gorm:"type:varchar(32);index"`
	PaidAmountMinor     int64  `json:"paid_amount_minor"`
	RefundedAmountMinor int64  `json:"refunded_amount_minor"`
	Currency            string `json:"currency" gorm:"type:varchar(3);index"`
	TopUpId             int    `json:"top_up_id" gorm:"index"`
	InvitationRebateId  int    `json:"invitation_rebate_id" gorm:"index"`
	CommissionLedgerId  int    `json:"commission_ledger_id" gorm:"index"`
	Status              string `json:"status" gorm:"type:varchar(32);index"`
	Reason              string `json:"reason" gorm:"type:text"`
	ReviewerId          int    `json:"reviewer_id" gorm:"index"`
	ReviewNote          string `json:"review_note" gorm:"type:text"`
	CreatedAt           int64  `json:"created_at" gorm:"index"`
	ResolvedAt          int64  `json:"resolved_at" gorm:"index"`
}

type PromotionRefundInput struct {
	Provider            string
	TradeNo             string
	RefundTradeNo       string
	Kind                string
	PaidAmountMinor     int64
	RefundedAmountMinor int64
	Currency            string
	Remark              string
}

// HandlePromotionRefund persists every verified refund/dispute before it is
// acknowledged. Safe full reversals resolve automatically; partial refunds
// and commissions already in a withdrawal remain pending for an operator.
func HandlePromotionRefund(input PromotionRefundInput) (*PromotionRefundCase, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.TradeNo = strings.TrimSpace(input.TradeNo)
	input.RefundTradeNo = strings.TrimSpace(input.RefundTradeNo)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Provider == "" || input.TradeNo == "" || input.RefundTradeNo == "" {
		return nil, errors.New("refund provider, trade number, and refund number are required")
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
	if input.PaidAmountMinor > 0 && input.RefundedAmountMinor > input.PaidAmountMinor {
		return nil, errors.New("refunded amount exceeds paid amount")
	}

	eventKey := fmt.Sprintf("%s:%s", input.Provider, common.Sha1([]byte(input.TradeNo+":"+input.RefundTradeNo+":"+input.Kind)))
	now := common.GetTimestamp()
	refundCase := &PromotionRefundCase{
		EventKey:            eventKey,
		Provider:            input.Provider,
		TradeNo:             input.TradeNo,
		RefundTradeNo:       input.RefundTradeNo,
		Kind:                input.Kind,
		PaidAmountMinor:     input.PaidAmountMinor,
		RefundedAmountMinor: input.RefundedAmountMinor,
		Currency:            input.Currency,
		Status:              PromotionRefundCaseStatusPendingReview,
		Reason:              strings.TrimSpace(input.Remark),
		CreatedAt:           now,
	}
	cacheUserIds := map[int]struct{}{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_key"}},
			DoNothing: true,
		}).Create(refundCase)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return tx.Where("event_key = ?", eventKey).First(refundCase).Error
		}

		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where("trade_no = ?", input.TradeNo).First(topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				refundCase.Reason = "local top-up order not found; manual review required"
				return tx.Model(refundCase).Update("reason", refundCase.Reason).Error
			}
			return err
		}
		refundCase.TopUpId = topUp.Id
		cacheUserIds[topUp.UserId] = struct{}{}
		var invitee User
		if err := tx.Select("inviter_id").Where("id = ?", topUp.UserId).First(&invitee).Error; err == nil && invitee.InviterId > 0 {
			cacheUserIds[invitee.InviterId] = struct{}{}
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if topUp.PaymentProvider != input.Provider {
			refundCase.Reason = fmt.Sprintf("payment provider mismatch: stored=%s callback=%s", topUp.PaymentProvider, input.Provider)
			return tx.Model(refundCase).Updates(map[string]interface{}{
				"top_up_id": topUp.Id,
				"reason":    refundCase.Reason,
			}).Error
		}

		if refundCase.PaidAmountMinor <= 0 && topUp.PaidAmountVerified {
			refundCase.PaidAmountMinor = topUp.PaidAmountMinor
		}
		if refundCase.Currency == "" && topUp.PaidAmountVerified {
			refundCase.Currency = topUp.PaidCurrency
		}
		if topUp.PaidAmountVerified && refundCase.Currency != topUp.PaidCurrency {
			refundCase.Reason = fmt.Sprintf("refund currency mismatch: stored=%s callback=%s", topUp.PaidCurrency, refundCase.Currency)
			return tx.Model(refundCase).Updates(map[string]interface{}{
				"top_up_id": topUp.Id,
				"reason":    refundCase.Reason,
			}).Error
		}
		if refundCase.PaidAmountMinor > 0 && refundCase.RefundedAmountMinor > refundCase.PaidAmountMinor {
			refundCase.Reason = "refunded amount exceeds verified paid amount"
			return tx.Model(refundCase).Updates(map[string]interface{}{
				"top_up_id":             topUp.Id,
				"paid_amount_minor":     refundCase.PaidAmountMinor,
				"refunded_amount_minor": refundCase.RefundedAmountMinor,
				"currency":              refundCase.Currency,
				"reason":                refundCase.Reason,
			}).Error
		}
		if input.Kind == PromotionRefundKindPartial {
			topUp.RefundStatus = TopUpRefundStatusPartial
			topUp.RefundedAmountMinor = input.RefundedAmountMinor
			topUp.RefundedAt = now
			result, err := reverseRefundedPromotionCommissionTx(tx, topUp, input.RefundTradeNo, input.Remark)
			if err != nil {
				return err
			}
			refundCase.InvitationRebateId = result.invitationRebateId
			refundCase.CommissionLedgerId = result.commissionLedgerId
			if result.inviterId > 0 {
				cacheUserIds[result.inviterId] = struct{}{}
			}
			refundCase.Reason = "partial refund invalidated the full cash commission; remaining rewards require review"
			if result.manualReason != "" {
				refundCase.Reason += "; " + result.manualReason
			}
			if err := tx.Save(topUp).Error; err != nil {
				return err
			}
			return tx.Model(refundCase).Updates(map[string]interface{}{
				"top_up_id":             topUp.Id,
				"paid_amount_minor":     refundCase.PaidAmountMinor,
				"refunded_amount_minor": refundCase.RefundedAmountMinor,
				"currency":              refundCase.Currency,
				"invitation_rebate_id":  refundCase.InvitationRebateId,
				"commission_ledger_id":  refundCase.CommissionLedgerId,
				"reason":                refundCase.Reason,
			}).Error
		}

		if input.Kind == PromotionRefundKindDispute {
			topUp.RefundStatus = TopUpRefundStatusDisputed
		} else {
			topUp.RefundStatus = TopUpRefundStatusFull
		}
		if refundCase.RefundedAmountMinor <= 0 {
			refundCase.RefundedAmountMinor = refundCase.PaidAmountMinor
		}
		topUp.RefundedAmountMinor = refundCase.RefundedAmountMinor
		topUp.RefundedAt = now
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		manualReasons := make([]string, 0, 2)
		commissionResult, err := reverseRefundedPromotionCommissionTx(tx, topUp, input.RefundTradeNo, input.Remark)
		if err != nil {
			return err
		}
		refundCase.InvitationRebateId = commissionResult.invitationRebateId
		refundCase.CommissionLedgerId = commissionResult.commissionLedgerId
		if commissionResult.inviterId > 0 {
			cacheUserIds[commissionResult.inviterId] = struct{}{}
		}
		if commissionResult.manualReason != "" {
			manualReasons = append(manualReasons, commissionResult.manualReason)
		}

		manualReason, err := reverseInvitationFirstTopUpRewardTx(tx, topUp, input.RefundTradeNo)
		if err != nil {
			return err
		}
		if manualReason != "" {
			manualReasons = append(manualReasons, manualReason)
		}
		manualReason, err = reverseGrowthFirstTopUpRewardTx(tx, topUp, input.RefundTradeNo)
		if err != nil {
			return err
		}
		if manualReason != "" {
			manualReasons = append(manualReasons, manualReason)
		}

		updates := map[string]interface{}{
			"top_up_id":             topUp.Id,
			"invitation_rebate_id":  refundCase.InvitationRebateId,
			"commission_ledger_id":  refundCase.CommissionLedgerId,
			"paid_amount_minor":     refundCase.PaidAmountMinor,
			"refunded_amount_minor": refundCase.RefundedAmountMinor,
			"currency":              refundCase.Currency,
		}
		if len(manualReasons) == 0 {
			refundCase.Status = PromotionRefundCaseStatusResolved
			refundCase.ResolvedAt = now
			updates["status"] = refundCase.Status
			updates["resolved_at"] = now
		} else {
			refundCase.Reason = strings.Join(manualReasons, "; ")
			updates["reason"] = refundCase.Reason
		}
		return tx.Model(refundCase).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	for userId := range cacheUserIds {
		_ = InvalidateUserCache(userId)
	}
	return refundCase, nil
}

type promotionRefundCommissionResult struct {
	invitationRebateId int
	commissionLedgerId int
	inviterId          int
	manualReason       string
}

func reverseRefundedPromotionCommissionTx(tx *gorm.DB, topUp *TopUp, refundTradeNo string, remark string) (promotionRefundCommissionResult, error) {
	result := promotionRefundCommissionResult{}
	var rebate InvitationRebate
	err := lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).First(&rebate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.invitationRebateId = rebate.Id
	result.inviterId = rebate.InviterId

	var ledger PromotionCommissionLedger
	ledgerErr := lockForUpdate(tx).
		Where("source_type = ? AND source_id = ?", PromotionCommissionSourceTopUpRebate, rebate.Id).
		First(&ledger).Error
	if ledgerErr != nil && !errors.Is(ledgerErr, gorm.ErrRecordNotFound) {
		return result, ledgerErr
	}
	if ledgerErr == nil {
		result.commissionLedgerId = ledger.Id
		if ledger.Status == PromotionCommissionStatusWithdrawing || ledger.Status == PromotionCommissionStatusWithdrawn {
			if err := tx.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("cashable", false).Error; err != nil {
				return result, err
			}
			result.manualReason = fmt.Sprintf("commission ledger is %s", ledger.Status)
			return result, nil
		}
		if ledger.Status == PromotionCommissionStatusTransferred && ledger.QuotaEquivalent > 0 {
			var inviter User
			if err := lockForUpdate(tx).Select("id", "quota").Where("id = ?", ledger.UserId).First(&inviter).Error; err != nil {
				return result, err
			}
			if inviter.Quota < common.MinQuota+ledger.QuotaEquivalent {
				if err := tx.Model(&PromotionCommissionLedger{}).Where("id = ?", ledger.Id).Update("cashable", false).Error; err != nil {
					return result, err
				}
				result.manualReason = "transferred commission wallet balance is insufficient for automatic reversal"
				return result, nil
			}
		}
	}

	if _, err := ReverseInvitationRebateByTopUpTx(tx, topUp.Id, refundTradeNo, remark); err != nil {
		return result, err
	}
	return result, nil
}

func reverseInvitationFirstTopUpRewardTx(tx *gorm.DB, topUp *TopUp, refundTradeNo string) (string, error) {
	var reward InvitationReward
	err := lockForUpdate(tx).
		Where("trigger_top_up_id = ? AND reward_type = ? AND status = ?", topUp.Id, InvitationRewardTypeFirstTopUp, InvitationRewardStatusSettled).
		First(&reward).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var user User
	if err := lockForUpdate(tx).Where("id = ?", reward.InviterId).First(&user).Error; err != nil {
		return "", err
	}
	if user.AffHistoryQuota < reward.RewardQuota {
		return "fixed invitation reward history is lower than reversal", nil
	}
	affDebit := reward.RewardQuota
	if user.AffQuota < affDebit {
		affDebit = user.AffQuota
	}
	walletDebit := reward.RewardQuota - affDebit
	if walletDebit > 0 && user.Quota < common.MinQuota+walletDebit {
		return "fixed invitation reward reversal would exceed wallet range", nil
	}
	if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"aff_quota":   gorm.Expr("aff_quota - ?", affDebit),
		"aff_history": gorm.Expr("aff_history - ?", reward.RewardQuota),
		"quota":       gorm.Expr("quota - ?", walletDebit),
	}).Error; err != nil {
		return "", err
	}
	result := tx.Model(&InvitationReward{}).
		Where("id = ? AND status = ?", reward.Id, InvitationRewardStatusSettled).
		Updates(map[string]interface{}{
			"status": InvitationRewardStatusReversed,
			"remark": fmt.Sprintf("reversed by refund %s", refundTradeNo),
		})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "fixed invitation reward status changed", nil
	}
	return "", nil
}

func reverseGrowthFirstTopUpRewardTx(tx *gorm.DB, topUp *TopUp, refundTradeNo string) (string, error) {
	var remainingTopUps int64
	if err := tx.Model(&TopUp{}).
		Where("user_id = ? AND id <> ? AND status = ?", topUp.UserId, topUp.Id, common.TopUpStatusSuccess).
		Where("refund_status IS NULL OR refund_status NOT IN ?", []string{TopUpRefundStatusFull, TopUpRefundStatusDisputed}).
		Count(&remainingTopUps).Error; err != nil {
		return "", err
	}
	if remainingTopUps > 0 {
		return "", nil
	}
	var reward GrowthReward
	err := lockForUpdate(tx).
		Where("user_id = ? AND item_code = ? AND status IN ?", topUp.UserId, GrowthRewardItemFirstTopUp, []string{GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
		First(&reward).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var user User
	if err := lockForUpdate(tx).Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
		return "", err
	}
	if reward.RewardQuota > 0 && user.Quota < common.MinQuota+reward.RewardQuota {
		return "growth first-top-up reward reversal would exceed wallet range", nil
	}
	if reward.RewardQuota > 0 {
		if err := tx.Model(&User{}).Where("id = ?", user.Id).
			Update("quota", gorm.Expr("quota - ?", reward.RewardQuota)).Error; err != nil {
			return "", err
		}
	}
	result := tx.Model(&GrowthReward{}).
		Where("id = ? AND status IN ?", reward.Id, []string{GrowthRewardStatusSettled, GrowthRewardStatusTransferred}).
		Updates(map[string]interface{}{
			"status": GrowthRewardStatusReversed,
			"remark": fmt.Sprintf("reversed by refund %s", refundTradeNo),
		})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "growth first-top-up reward status changed", nil
	}
	return "", nil
}
