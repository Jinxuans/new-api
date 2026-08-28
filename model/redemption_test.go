package model

import (
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{
			name:      "no filters returns all rows",
			num:       10,
			wantTotal: 5,
			wantIds:   []int{5, 4, 3, 2, 1},
		},
		{
			name:      "keyword filters by name prefix",
			keyword:   "alpha",
			num:       10,
			wantTotal: 3,
			wantIds:   []int{3, 2, 1},
		},
		{
			name:      "enabled status excludes expired rows",
			status:    "1",
			num:       10,
			wantTotal: 2,
			wantIds:   []int{2, 1},
		},
		{
			name:      "expired status returns enabled expired rows",
			status:    "expired",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{3},
		},
		{
			name:      "disabled status",
			status:    "2",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{4},
		},
		{
			name:      "used status",
			status:    "3",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{5},
		},
		{
			name:      "pagination keeps unpaged total",
			startIdx:  1,
			num:       2,
			wantTotal: 5,
			wantIds:   []int{4, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(
		&Redemption{},
		&PromotionFundTransaction{},
		&PromotionFundTransactionLeg{},
	))
	require.NoError(t, DB.Exec("DELETE FROM promotion_fund_legs").Error)
	require.NoError(t, DB.Exec("DELETE FROM promotion_fund_transactions").Error)
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		require.NoError(t, DB.Exec("DELETE FROM promotion_fund_legs").Error)
		require.NoError(t, DB.Exec("DELETE FROM promotion_fund_transactions").Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	var transaction PromotionFundTransaction
	require.NoError(t, DB.Preload("Legs").Where("transaction_key = ?", "redemption:"+strconv.Itoa(redemption.Id)+":credited").First(&transaction).Error)
	assert.Equal(t, PromotionFundKindRedemptionCredited, transaction.Kind)
	assert.Equal(t, userId, transaction.UserId)
	require.Len(t, transaction.Legs, 1)
	assert.Equal(t, int64(500), transaction.Legs[0].Amount)
	require.NotNil(t, transaction.Legs[0].BalanceAfter)
	assert.Equal(t, int64(500), *transaction.Legs[0].BalanceAfter)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}


func TestRedeemRollsBackCodeBalanceAndFundRecordOnWalletOverflow(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxQuota-100).Error)

	_, err := Redeem(key, userId)
	require.Error(t, err)

	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, common.MaxQuota-100, user.Quota)
	var redemption Redemption
	require.NoError(t, DB.Where("key = ?", key).First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
	assert.Zero(t, redemption.UsedUserId)
	assert.Zero(t, redemption.RedeemedTime)
	var transactionCount int64
	require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestRedeemDoesNotConsumeCodeForMissingOrDeletedUser(t *testing.T) {
	testCases := []struct {
		name    string
		prepare func(*testing.T, int) int
	}{
		{
			name: "missing user",
			prepare: func(_ *testing.T, _ int) int {
				return 999_999
			},
		},
		{
			name: "soft deleted user",
			prepare: func(t *testing.T, userId int) int {
				require.NoError(t, DB.Delete(&User{}, userId).Error)
				return userId
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			userId, key := setupRedeemFixture(t, 200)
			targetUserId := testCase.prepare(t, userId)

			_, err := Redeem(key, targetUserId)
			require.Error(t, err)
			var redemption Redemption
			require.NoError(t, DB.Where("key = ?", key).First(&redemption).Error)
			assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
			assert.Zero(t, redemption.UsedUserId)
			var transactionCount int64
			require.NoError(t, DB.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
			assert.Zero(t, transactionCount)
		})
	}
}

func TestHardDeleteUserRetainsSoftDeletedLegacyUsedRedemption(t *testing.T) {
	truncateTables(t)
	user := &User{
		Username: "legacy-redemption-owner", AffCode: "legacy-redemption-owner",
		Status: common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
	redemption := &Redemption{
		Name: "legacy-used-redemption", Key: "legacy-used-redemption-key",
		Status: common.RedemptionCodeStatusUsed, Quota: 100,
		UsedUserId: user.Id, RedeemedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	require.NoError(t, DB.Delete(redemption).Error)

	err := user.HardDelete()
	require.ErrorIs(t, err, ErrUserFinancialHistory)
	var retained User
	require.NoError(t, DB.Unscoped().First(&retained, user.Id).Error)
	assert.False(t, retained.DeletedAt.Valid)
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	setupRedeemFixture(t, 500)

	redemption := &Redemption{
		Name:        "overflow-redemption",
		Key:         "10000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       common.MaxQuota + 1,
		CreatedTime: common.GetTimestamp(),
	}
	require.Error(t, redemption.Insert())
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.
func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}
