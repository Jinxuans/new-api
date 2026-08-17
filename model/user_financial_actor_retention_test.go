package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHardDeletePreservesAdministratorIdentityReferencedByFinancialAudit(t *testing.T) {
	testCases := []struct {
		name           string
		createEvidence func(*testing.T, *User, *User)
	}{
		{
			name: "fund transaction actor",
			createEvidence: func(t *testing.T, admin *User, subject *User) {
				balanceAfter := int64(10)
				require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
					return CreatePromotionFundTransactionTx(tx, &PromotionFundTransaction{
						TransactionKey: "actor-retention-fund", Kind: PromotionFundKindAdminQuotaCredited,
						UserId: subject.Id, SourceType: "admin_quota_adjustments",
						ActorType: "admin", ActorId: admin.Id, ActorRef: admin.Username,
					}, []PromotionFundTransactionLeg{{
						Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
						Amount: 10, SourceType: "admin_quota_adjustments", BalanceAfter: &balanceAfter,
					}})
				}))
			},
		},
		{
			name: "refund action actor",
			createEvidence: func(t *testing.T, admin *User, subject *User) {
				require.NoError(t, DB.Create(&PromotionRefundAction{
					ActionKey: "actor-retention-refund-action", RefundCaseId: 10,
					UserId: subject.Id, Action: PromotionRefundActionWaive, ActorId: admin.Id,
				}).Error)
			},
		},
		{
			name: "refund case reviewer",
			createEvidence: func(t *testing.T, admin *User, subject *User) {
				require.NoError(t, DB.Create(&PromotionRefundCase{
					EventKey: "actor-retention-refund-case", UserId: subject.Id,
					Status: PromotionRefundCaseStatusResolved, ReviewerId: admin.Id,
				}).Error)
			},
		},
		{
			name: "withdrawal reviewer",
			createEvidence: func(t *testing.T, admin *User, subject *User) {
				require.NoError(t, DB.Create(&PromotionWithdrawal{
					UserId: subject.Id, Status: PromotionWithdrawalStatusPaid,
					Currency: "CNY", ReviewerId: admin.Id,
				}).Error)
			},
		},
		{
			name: "withdrawal operation actor",
			createEvidence: func(t *testing.T, admin *User, _ *User) {
				require.NoError(t, DB.Create(&PromotionWithdrawalOperation{
					WithdrawalId: 77, Action: PromotionWithdrawalActionApproved,
					ActorType: PromotionWithdrawalActorAdmin, ActorId: admin.Id,
				}).Error)
			},
		},
		{
			name: "growth submission reviewer",
			createEvidence: func(t *testing.T, admin *User, subject *User) {
				require.NoError(t, DB.Create(&GrowthSubmission{
					UserId: subject.Id, ItemCode: "actor-retention-task",
					Status: GrowthSubmissionStatusApproved, ReviewerId: admin.Id,
				}).Error)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			admin := &User{
				Username: "retained-admin-" + common.GetRandomString(8),
				AffCode:  "retained-admin-aff-" + common.GetRandomString(8),
				Role:     common.RoleAdminUser, Status: common.UserStatusEnabled,
			}
			subject := &User{
				Username: "financial-subject-" + common.GetRandomString(8),
				AffCode:  "financial-subject-aff-" + common.GetRandomString(8),
				Status:   common.UserStatusEnabled,
			}
			require.NoError(t, DB.Create(admin).Error)
			require.NoError(t, DB.Create(subject).Error)
			testCase.createEvidence(t, admin, subject)

			err := admin.HardDelete()
			require.ErrorIs(t, err, ErrUserFinancialHistory)
			var retained User
			require.NoError(t, DB.Unscoped().First(&retained, admin.Id).Error)
			assert.False(t, retained.DeletedAt.Valid)
		})
	}
}
