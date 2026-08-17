package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type AdminPromotionRefundActionRequest struct {
	IdempotencyKey                    string  `json:"idempotency_key"`
	Action                            string  `json:"action"`
	ObligationId                      int     `json:"obligation_id"`
	UserId                            int     `json:"user_id"`
	TopUpId                           int     `json:"top_up_id"`
	Asset                             string  `json:"asset"`
	Currency                          string  `json:"currency"`
	Amount                            int64   `json:"amount"`
	ExternalRef                       string  `json:"external_ref"`
	Remark                            string  `json:"remark"`
	CommissionLedgerId                int     `json:"commission_ledger_id"`
	CommissionLedgerStatus            *string `json:"commission_ledger_status"`
	UserSubscriptionId                int     `json:"user_subscription_id"`
	ExpectedResponsibilityFingerprint string  `json:"expected_responsibility_fingerprint"`
}

type AdminPromotionRefundCaseCreateRequest struct {
	IdempotencyKey      string `json:"idempotency_key"`
	TradeNo             string `json:"trade_no"`
	ExternalReference   string `json:"external_ref"`
	IntakeSource        string `json:"intake_source"`
	Kind                string `json:"kind"`
	RefundedAmountMinor int64  `json:"refunded_amount_minor"`
	Currency            string `json:"currency"`
	AmountIsCumulative  bool   `json:"amount_is_cumulative"`
	Remark              string `json:"remark"`
}

func CreateAdminPromotionRefundCase(actorId int, actorRole int, req AdminPromotionRefundCaseCreateRequest) (*model.PromotionRefundCase, error) {
	refundCase, err := model.CreateAdminPromotionRefundCase(model.AdminPromotionRefundCaseInput{
		IdempotencyKey: req.IdempotencyKey, TradeNo: req.TradeNo, ExternalReference: req.ExternalReference,
		IntakeSource: req.IntakeSource, Kind: req.Kind, RefundedAmountMinor: req.RefundedAmountMinor,
		Currency: req.Currency, AmountIsCumulative: req.AmountIsCumulative, Remark: req.Remark,
		ActorId: actorId, ActorRole: actorRole,
	})
	if err != nil {
		return nil, err
	}
	if err := model.LoadPromotionRefundResponsibleUsers(model.DB, []*model.PromotionRefundCase{refundCase}); err != nil {
		return nil, err
	}
	if err := model.LoadPromotionRefundSubscriptionEntitlements(model.DB, []*model.PromotionRefundCase{refundCase}); err != nil {
		return nil, err
	}
	return refundCase, nil
}

func ListAdminPromotionRefundCases(pageInfo *common.PageInfo, status string) ([]*model.PromotionRefundCase, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
	if pageInfo.Page < 1 || pageInfo.PageSize < 1 || pageInfo.PageSize > 100 {
		return nil, 0, errors.New("refund case page must be positive and page size must be between 1 and 100")
	}
	maxInt := int(^uint(0) >> 1)
	if pageInfo.Page-1 > maxInt/pageInfo.PageSize {
		return nil, 0, errors.New("refund case page offset is too large")
	}
	offset := (pageInfo.Page - 1) * pageInfo.PageSize
	status = strings.TrimSpace(status)
	if status == "" {
		status = model.PromotionRefundCaseStatusPendingReview
	}
	if status != "all" && status != model.PromotionRefundCaseStatusPendingReview && status != model.PromotionRefundCaseStatusResolved {
		return nil, 0, errors.New("invalid refund case status")
	}

	query := model.DB.Model(&model.PromotionRefundCase{})
	if status != "all" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var refundCases []*model.PromotionRefundCase
	if err := query.Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(offset).
		Find(&refundCases).Error; err != nil {
		return nil, 0, err
	}
	caseIds := make([]int, 0, len(refundCases))
	caseById := make(map[int]*model.PromotionRefundCase, len(refundCases))
	for _, refundCase := range refundCases {
		caseIds = append(caseIds, refundCase.Id)
		caseById[refundCase.Id] = refundCase
		refundCase.Obligations = make([]*model.PromotionRefundObligation, 0)
		refundCase.Actions = make([]*model.PromotionRefundAction, 0)
	}
	if len(caseIds) == 0 {
		return refundCases, total, nil
	}
	var obligations []*model.PromotionRefundObligation
	if err := model.DB.Where("refund_case_id IN ?", caseIds).Order("id ASC").Find(&obligations).Error; err != nil {
		return nil, 0, err
	}
	for _, obligation := range obligations {
		if refundCase := caseById[obligation.RefundCaseId]; refundCase != nil {
			refundCase.Obligations = append(refundCase.Obligations, obligation)
		}
	}
	var actions []*model.PromotionRefundAction
	if err := model.DB.Where("refund_case_id IN ?", caseIds).Order("id ASC").Find(&actions).Error; err != nil {
		return nil, 0, err
	}
	for _, action := range actions {
		if refundCase := caseById[action.RefundCaseId]; refundCase != nil {
			refundCase.Actions = append(refundCase.Actions, action)
		}
	}
	if err := model.LoadPromotionRefundResponsibleUsers(model.DB, refundCases); err != nil {
		return nil, 0, err
	}
	if err := model.LoadPromotionRefundSubscriptionEntitlements(model.DB, refundCases); err != nil {
		return nil, 0, err
	}
	return refundCases, total, nil
}

func ApplyAdminPromotionRefundAction(id int, actorId int, actorRole int, req AdminPromotionRefundActionRequest) (*model.PromotionRefundCase, error) {
	refundCase, err := model.ApplyPromotionRefundRecoveryAction(model.PromotionRefundRecoveryActionInput{
		RefundCaseId: id, IdempotencyKey: req.IdempotencyKey, Action: req.Action,
		ObligationId: req.ObligationId, UserId: req.UserId, TopUpId: req.TopUpId,
		Asset: req.Asset, Currency: req.Currency, Amount: req.Amount, ActorId: actorId, ActorRole: actorRole,
		ExternalRef: req.ExternalRef, Remark: req.Remark,
		ExpectedCommissionLedgerId:        req.CommissionLedgerId,
		ExpectedCommissionLedgerStatus:    req.CommissionLedgerStatus,
		UserSubscriptionId:                req.UserSubscriptionId,
		ExpectedResponsibilityFingerprint: req.ExpectedResponsibilityFingerprint,
	})
	if err != nil {
		return nil, err
	}
	if err := model.LoadPromotionRefundResponsibleUsers(model.DB, []*model.PromotionRefundCase{refundCase}); err != nil {
		return nil, err
	}
	if err := model.LoadPromotionRefundSubscriptionEntitlements(model.DB, []*model.PromotionRefundCase{refundCase}); err != nil {
		return nil, err
	}
	return refundCase, nil
}
