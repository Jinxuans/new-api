package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openPromotionFundTransactionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&PromotionFundTransaction{}, &PromotionFundTransactionLeg{}))
	return db
}

func TestCreatePromotionFundTransactionTxIsIdempotentAndLoadsLegs(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	first := &PromotionFundTransaction{
		TransactionKey: "invitation-transfer:1001",
		Kind:           "invitation_credit_transfer",
		UserId:         42,
		SourceType:     "invitation_rewards",
		SourceId:       1001,
	}
	legs := []PromotionFundTransactionLeg{{
		Account:    PromotionFundAccountReferralCredit,
		Asset:      PromotionFundAssetQuota,
		Amount:     200,
		SourceType: "invitation_rewards",
		SourceId:   1001,
	}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, first, legs)
	}))
	require.NotZero(t, first.Id)
	require.Len(t, first.Legs, 1)

	retry := &PromotionFundTransaction{
		TransactionKey: first.TransactionKey,
		Kind:           first.Kind,
		UserId:         first.UserId,
		SourceType:     first.SourceType,
		SourceId:       first.SourceId,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, retry, legs)
	}))
	assert.Equal(t, first.Id, retry.Id)
	require.Len(t, retry.Legs, 1)
	assert.Equal(t, first.Legs[0].Id, retry.Legs[0].Id)
	assert.Equal(t, int64(200), retry.Legs[0].Amount)

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)
	var legCount int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).Count(&legCount).Error)
	assert.Equal(t, int64(1), legCount)
}

func TestCreatePromotionFundTransactionTxRollsBackIncompleteWrite(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&PromotionFundTransactionLeg{}))

	transaction := &PromotionFundTransaction{
		TransactionKey: "rollback:missing-leg-table",
		Kind:           "test_rollback",
		UserId:         43,
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, transaction, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance,
			Asset:   PromotionFundAssetQuota,
			Amount:  50,
		}})
	})
	require.Error(t, err)

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Where("transaction_key = ?", transaction.TransactionKey).Count(&transactionCount).Error)
	assert.Zero(t, transactionCount)
}

func TestCreatePromotionFundTransactionTxRejectsInvalidLegs(t *testing.T) {
	testCases := []struct {
		name string
		leg  PromotionFundTransactionLeg
	}{
		{
			name: "unknown account",
			leg:  PromotionFundTransactionLeg{Account: "unknown", Asset: PromotionFundAssetQuota, Amount: 1},
		},
		{
			name: "account and asset mismatch",
			leg:  PromotionFundTransactionLeg{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetCash, Currency: "CNY", Amount: 1},
		},
		{
			name: "quota has currency",
			leg:  PromotionFundTransactionLeg{Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota, Currency: "CNY", Amount: 1},
		},
		{
			name: "cash currency is not uppercase ISO form",
			leg:  PromotionFundTransactionLeg{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: "cny", Amount: 1},
		},
		{
			name: "zero amount",
			leg:  PromotionFundTransactionLeg{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 0},
		},
	}

	for i, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openPromotionFundTransactionTestDB(t)
			transaction := &PromotionFundTransaction{
				TransactionKey: "invalid-leg:" + string(rune('a'+i)),
				Kind:           "invalid_leg_test",
				UserId:         44,
			}
			err := db.Transaction(func(tx *gorm.DB) error {
				return CreatePromotionFundTransactionTx(tx, transaction, []PromotionFundTransactionLeg{testCase.leg})
			})
			require.ErrorIs(t, err, ErrPromotionFundTransactionLegInvalid)

			var count int64
			require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestCreatePromotionFundTransactionTxPersistsMultipleLegs(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	availableAfter := int64(3750)
	reservedAfter := int64(1250)
	transaction := &PromotionFundTransaction{
		TransactionKey: "withdrawal-reserve:501",
		Kind:           "commission_withdrawal_reserved",
		UserId:         45,
		SourceType:     "promotion_withdrawals",
		SourceId:       501,
		ActorType:      "user",
		ActorId:        45,
	}
	legs := []PromotionFundTransactionLeg{
		{
			Account:      PromotionFundAccountCommissionAvailable,
			Asset:        PromotionFundAssetCash,
			Currency:     "CNY",
			Amount:       -1250,
			SourceType:   "promotion_commission_ledgers",
			SourceId:     701,
			BalanceAfter: &availableAfter,
		},
		{
			Account:      PromotionFundAccountCommissionReserved,
			Asset:        PromotionFundAssetCash,
			Currency:     "CNY",
			Amount:       1250,
			SourceType:   "promotion_commission_ledgers",
			SourceId:     701,
			BalanceAfter: &reservedAfter,
		},
	}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, transaction, legs)
	}))
	require.Len(t, transaction.Legs, 2)
	assert.Equal(t, transaction.Id, transaction.Legs[0].TransactionId)
	assert.Equal(t, transaction.Id, transaction.Legs[1].TransactionId)
	assert.Equal(t, int64(-1250), transaction.Legs[0].Amount)
	assert.Equal(t, int64(1250), transaction.Legs[1].Amount)
	assert.Equal(t, availableAfter, *transaction.Legs[0].BalanceAfter)
	assert.Equal(t, reservedAfter, *transaction.Legs[1].BalanceAfter)
}

func TestCreatePromotionFundTransactionTxEnforcesPersistableReferenceLength(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	maximumReference := strings.Repeat("参", promotionFundReferenceMaxRunes)
	transaction := &PromotionFundTransaction{
		TransactionKey: "reference:max-length",
		Kind:           "reference_length_test",
		UserId:         45,
		SourceKey:      maximumReference,
		ExternalRef:    maximumReference,
	}
	legs := []PromotionFundTransactionLeg{{
		Account: PromotionFundAccountAPIBalance,
		Asset:   PromotionFundAssetQuota,
		Amount:  1,
	}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, transaction, legs)
	}))

	var persisted PromotionFundTransaction
	require.NoError(t, db.First(&persisted, transaction.Id).Error)
	assert.Equal(t, maximumReference, persisted.SourceKey)
	assert.Equal(t, maximumReference, persisted.ExternalRef)

	tooLong := strings.Repeat("参", promotionFundReferenceMaxRunes+1)
	for _, testCase := range []struct {
		name        string
		sourceKey   string
		externalRef string
		expectedErr error
	}{
		{name: "source key", sourceKey: tooLong, expectedErr: ErrPromotionFundTransactionSourceKeyInvalid},
		{name: "external reference", externalRef: tooLong, expectedErr: ErrPromotionFundTransactionExternalRefInvalid},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := &PromotionFundTransaction{
				TransactionKey: "reference:too-long:" + testCase.name,
				Kind:           "reference_length_test",
				UserId:         45,
				SourceKey:      testCase.sourceKey,
				ExternalRef:    testCase.externalRef,
			}
			err := db.Transaction(func(tx *gorm.DB) error {
				return CreatePromotionFundTransactionTx(tx, candidate, legs)
			})
			require.ErrorIs(t, err, testCase.expectedErr)
		})
	}
}

func TestCreatePromotionFundTransactionTxValidatesReversalLink(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	leg := []PromotionFundTransactionLeg{{
		Account: PromotionFundAccountAPIBalance,
		Asset:   PromotionFundAssetQuota,
		Amount:  -100,
	}}

	missing := &PromotionFundTransaction{
		TransactionKey:        "refund:missing-original",
		Kind:                  "refund_reversal",
		UserId:                46,
		ReversesTransactionId: 999,
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, missing, leg)
	})
	require.ErrorIs(t, err, ErrPromotionFundReversalNotFound)

	original := &PromotionFundTransaction{
		TransactionKey: "topup:original",
		Kind:           "topup_credit",
		UserId:         46,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, original, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance,
			Asset:   PromotionFundAssetQuota,
			Amount:  100,
		}})
	}))

	wrongUser := &PromotionFundTransaction{
		TransactionKey:        "refund:wrong-user",
		Kind:                  "refund_reversal",
		UserId:                47,
		ReversesTransactionId: original.Id,
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, wrongUser, leg)
	})
	require.ErrorIs(t, err, ErrPromotionFundReversalUserMismatch)

	reversal := &PromotionFundTransaction{
		TransactionKey:        "refund:valid",
		Kind:                  "refund_reversal",
		UserId:                original.UserId,
		ReversesTransactionId: original.Id,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, reversal, leg)
	}))
	assert.Equal(t, original.Id, reversal.ReversesTransactionId)
}

func TestCreatePromotionFundTransactionTxRejectsIdempotencyPayloadConflicts(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	original := &PromotionFundTransaction{
		TransactionKey: "conflict:stable-key",
		Kind:           "reward_issued",
		UserId:         48,
		SourceType:     "growth_rewards",
		SourceId:       10,
		Remark:         "original",
	}
	originalLegs := []PromotionFundTransactionLeg{{
		Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
		Amount: 100, SourceType: "growth_rewards", SourceId: 10,
	}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, original, originalLegs)
	}))

	testCases := []struct {
		name        string
		transaction *PromotionFundTransaction
		legs        []PromotionFundTransactionLeg
	}{
		{
			name: "header",
			transaction: &PromotionFundTransaction{
				TransactionKey: original.TransactionKey, Kind: original.Kind, UserId: original.UserId,
				SourceType: original.SourceType, SourceId: original.SourceId, Remark: "different",
			},
			legs: originalLegs,
		},
		{
			name: "legs",
			transaction: &PromotionFundTransaction{
				TransactionKey: original.TransactionKey, Kind: original.Kind, UserId: original.UserId,
				SourceType: original.SourceType, SourceId: original.SourceId, Remark: original.Remark,
			},
			legs: []PromotionFundTransactionLeg{{
				Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
				Amount: 101, SourceType: "growth_rewards", SourceId: 10,
			}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := db.Transaction(func(tx *gorm.DB) error {
				return CreatePromotionFundTransactionTx(tx, testCase.transaction, testCase.legs)
			})
			require.ErrorIs(t, err, ErrPromotionFundTransactionConflict)
		})
	}

	var transactionCount int64
	require.NoError(t, db.Model(&PromotionFundTransaction{}).Count(&transactionCount).Error)
	assert.Equal(t, int64(1), transactionCount)
	var legCount int64
	require.NoError(t, db.Model(&PromotionFundTransactionLeg{}).Count(&legCount).Error)
	assert.Equal(t, int64(1), legCount)
}

func TestPromotionFundTransactionHistoryIsImmutable(t *testing.T) {
	db := openPromotionFundTransactionTestDB(t)
	transaction := &PromotionFundTransaction{TransactionKey: "immutable:1", Kind: "reward_issued", UserId: 49}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return CreatePromotionFundTransactionTx(tx, transaction, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: 100,
		}})
	}))
	require.Len(t, transaction.Legs, 1)

	require.ErrorIs(t, db.Model(&PromotionFundTransaction{Id: transaction.Id}).Update("remark", "tampered").Error,
		ErrPromotionFundTransactionImmutable)
	require.ErrorIs(t, db.Delete(&PromotionFundTransaction{Id: transaction.Id}).Error,
		ErrPromotionFundTransactionImmutable)
	require.ErrorIs(t, db.Model(&PromotionFundTransactionLeg{Id: transaction.Legs[0].Id}).Update("amount", 1).Error,
		ErrPromotionFundTransactionImmutable)
	require.ErrorIs(t, db.Delete(&PromotionFundTransactionLeg{Id: transaction.Legs[0].Id}).Error,
		ErrPromotionFundTransactionImmutable)
}
