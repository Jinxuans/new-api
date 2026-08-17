package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PromotionFundKindRootInitialQuotaGranted = "root_initial_quota_granted"
	PromotionFundSourceSystemSetup           = "system_setup"
)

// CreateInitialRootUser creates the first root account and the immutable
// evidence for its opening API balance in one transaction.
func CreateInitialRootUser(user *User) error {
	if user == nil || user.Role != common.RoleRootUser {
		return errors.New("valid root user is required")
	}
	if user.Quota < 0 || user.Quota >= common.MaxQuota {
		return errors.New("initial root quota exceeds supported range")
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		if user.Quota == 0 {
			return nil
		}

		balanceAfter := int64(user.Quota)
		sourceKey := fmt.Sprintf("%s:root_user:%d", PromotionFundSourceSystemSetup, user.Id)
		return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
			TransactionKey: sourceKey + ":initial_quota",
			Kind:           PromotionFundKindRootInitialQuotaGranted,
			UserId:         user.Id,
			SourceType:     PromotionFundSourceSystemSetup,
			SourceId:       user.Id,
			SourceKey:      sourceKey,
			ActorType:      "system",
			OccurredAt:     user.CreatedAt,
		}, []PromotionFundTransactionLeg{{
			Account:      PromotionFundAccountAPIBalance,
			Asset:        PromotionFundAssetQuota,
			Amount:       int64(user.Quota),
			SourceType:   PromotionFundSourceSystemSetup,
			SourceId:     user.Id,
			BalanceAfter: &balanceAfter,
		}})
	})
}
