package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromotionWithdrawalPayoutReferenceIsUniqueUnderConcurrentInitiation(t *testing.T) {
	truncate(t)
	for _, actorId := range []int{70, 71, 80, 81, 99} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	withdrawals := make([]*model.PromotionWithdrawal, 0, 2)
	for index, userId := range []int{3060, 3061} {
		seedUser(t, userId, 0)
		seedPromotionCommissionLedger(t, userId, int64(1000+index), 5000+index)
		withdrawal, err := CreatePromotionWithdrawal(userId, withPromotionCommissionBalanceExpectation(t, userId, PromotionWithdrawalRequest{
			PayoutMethod: "bank", PayoutAccount: "account",
		}))
		require.NoError(t, err)
		withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 70+index, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
		require.NoError(t, err)
		withdrawals = append(withdrawals, withdrawal)
	}

	type initiateResult struct {
		index      int
		withdrawal *model.PromotionWithdrawal
		err        error
	}
	start := make(chan struct{})
	results := make(chan initiateResult, len(withdrawals))
	var waitGroup sync.WaitGroup
	for index := range withdrawals {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			reference := "bank-reference-3060"
			if index == 1 {
				reference = "BANK-REFERENCE-3060"
			}
			withdrawal, err := AdminInitiatePromotionWithdrawalPayout(withdrawals[index].Id, 80+index, PromotionWithdrawalReviewRequest{
				TradeNo: reference, ReviewNote: "initiated",
			})
			results <- initiateResult{index: index, withdrawal: withdrawal, err: err}
		}(index)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes := make([]initiateResult, 0, 1)
	failures := make([]initiateResult, 0, 1)
	for result := range results {
		if result.err == nil {
			successes = append(successes, result)
		} else {
			failures = append(failures, result)
		}
	}
	require.Len(t, successes, 1)
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0].err, model.ErrPromotionWithdrawalPayoutReferenceAlreadyUsed)
	assert.Equal(t, model.PromotionWithdrawalStatusProcessing, successes[0].withdrawal.Status)

	winner, err := GetAdminPromotionWithdrawal(withdrawals[successes[0].index].Id)
	require.NoError(t, err)
	loser, err := GetAdminPromotionWithdrawal(withdrawals[failures[0].index].Id)
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusProcessing, winner.Status)
	assert.Equal(t, model.PromotionWithdrawalStatusApproved, loser.Status)
	require.Len(t, winner.Operations, 3)
	require.Len(t, loser.Operations, 2)

	retry, err := AdminInitiatePromotionWithdrawalPayout(winner.Id, 99, PromotionWithdrawalReviewRequest{
		TradeNo: winner.TradeNo, ReviewNote: "initiated",
	})
	require.NoError(t, err)
	assert.Equal(t, winner.Id, retry.Id)
	require.Len(t, retry.Operations, 3)
	_, err = AdminInitiatePromotionWithdrawalPayout(winner.Id, 99, PromotionWithdrawalReviewRequest{
		TradeNo: winner.TradeNo, ReviewNote: "different payload",
	})
	require.ErrorIs(t, err, model.ErrPromotionWithdrawalPayoutReferenceConflict)

	var claims []model.PromotionWithdrawalPayoutReference
	require.NoError(t, model.DB.Find(&claims).Error)
	require.Len(t, claims, 1)
	assert.Equal(t, winner.Id, claims[0].WithdrawalId)
	require.ErrorIs(t, model.DB.Model(&model.PromotionWithdrawalPayoutReference{Id: claims[0].Id}).
		Update("external_reference", "tampered").Error, model.ErrPromotionWithdrawalPayoutReferenceImmutable)
	require.ErrorIs(t, model.DB.Delete(&model.PromotionWithdrawalPayoutReference{Id: claims[0].Id}).Error,
		model.ErrPromotionWithdrawalPayoutReferenceImmutable)
}

func TestPromotionWithdrawalPayoutReferenceRejectsDifferentRetryReference(t *testing.T) {
	truncate(t)
	seedFinancialActor(t, 90, common.RoleAdminUser)
	seedFinancialActor(t, 91, common.RoleAdminUser)
	seedUser(t, 3062, 0)
	seedPromotionCommissionLedger(t, 3062, 1000, 5000)
	withdrawal, err := CreatePromotionWithdrawal(3062, withPromotionCommissionBalanceExpectation(t, 3062, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account",
	}))
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 90, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 91, PromotionWithdrawalReviewRequest{
		TradeNo: "reference-3062", ReviewNote: "initiated",
	})
	require.NoError(t, err)

	_, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 91, PromotionWithdrawalReviewRequest{
		TradeNo: "reference-3062-other", ReviewNote: "initiated",
	})
	require.True(t, errors.Is(err, model.ErrPromotionWithdrawalPayoutReferenceConflict))
}

func TestPromotionWithdrawalPayoutReferenceMaximumLengthPersistsThroughPaidJournal(t *testing.T) {
	truncate(t)
	for _, actorId := range []int{92, 93, 94} {
		seedFinancialActor(t, actorId, common.RoleAdminUser)
	}
	seedUser(t, 3063, 0)
	seedPromotionCommissionLedger(t, 3063, 1000, 5000)
	withdrawal, err := CreatePromotionWithdrawal(3063, withPromotionCommissionBalanceExpectation(t, 3063, PromotionWithdrawalRequest{
		PayoutMethod: "bank", PayoutAccount: "account",
	}))
	require.NoError(t, err)
	withdrawal, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 92, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
	require.NoError(t, err)

	reference := strings.Repeat("R", 255)
	withdrawal, err = AdminInitiatePromotionWithdrawalPayout(withdrawal.Id, 93, PromotionWithdrawalReviewRequest{
		TradeNo: reference, ReviewNote: "initiated",
	})
	require.NoError(t, err)
	withdrawal, err = AdminMarkPromotionWithdrawalPaid(withdrawal.Id, 94, PromotionWithdrawalReviewRequest{
		TradeNo: reference, ReviewNote: "paid",
	})
	require.NoError(t, err)
	assert.Equal(t, model.PromotionWithdrawalStatusPaid, withdrawal.Status)

	var payout model.PromotionFundTransaction
	require.NoError(t, model.DB.Where("transaction_key = ?", fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id)).First(&payout).Error)
	assert.Equal(t, reference, payout.ExternalRef)
}
