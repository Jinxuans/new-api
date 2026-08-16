package service

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const maxPromotionRefundReviewNoteLength = 1000

func ListAdminPromotionRefundCases(pageInfo *common.PageInfo, status string) ([]*model.PromotionRefundCase, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
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
		Offset(pageInfo.GetStartIdx()).
		Find(&refundCases).Error; err != nil {
		return nil, 0, err
	}
	return refundCases, total, nil
}

// ResolvePromotionRefundCase records an operator's completed manual review.
// It deliberately does not infer or apply any wallet/commission adjustment.
func ResolvePromotionRefundCase(id int, reviewerId int, reviewNote string) (*model.PromotionRefundCase, error) {
	reviewNote = strings.TrimSpace(reviewNote)
	if id <= 0 || reviewerId <= 0 {
		return nil, errors.New("invalid refund case review")
	}
	if reviewNote == "" {
		return nil, errors.New("review note is required")
	}
	if utf8.RuneCountInString(reviewNote) > maxPromotionRefundReviewNoteLength {
		return nil, errors.New("review note cannot exceed 1000 characters")
	}

	now := common.GetTimestamp()
	result := model.DB.Model(&model.PromotionRefundCase{}).
		Where("id = ? AND status = ?", id, model.PromotionRefundCaseStatusPendingReview).
		Updates(map[string]interface{}{
			"status":      model.PromotionRefundCaseStatusResolved,
			"reviewer_id": reviewerId,
			"review_note": reviewNote,
			"resolved_at": now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		var refundCase model.PromotionRefundCase
		if err := model.DB.Select("id", "status").Where("id = ?", id).First(&refundCase).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New("refund case not found")
			}
			return nil, err
		}
		return nil, errors.New("refund case already resolved")
	}

	var refundCase model.PromotionRefundCase
	if err := model.DB.Where("id = ?", id).First(&refundCase).Error; err != nil {
		return nil, err
	}
	return &refundCase, nil
}
