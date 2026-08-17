package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ListPromotionFundTransactions returns immutable journal entries with their
// legs, newest occurrence first.
func ListPromotionFundTransactions(userId int, pageInfo *common.PageInfo) ([]*PromotionFundTransaction, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("user ID is required")
	}
	if pageInfo == nil || pageInfo.GetPage() < 1 || pageInfo.GetPageSize() < 1 || pageInfo.GetPageSize() > 100 {
		return nil, 0, errors.New("valid page info is required")
	}
	page := pageInfo.GetPage()
	pageSize := pageInfo.GetPageSize()
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return nil, 0, errors.New("promotion fund page offset is too large")
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := DB.Model(&PromotionFundTransaction{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var transactions []*PromotionFundTransaction
	err := DB.
		Preload("Legs", func(tx *gorm.DB) *gorm.DB {
			return tx.Order("id ASC")
		}).
		Where("user_id = ?", userId).
		Order("occurred_at DESC, id DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&transactions).Error
	return transactions, total, err
}
