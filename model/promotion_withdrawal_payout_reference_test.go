package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openPromotionWithdrawalPayoutReferenceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openPromotionFundTransactionTestDB(t)
	require.NoError(t, db.AutoMigrate(&PromotionWithdrawal{}, &PromotionWithdrawalPayoutReference{}))
	return db
}

func TestBackfillPromotionWithdrawalPayoutReferencesClaimsOnlyPayoutStatesAndIsIdempotent(t *testing.T) {
	db := openPromotionWithdrawalPayoutReferenceTestDB(t)
	require.NoError(t, db.Create(&[]PromotionWithdrawal{
		{Id: 1, UserId: 1, Status: PromotionWithdrawalStatusProcessing, TradeNo: " bank-ref-1 ", PayoutMethod: "BANK"},
		{Id: 2, UserId: 2, Status: PromotionWithdrawalStatusPaid, TradeNo: "wallet-ref-2", PayoutMethod: "wallet"},
		{Id: 3, UserId: 3, Status: PromotionWithdrawalStatusPaid, TradeNo: "   ", PayoutMethod: "wallet"},
		{Id: 4, UserId: 4, Status: PromotionWithdrawalStatusApproved, TradeNo: "not-initiated", PayoutMethod: "bank"},
		{Id: 5, UserId: 5, Status: PromotionWithdrawalStatusFailed, TradeNo: "failed-ref-5", PayoutMethod: "bank", PayoutInitiatedAt: 10},
		{Id: 6, UserId: 6, Status: PromotionWithdrawalStatusFailed, TradeNo: "never-initiated", PayoutMethod: "bank"},
	}).Error)

	require.NoError(t, BackfillPromotionWithdrawalPayoutReferences(db))
	require.NoError(t, BackfillPromotionWithdrawalPayoutReferences(db))
	var claims []PromotionWithdrawalPayoutReference
	require.NoError(t, db.Order("withdrawal_id ASC").Find(&claims).Error)
	require.Len(t, claims, 3)
	assert.Equal(t, 1, claims[0].WithdrawalId)
	assert.Equal(t, "bank-ref-1", claims[0].ExternalReference)
	assert.Equal(t, "bank", claims[0].PayoutMethod)
	assert.Equal(t, 2, claims[1].WithdrawalId)
	assert.Equal(t, "wallet-ref-2", claims[1].ExternalReference)
	assert.Equal(t, 5, claims[2].WithdrawalId)
	assert.Equal(t, "failed-ref-5", claims[2].ExternalReference)
}

func TestPromotionWithdrawalPayoutReferencePreservesOriginalCaseAndAcceptsCaseOnlyRetry(t *testing.T) {
	db := openPromotionWithdrawalPayoutReferenceTestDB(t)
	withdrawal := &PromotionWithdrawal{
		Id: 7, UserId: 7, Status: PromotionWithdrawalStatusApproved, PayoutMethod: "bank",
	}
	require.NoError(t, db.Create(withdrawal).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ClaimPromotionWithdrawalPayoutReferenceTx(tx, withdrawal, " Provider-Ref-AbC ")
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ClaimPromotionWithdrawalPayoutReferenceTx(tx, withdrawal, "provider-ref-abc")
	}))

	var claim PromotionWithdrawalPayoutReference
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).First(&claim).Error)
	assert.Equal(t, "Provider-Ref-AbC", claim.ExternalReference)
	assert.Equal(t, fmt.Sprintf("%x", common.Sha256Raw([]byte("PROVIDER-REF-ABC"))), claim.ReferenceKey)
}

func TestBackfillPromotionWithdrawalPayoutReferencesKeepsFirstHistoricalDuplicateClaim(t *testing.T) {
	db := openPromotionWithdrawalPayoutReferenceTestDB(t)
	require.NoError(t, db.Create(&[]PromotionWithdrawal{
		{Id: 10, UserId: 10, Status: PromotionWithdrawalStatusProcessing, TradeNo: "duplicate-reference", PayoutMethod: "bank"},
		{Id: 11, UserId: 11, Status: PromotionWithdrawalStatusPaid, TradeNo: "DUPLICATE-REFERENCE", PayoutMethod: "wallet"},
		{Id: 12, UserId: 12, Status: PromotionWithdrawalStatusApproved, PayoutMethod: "bank"},
	}).Error)

	require.NoError(t, BackfillPromotionWithdrawalPayoutReferences(db))
	require.NoError(t, BackfillPromotionWithdrawalPayoutReferences(db))
	var claims []PromotionWithdrawalPayoutReference
	require.NoError(t, db.Find(&claims).Error)
	require.Len(t, claims, 1)
	assert.Equal(t, 10, claims[0].WithdrawalId)

	var newWithdrawal PromotionWithdrawal
	require.NoError(t, db.First(&newWithdrawal, 12).Error)
	err := db.Transaction(func(tx *gorm.DB) error {
		return ClaimPromotionWithdrawalPayoutReferenceTx(tx, &newWithdrawal, " Duplicate-Reference ")
	})
	require.ErrorIs(t, err, ErrPromotionWithdrawalPayoutReferenceAlreadyUsed)
}
