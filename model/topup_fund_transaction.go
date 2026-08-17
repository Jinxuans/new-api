package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PromotionFundKindTopUpCredited = "api_balance_topup_credited"
	PromotionFundSourceTopUps      = "top_ups"
)

var ErrTopUpFundTransactionInvalid = errors.New("top-up fund transaction is invalid")

type topUpFundActor struct {
	ActorType string
	ActorId   int
	ActorRef  string
	Remark    string
}

// topUpCreditedQuotaForFundRecord returns the exact wallet credit represented
// by a successful API-balance order. New orders persist CreditedQuota. The
// amount and money fields on legacy rows are not sufficient evidence because
// the quota conversion ratio may have changed after settlement.
func topUpCreditedQuotaForFundRecord(topUp *TopUp) (int, bool) {
	if topUp == nil || topUp.UserId <= 0 || topUp.Id <= 0 ||
		topUp.Purpose != TopUpPurposeAPIBalance || topUp.Status != common.TopUpStatusSuccess {
		return 0, false
	}
	if topUp.CreditedQuota > 0 && topUp.CreditedQuota < common.MaxQuota {
		return topUp.CreditedQuota, true
	}
	if topUp.CreditedQuota != 0 {
		return 0, false
	}
	return 0, false
}

// recordTopUpFundTransactionTx persists the immutable evidence for one
// successful API-balance credit. A known BalanceAfter is supplied only by the
// live settlement transaction; historical reconstruction deliberately leaves
// it nil instead of presenting the current wallet as the historical balance.
func recordTopUpFundTransactionTx(tx *gorm.DB, topUp *TopUp, balanceAfter *int64, historical bool, actor topUpFundActor) (*PromotionFundTransaction, error) {
	if tx == nil {
		return nil, errors.New("database transaction is required")
	}
	creditedQuota, ok := topUpCreditedQuotaForFundRecord(topUp)
	if !ok {
		return nil, ErrTopUpFundTransactionInvalid
	}
	if !historical && actor.ActorType == "" {
		return nil, ErrTopUpFundTransactionInvalid
	}

	occurredAt := promotionFundBackfillOccurredAt(topUp.CompleteTime, topUp.CreateTime)
	transaction := &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("topup:%d:credited", topUp.Id),
		Kind:           PromotionFundKindTopUpCredited,
		UserId:         topUp.UserId,
		SourceType:     PromotionFundSourceTopUps,
		SourceId:       topUp.Id,
		SourceKey:      topUp.TradeNo,
		ActorType:      actor.ActorType,
		ActorId:        actor.ActorId,
		ActorRef:       actor.ActorRef,
		ExternalRef:    topUp.ProviderPaymentId,
		Remark:         actor.Remark,
		OccurredAt:     occurredAt,
	}
	legs := []PromotionFundTransactionLeg{{
		Account:      PromotionFundAccountAPIBalance,
		Asset:        PromotionFundAssetQuota,
		Amount:       int64(creditedQuota),
		SourceType:   PromotionFundSourceTopUps,
		SourceId:     topUp.Id,
		BalanceAfter: balanceAfter,
	}}
	if !historical {
		if err := CreatePromotionFundTransactionTx(tx, transaction, legs); err != nil {
			return nil, err
		}
		return transaction, nil
	}

	transaction.TransactionKey = fmt.Sprintf("pfb:%s:%d:credited", PromotionFundSourceTopUps, topUp.Id)
	transaction.ActorType = "system"
	transaction.ActorId = 0
	transaction.ActorRef = promotionFundBackfillActorRef
	if err := createPromotionFundBackfillTransitionTx(tx, transaction, legs,
		fmt.Sprintf("topup:%d:credited", topUp.Id)); err != nil {
		return nil, err
	}
	return transaction, nil
}

// creditTopUpQuotaWithFundTransactionTx commits the wallet mutation and its
// immutable journal entry through the same caller-owned database transaction.
func creditTopUpQuotaWithFundTransactionTx(tx *gorm.DB, topUp *TopUp, creditedQuota int, updates map[string]interface{}, actor topUpFundActor) error {
	if topUp == nil || topUp.CreditedQuota != creditedQuota || topUp.Purpose != TopUpPurposeAPIBalance ||
		topUp.Status != common.TopUpStatusSuccess {
		return ErrTopUpFundTransactionInvalid
	}
	if err := creditTopUpQuota(tx, topUp.UserId, creditedQuota, updates); err != nil {
		return err
	}
	var user User
	if err := tx.Select("quota").Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
		return err
	}
	balanceAfter := int64(user.Quota)
	_, err := recordTopUpFundTransactionTx(tx, topUp, &balanceAfter, false, actor)
	return err
}

// ensureTopUpFundTransactionTx repairs a successful row written by an older
// instance. Because that credit happened before this transaction, its exact
// historical BalanceAfter cannot be inferred and remains nil.
func ensureTopUpFundTransactionTx(tx *gorm.DB, topUp *TopUp) (*PromotionFundTransaction, error) {
	if _, ok := topUpCreditedQuotaForFundRecord(topUp); !ok {
		return nil, nil
	}
	return recordTopUpFundTransactionTx(tx, topUp, nil, true, topUpFundActor{})
}
