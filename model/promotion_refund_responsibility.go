package model

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func recordPromotionRefundCaseUsersTx(tx *gorm.DB, refundCaseId int, userIds ...int) error {
	if tx == nil || refundCaseId <= 0 || len(userIds) == 0 {
		return errors.New("refund case users and transaction are required")
	}
	uniqueUserIds := make(map[int]struct{}, len(userIds))
	orderedUserIds := make([]int, 0, len(userIds))
	for _, userId := range userIds {
		if userId <= 0 {
			return errors.New("refund case user is invalid")
		}
		if _, exists := uniqueUserIds[userId]; exists {
			continue
		}
		uniqueUserIds[userId] = struct{}{}
		orderedUserIds = append(orderedUserIds, userId)
	}
	sort.Ints(orderedUserIds)
	now := common.GetTimestamp()
	caseUsers := make([]PromotionRefundCaseUser, 0, len(orderedUserIds))
	for _, userId := range orderedUserIds {
		caseUsers = append(caseUsers, PromotionRefundCaseUser{
			RefundCaseId: refundCaseId,
			UserId:       userId,
			CreatedAt:    now,
		})
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "refund_case_id"}, {Name: "user_id"}},
		DoNothing: true,
	}).Create(&caseUsers).Error
}

func markPromotionRefundResponsibilityIntegrityError(refundCase *PromotionRefundCase, message string) {
	if refundCase == nil || message == "" {
		return
	}
	refundCase.RequiresRootReview = true
	if refundCase.ResponsibilityIntegrityError == "" {
		refundCase.ResponsibilityIntegrityError = message
	} else if !strings.Contains(refundCase.ResponsibilityIntegrityError, message) {
		refundCase.ResponsibilityIntegrityError += "; " + message
	}
	warning := "Responsibility integrity check failed: " + message
	if strings.TrimSpace(refundCase.Reason) == "" {
		refundCase.Reason = warning
	} else if !strings.Contains(refundCase.Reason, warning) {
		refundCase.Reason += "\n" + warning
	}
}

// LoadPromotionRefundResponsibleUsers attaches only users whose responsibility
// is supported by the persisted refund, top-up, rebate, and commission links.
// The recovery mutation performs the same relationship checks again.
func LoadPromotionRefundResponsibleUsers(db *gorm.DB, refundCases []*PromotionRefundCase) error {
	if db == nil {
		return errors.New("database is required")
	}
	topUpIds := make([]int, 0, len(refundCases))
	rebateIds := make([]int, 0, len(refundCases))
	caseLedgerIds := make([]int, 0, len(refundCases))
	markIntegrityError := markPromotionRefundResponsibilityIntegrityError
	for _, refundCase := range refundCases {
		if refundCase == nil {
			continue
		}
		refundCase.ResponsibleUsers = make([]PromotionRefundResponsibleUser, 0, 2)
		refundCase.CommissionLedgerStatus = ""
		refundCase.CommissionReconciliationRequired = false
		refundCase.ResponsibilityIntegrityError = ""
		if refundCase.TopUpId == 0 && refundCase.TradeNo != "" && refundCase.Provider != "" {
			var candidates []TopUp
			if err := db.Select("id", "user_id", "trade_no", "payment_provider").
				Where("trade_no = ? AND payment_provider = ?", refundCase.TradeNo, refundCase.Provider).
				Order("id ASC").Limit(2).Find(&candidates).Error; err != nil {
				return err
			}
			if len(candidates) == 1 && candidates[0].UserId > 0 &&
				(refundCase.UserId == 0 || refundCase.UserId == candidates[0].UserId) {
				refundCase.TopUpId = candidates[0].Id
				refundCase.UserId = candidates[0].UserId
			}
		}
		if refundCase.TopUpId == 0 {
			markIntegrityError(refundCase, "The refund case is not linked to a unique top-up.")
		}
		if refundCase.TopUpId > 0 {
			topUpIds = append(topUpIds, refundCase.TopUpId)
		}
		if refundCase.InvitationRebateId > 0 {
			rebateIds = append(rebateIds, refundCase.InvitationRebateId)
		}
		if refundCase.CommissionLedgerId > 0 {
			caseLedgerIds = append(caseLedgerIds, refundCase.CommissionLedgerId)
		}
	}

	topUpById := make(map[int]TopUp, len(topUpIds))
	if len(topUpIds) > 0 {
		var topUps []TopUp
		if err := db.Select("id", "user_id", "trade_no", "payment_provider").
			Where("id IN ?", topUpIds).Find(&topUps).Error; err != nil {
			return err
		}
		for i := range topUps {
			topUpById[topUps[i].Id] = topUps[i]
		}
	}

	rebateById := make(map[int]InvitationRebate, len(rebateIds))
	rebateByTopUpId := make(map[int]InvitationRebate, len(topUpIds))
	if len(topUpIds) > 0 || len(rebateIds) > 0 {
		var rebates []InvitationRebate
		query := db.Select(
			"id", "inviter_id", "invitee_id", "top_up_id", "trade_no", "rebate_amount_minor",
			"rebate_quota", "rebate_currency", "status",
		)
		if len(topUpIds) > 0 {
			query = query.Where("top_up_id IN ?", topUpIds)
			if len(rebateIds) > 0 {
				query = query.Or("id IN ?", rebateIds)
			}
		} else {
			query = query.Where("id IN ?", rebateIds)
		}
		if err := query.Find(&rebates).Error; err != nil {
			return err
		}
		for i := range rebates {
			rebateById[rebates[i].Id] = rebates[i]
			rebateByTopUpId[rebates[i].TopUpId] = rebates[i]
		}
	}
	invitationRewardByTopUpId := make(map[int]InvitationReward, len(topUpIds))
	if len(topUpIds) > 0 {
		var rewards []InvitationReward
		if err := db.Select("id", "inviter_id", "invitee_id", "trigger_top_up_id", "reward_quota", "transferred_quota", "status").
			Where("trigger_top_up_id IN ? AND reward_type = ? AND status = ?",
				topUpIds, InvitationRewardTypeFirstTopUp, InvitationRewardStatusSettled).
			Find(&rewards).Error; err != nil {
			return err
		}
		for i := range rewards {
			invitationRewardByTopUpId[rewards[i].TriggerTopUpId] = rewards[i]
		}
	}

	linkedRebateIds := make([]int, 0, len(rebateById))
	for rebateId := range rebateById {
		linkedRebateIds = append(linkedRebateIds, rebateId)
	}
	ledgerById := make(map[int]PromotionCommissionLedger, len(caseLedgerIds))
	ledgerByRebateId := make(map[int]PromotionCommissionLedger, len(linkedRebateIds))
	if len(linkedRebateIds) > 0 || len(caseLedgerIds) > 0 {
		var ledgers []PromotionCommissionLedger
		query := db.Select(
			"id", "user_id", "invitee_id", "source_type", "source_id", "source_trade_no",
			"net_amount_cents", "quota_equivalent", "currency", "status",
		)
		if len(linkedRebateIds) > 0 {
			query = query.Where("source_type = ? AND source_id IN ?", PromotionCommissionSourceTopUpRebate, linkedRebateIds)
			if len(caseLedgerIds) > 0 {
				query = query.Or("id IN ?", caseLedgerIds)
			}
		} else {
			query = query.Where("id IN ?", caseLedgerIds)
		}
		if err := query.Find(&ledgers).Error; err != nil {
			return err
		}
		for i := range ledgers {
			ledgerById[ledgers[i].Id] = ledgers[i]
			if ledgers[i].SourceType == PromotionCommissionSourceTopUpRebate {
				ledgerByRebateId[ledgers[i].SourceId] = ledgers[i]
			}
		}
	}

	verifiedTopUpByCase := make(map[*PromotionRefundCase]TopUp, len(refundCases))
	verifiedRebateByCase := make(map[*PromotionRefundCase]InvitationRebate, len(refundCases))
	verifiedLedgerByCase := make(map[*PromotionRefundCase]PromotionCommissionLedger, len(refundCases))
	verifiedInvitationRewardByCase := make(map[*PromotionRefundCase]InvitationReward, len(refundCases))
	userIds := make([]int, 0, len(refundCases)*2)
	seenUserIds := make(map[int]struct{}, len(refundCases)*2)
	addUserId := func(userId int) {
		if userId <= 0 {
			return
		}
		if _, exists := seenUserIds[userId]; exists {
			return
		}
		seenUserIds[userId] = struct{}{}
		userIds = append(userIds, userId)
	}
	for _, refundCase := range refundCases {
		if refundCase == nil {
			continue
		}
		if refundCase.TopUpId <= 0 {
			addUserId(refundCase.UserId)
			continue
		}
		topUp, exists := topUpById[refundCase.TopUpId]
		if !exists {
			markIntegrityError(refundCase, "The stored top-up link no longer exists.")
			continue
		}
		if topUp.UserId <= 0 || topUp.TradeNo == "" || topUp.TradeNo != refundCase.TradeNo ||
			refundCase.Provider == "" || topUp.PaymentProvider == "" || topUp.PaymentProvider != refundCase.Provider ||
			(refundCase.UserId > 0 && refundCase.UserId != topUp.UserId) {
			markIntegrityError(refundCase, "The stored top-up link does not match the refund case.")
			continue
		}
		verifiedTopUpByCase[refundCase] = topUp
		addUserId(topUp.UserId)

		rebate, hasRebate := rebateByTopUpId[topUp.Id]
		if !hasRebate && refundCase.InvitationRebateId > 0 {
			rebate, hasRebate = rebateById[refundCase.InvitationRebateId]
		}
		if !hasRebate && refundCase.InvitationRebateId > 0 {
			markIntegrityError(refundCase, "The stored invitation rebate link no longer exists.")
			continue
		}
		if hasRebate && (rebate.TopUpId != topUp.Id || rebate.InviteeId != topUp.UserId || rebate.InviterId <= 0 ||
			(rebate.TradeNo != "" && rebate.TradeNo != topUp.TradeNo) ||
			(refundCase.InvitationRebateId > 0 && refundCase.InvitationRebateId != rebate.Id)) {
			markIntegrityError(refundCase, "The stored invitation rebate link does not match the refunded top-up.")
			continue
		}
		if hasRebate && refundCase.InvitationRebateId == 0 {
			refundCase.InvitationRebateId = rebate.Id
		}
		if hasRebate {
			verifiedRebateByCase[refundCase] = rebate
			addUserId(rebate.InviterId)
		}
		if reward, hasReward := invitationRewardByTopUpId[topUp.Id]; hasReward {
			if reward.InviteeId != topUp.UserId || reward.InviterId <= 0 {
				markIntegrityError(refundCase, "A linked invitation reward does not match the refunded top-up.")
				continue
			}
			verifiedInvitationRewardByCase[refundCase] = reward
			addUserId(reward.InviterId)
		}

		ledger, hasLedger := PromotionCommissionLedger{}, false
		if hasRebate {
			ledger, hasLedger = ledgerByRebateId[rebate.Id]
		}
		if !hasLedger && refundCase.CommissionLedgerId > 0 {
			ledger, hasLedger = ledgerById[refundCase.CommissionLedgerId]
		}
		if !hasLedger {
			if refundCase.CommissionLedgerId > 0 {
				markIntegrityError(refundCase, "The stored commission ledger link no longer exists.")
			}
			continue
		}
		if ledger.UserId <= 0 ||
			(hasRebate && (ledger.UserId != rebate.InviterId || ledger.SourceType != PromotionCommissionSourceTopUpRebate || ledger.SourceId != rebate.Id)) ||
			(!hasRebate && (ledger.InviteeId != topUp.UserId || ledger.SourceTradeNo == "" || ledger.SourceTradeNo != topUp.TradeNo)) ||
			(ledger.InviteeId > 0 && ledger.InviteeId != topUp.UserId) ||
			(ledger.SourceTradeNo != "" && ledger.SourceTradeNo != topUp.TradeNo) ||
			(refundCase.CommissionLedgerId > 0 && refundCase.CommissionLedgerId != ledger.Id) {
			markIntegrityError(refundCase, "The stored commission ledger link does not match the refunded top-up.")
			continue
		}
		if refundCase.CommissionLedgerId == 0 {
			refundCase.CommissionLedgerId = ledger.Id
		}
		verifiedLedgerByCase[refundCase] = ledger
		addUserId(ledger.UserId)
	}

	usernameByUserId := make(map[int]string, len(userIds))
	if len(userIds) > 0 {
		var users []User
		if err := db.Unscoped().Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
			return err
		}
		for i := range users {
			usernameByUserId[users[i].Id] = users[i].Username
		}
	}

	for _, refundCase := range refundCases {
		if refundCase == nil {
			continue
		}
		principalUserId := refundCase.UserId
		isTopUpUser := false
		if topUp, exists := verifiedTopUpByCase[refundCase]; exists {
			principalUserId = topUp.UserId
			isTopUpUser = true
		} else if refundCase.TopUpId > 0 {
			continue
		}
		candidateByUserId := make(map[int]int, 2)
		if username, exists := usernameByUserId[principalUserId]; exists {
			candidateByUserId[principalUserId] = len(refundCase.ResponsibleUsers)
			refundCase.ResponsibleUsers = append(refundCase.ResponsibleUsers, PromotionRefundResponsibleUser{
				UserId: principalUserId, Username: username, IsTopUpUser: isTopUpUser,
			})
		} else if principalUserId > 0 {
			markIntegrityError(refundCase, "The principal responsible user no longer exists.")
		}
		if rebate, hasRebate := verifiedRebateByCase[refundCase]; hasRebate {
			candidateIndex, exists := candidateByUserId[rebate.InviterId]
			if !exists {
				username, userExists := usernameByUserId[rebate.InviterId]
				if userExists {
					candidateIndex = len(refundCase.ResponsibleUsers)
					candidateByUserId[rebate.InviterId] = candidateIndex
					refundCase.ResponsibleUsers = append(refundCase.ResponsibleUsers, PromotionRefundResponsibleUser{
						UserId: rebate.InviterId, Username: username,
					})
					exists = true
				} else {
					markIntegrityError(refundCase, "The invitation rebate recipient no longer exists.")
				}
			}
			if exists {
				candidate := &refundCase.ResponsibleUsers[candidateIndex]
				candidate.IsRebateRecipient = true
				candidate.InvitationRebateId = rebate.Id
				candidate.RebateAmountMinor = rebate.RebateAmountMinor
				candidate.RebateQuota = rebate.RebateQuota
				candidate.RebateCurrency = rebate.RebateCurrency
			}
		}
		if reward, hasReward := verifiedInvitationRewardByCase[refundCase]; hasReward {
			candidateIndex, exists := candidateByUserId[reward.InviterId]
			if !exists {
				username, userExists := usernameByUserId[reward.InviterId]
				if userExists {
					candidateIndex = len(refundCase.ResponsibleUsers)
					candidateByUserId[reward.InviterId] = candidateIndex
					refundCase.ResponsibleUsers = append(refundCase.ResponsibleUsers, PromotionRefundResponsibleUser{
						UserId: reward.InviterId, Username: username,
					})
					exists = true
				} else {
					markIntegrityError(refundCase, "The invitation reward recipient no longer exists.")
				}
			}
			if exists {
				candidate := &refundCase.ResponsibleUsers[candidateIndex]
				candidate.IsInvitationRewardRecipient = true
				candidate.InvitationRewardId = reward.Id
				candidate.InvitationRewardQuota = reward.RewardQuota
				candidate.InvitationTransferredQuota = reward.TransferredQuota
			}
		}
		ledger, hasLedger := verifiedLedgerByCase[refundCase]
		if !hasLedger {
			continue
		}
		refundCase.CommissionLedgerStatus = ledger.Status
		if refundCase.Status == PromotionRefundCaseStatusPendingReview && !isKnownPromotionCommissionStatus(ledger.Status) {
			quarantined := false
			for _, action := range refundCase.Actions {
				if action != nil && action.Action == PromotionRefundActionQuarantineUnknownCommission &&
					action.CommissionLedgerId == ledger.Id && action.CommissionLedgerStatus == ledger.Status {
					quarantined = true
					break
				}
			}
			refundCase.CommissionReconciliationRequired = !quarantined
			if !quarantined {
				refundCase.RequiresRootReview = true
			}
		}
		candidateIndex, exists := candidateByUserId[ledger.UserId]
		if !exists {
			username, userExists := usernameByUserId[ledger.UserId]
			if !userExists {
				markIntegrityError(refundCase, "The commission recipient no longer exists.")
				continue
			}
			candidateIndex = len(refundCase.ResponsibleUsers)
			refundCase.ResponsibleUsers = append(refundCase.ResponsibleUsers, PromotionRefundResponsibleUser{
				UserId: ledger.UserId, Username: username,
			})
		}
		candidate := &refundCase.ResponsibleUsers[candidateIndex]
		candidate.IsCommissionRecipient = true
		candidate.CommissionLedgerId = ledger.Id
		candidate.CommissionAmountMinor = ledger.NetAmountCents
		candidate.CommissionQuota = ledger.QuotaEquivalent
		candidate.CommissionCurrency = ledger.Currency
	}
	return nil
}
