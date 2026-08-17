package service

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	UserPromotionRefundStageUnderReview       = "under_review"
	UserPromotionRefundStageRepaymentRequired = "repayment_required"
	UserPromotionRefundStageFinalReview       = "final_review"
	UserPromotionRefundStageResolved          = "resolved"
)

type UserPromotionRefundCashDebt struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
}

// UserPromotionRefundCase is deliberately narrower than the administrative
// case. Provider references, local order IDs, other responsible users,
// evidence, free-form notes, actors, and obligation sources are never exposed.
type UserPromotionRefundCase struct {
	Reference        string                        `json:"reference"`
	Kind             string                        `json:"kind"`
	Status           string                        `json:"status"`
	Stage            string                        `json:"stage"`
	OutstandingQuota int64                         `json:"outstanding_quota"`
	OutstandingCash  []UserPromotionRefundCashDebt `json:"outstanding_cash"`
	CreatedAt        int64                         `json:"created_at"`
	ResolvedAt       int64                         `json:"resolved_at,omitempty"`
}

type UserPromotionRefundRecovery struct {
	Hold             bool                          `json:"hold"`
	OutstandingQuota int64                         `json:"outstanding_quota"`
	OutstandingCash  []UserPromotionRefundCashDebt `json:"outstanding_cash"`
	Page             int                           `json:"page"`
	PageSize         int                           `json:"page_size"`
	Total            int64                         `json:"total"`
	Items            []UserPromotionRefundCase     `json:"items"`
}

func GetUserPromotionRefundRecovery(userId int, pageInfo *common.PageInfo, status string) (*UserPromotionRefundRecovery, error) {
	if userId <= 0 || pageInfo == nil {
		return nil, errors.New("user and page info are required")
	}
	if pageInfo.Page < 1 || pageInfo.PageSize < 1 || pageInfo.PageSize > 100 {
		return nil, errors.New("refund case page must be positive and page size must be between 1 and 100")
	}
	maxInt := int(^uint(0) >> 1)
	if pageInfo.Page-1 > maxInt/pageInfo.PageSize {
		return nil, errors.New("refund case page offset is too large")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = model.PromotionRefundCaseStatusPendingReview
	}
	if status != "all" && status != model.PromotionRefundCaseStatusPendingReview && status != model.PromotionRefundCaseStatusResolved {
		return nil, errors.New("invalid refund case status")
	}

	var user model.User
	if err := model.DB.Select("id", "refund_hold", "refund_debt_quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}
	result := &UserPromotionRefundRecovery{
		Hold: user.RefundHold, OutstandingQuota: user.RefundDebtQuota,
		OutstandingCash: make([]UserPromotionRefundCashDebt, 0),
		Page:            pageInfo.Page, PageSize: pageInfo.PageSize, Items: make([]UserPromotionRefundCase, 0),
	}

	var openObligations []model.PromotionRefundObligation
	if err := model.DB.Where("user_id = ? AND status = ?", userId, model.PromotionRefundObligationStatusOpen).
		Order("id ASC").Find(&openObligations).Error; err != nil {
		return nil, err
	}
	cashByCurrency := make(map[string]int64)
	for i := range openObligations {
		obligation := &openObligations[i]
		outstanding := obligation.OutstandingAmount()
		if outstanding == 0 || obligation.Asset != model.PromotionFundAssetCash {
			continue
		}
		current := cashByCurrency[obligation.Currency]
		if current > math.MaxInt64-outstanding {
			return nil, errors.New("refund cash debt overflow")
		}
		cashByCurrency[obligation.Currency] = current + outstanding
	}
	result.OutstandingCash = publicRefundCashDebts(cashByCurrency)

	obligationCaseIds := model.DB.Model(&model.PromotionRefundObligation{}).
		Select("refund_case_id").Where("user_id = ?", userId)
	responsibilityCaseIds := model.DB.Model(&model.PromotionRefundCaseUser{}).
		Select("refund_case_id").Where("user_id = ?", userId)
	query := model.DB.Model(&model.PromotionRefundCase{}).
		Where("user_id = ? OR id IN (?) OR id IN (?)", userId, obligationCaseIds, responsibilityCaseIds)
	if status != "all" {
		query = query.Where("status = ?", status)
	}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var cases []model.PromotionRefundCase
	offset := (pageInfo.Page - 1) * pageInfo.PageSize
	if err := query.Order("id DESC").Limit(pageInfo.PageSize).Offset(offset).Find(&cases).Error; err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return result, nil
	}

	caseIds := make([]int, 0, len(cases))
	caseIndex := make(map[int]int, len(cases))
	caseStatus := make(map[int]*model.PromotionRefundCase, len(cases))
	result.Items = make([]UserPromotionRefundCase, 0, len(cases))
	for i := range cases {
		refundCase := &cases[i]
		item := UserPromotionRefundCase{
			Reference: fmt.Sprintf("RC-%06d", refundCase.Id), Kind: refundCase.Kind,
			Status: refundCase.Status, Stage: UserPromotionRefundStageFinalReview,
			OutstandingCash: make([]UserPromotionRefundCashDebt, 0),
			CreatedAt:       refundCase.CreatedAt, ResolvedAt: refundCase.ResolvedAt,
		}
		caseIds = append(caseIds, refundCase.Id)
		result.Items = append(result.Items, item)
		caseIndex[refundCase.Id] = len(result.Items) - 1
		caseStatus[refundCase.Id] = refundCase
	}

	var ownObligations []model.PromotionRefundObligation
	if err := model.DB.Where("user_id = ? AND refund_case_id IN ?", userId, caseIds).
		Order("id ASC").Find(&ownObligations).Error; err != nil {
		return nil, err
	}
	caseCash := make(map[int]map[string]int64, len(cases))
	for i := range ownObligations {
		obligation := &ownObligations[i]
		outstanding := obligation.OutstandingAmount()
		if outstanding == 0 {
			continue
		}
		index, ok := caseIndex[obligation.RefundCaseId]
		if !ok {
			continue
		}
		item := &result.Items[index]
		if obligation.Asset == model.PromotionFundAssetQuota {
			if item.OutstandingQuota > math.MaxInt64-outstanding {
				return nil, errors.New("refund quota debt overflow")
			}
			item.OutstandingQuota += outstanding
			continue
		}
		if obligation.Asset == model.PromotionFundAssetCash {
			if caseCash[obligation.RefundCaseId] == nil {
				caseCash[obligation.RefundCaseId] = make(map[string]int64)
			}
			current := caseCash[obligation.RefundCaseId][obligation.Currency]
			if current > math.MaxInt64-outstanding {
				return nil, errors.New("refund cash debt overflow")
			}
			caseCash[obligation.RefundCaseId][obligation.Currency] = current + outstanding
		}
	}
	for caseId, index := range caseIndex {
		item := &result.Items[index]
		item.OutstandingCash = publicRefundCashDebts(caseCash[caseId])
		refundCase := caseStatus[caseId]
		hasOutstanding := item.OutstandingQuota > 0 || len(item.OutstandingCash) > 0
		switch {
		case refundCase.Status == model.PromotionRefundCaseStatusResolved:
			item.Stage = UserPromotionRefundStageResolved
		case hasOutstanding:
			item.Stage = UserPromotionRefundStageRepaymentRequired
		case refundCase.RequiresRootReview:
			item.Stage = UserPromotionRefundStageUnderReview
		default:
			item.Stage = UserPromotionRefundStageFinalReview
		}
	}
	return result, nil
}

func publicRefundCashDebts(amounts map[string]int64) []UserPromotionRefundCashDebt {
	currencies := make([]string, 0, len(amounts))
	for currency, amount := range amounts {
		if amount > 0 {
			currencies = append(currencies, currency)
		}
	}
	sort.Strings(currencies)
	result := make([]UserPromotionRefundCashDebt, 0, len(currencies))
	for _, currency := range currencies {
		result = append(result, UserPromotionRefundCashDebt{Currency: currency, Amount: amounts[currency]})
	}
	return result
}
