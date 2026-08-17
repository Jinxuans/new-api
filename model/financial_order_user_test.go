package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinancialOrderInsertRejectsMissingOrDeletedUser(t *testing.T) {
	testCases := []struct {
		name         string
		softDelete   bool
		createRecord func(int, string) error
	}{
		{
			name: "top-up missing user",
			createRecord: func(userId int, key string) error {
				return (&TopUp{
					UserId: userId, Purpose: TopUpPurposeAPIBalance,
					TradeNo: key, Status: common.TopUpStatusPending,
				}).Insert()
			},
		},
		{
			name:       "top-up soft deleted user",
			softDelete: true,
			createRecord: func(userId int, key string) error {
				return (&TopUp{
					UserId: userId, Purpose: TopUpPurposeAPIBalance,
					TradeNo: key, Status: common.TopUpStatusPending,
				}).Insert()
			},
		},
		{
			name: "subscription order missing user",
			createRecord: func(userId int, key string) error {
				return (&SubscriptionOrder{
					UserId: userId, PlanId: 1, TradeNo: key,
					Status: common.TopUpStatusPending,
				}).Insert()
			},
		},
		{
			name:       "subscription order soft deleted user",
			softDelete: true,
			createRecord: func(userId int, key string) error {
				return (&SubscriptionOrder{
					UserId: userId, PlanId: 1, TradeNo: key,
					Status: common.TopUpStatusPending,
				}).Insert()
			},
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			userId := 900_000 + index
			if testCase.softDelete {
				user := &User{
					Username: fmt.Sprintf("deleted-order-user-%d", index),
					AffCode:  fmt.Sprintf("deleted-order-aff-%d", index),
					Status:   common.UserStatusEnabled,
				}
				require.NoError(t, DB.Create(user).Error)
				userId = user.Id
				require.NoError(t, DB.Delete(user).Error)
			}

			err := testCase.createRecord(userId, fmt.Sprintf("missing-order-%d", index))
			require.Error(t, err)
			var topUpCount int64
			require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
			assert.Zero(t, topUpCount)
			var subscriptionOrderCount int64
			require.NoError(t, DB.Model(&SubscriptionOrder{}).Count(&subscriptionOrderCount).Error)
			assert.Zero(t, subscriptionOrderCount)
		})
	}
}

func TestHardDeleteUserIsBlockedAfterFinancialOrderCreation(t *testing.T) {
	testCases := []struct {
		name         string
		createRecord func(*testing.T, int)
	}{
		{
			name: "top-up order",
			createRecord: func(t *testing.T, userId int) {
				require.NoError(t, (&TopUp{
					UserId: userId, Purpose: TopUpPurposeAPIBalance,
					TradeNo: "hard-delete-topup-order", Status: common.TopUpStatusPending,
				}).Insert())
			},
		},
		{
			name: "subscription order",
			createRecord: func(t *testing.T, userId int) {
				require.NoError(t, (&SubscriptionOrder{
					UserId: userId, PlanId: 1, TradeNo: "hard-delete-subscription-order",
					Status: common.TopUpStatusPending,
				}).Insert())
			},
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			user := &User{
				Username: fmt.Sprintf("financial-order-owner-%d", index),
				AffCode:  fmt.Sprintf("financial-order-owner-aff-%d", index),
				Status:   common.UserStatusEnabled,
			}
			require.NoError(t, DB.Create(user).Error)
			testCase.createRecord(t, user.Id)

			err := user.HardDelete()
			require.ErrorIs(t, err, ErrUserFinancialHistory)
			var retained User
			require.NoError(t, DB.Unscoped().First(&retained, user.Id).Error)
			assert.False(t, retained.DeletedAt.Valid)
		})
	}
}
