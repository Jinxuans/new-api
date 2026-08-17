package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PromotionFundBackfillKey       = "promotion_fund_transactions"
	PromotionFundBackfillVersion   = 4
	PromotionFundBackfillBatchSize = 200
	promotionFundBackfillActorRef  = "backfill"
	promotionFundReconcileKey      = "promotion_fund_reconciliation"

	PromotionFundSourceLegacyAggregate = "legacy_aggregate"

	PromotionFundKindGrowthRewardIssued             = "growth_reward_issued"
	PromotionFundKindGrowthRewardReversed           = "growth_reward_reversed"
	PromotionFundKindInvitationRewardIssued         = "invitation_reward_issued"
	PromotionFundKindInvitationRewardTransferred    = "invitation_reward_transferred"
	PromotionFundKindCommissionPendingAccrued       = "commission_pending_accrued"
	PromotionFundKindCommissionAvailableAccrued     = "commission_available_accrued"
	PromotionFundKindCommissionSettled              = "commission_settled"
	PromotionFundKindCommissionTransferredToBalance = "commission_transferred_to_balance"
	PromotionFundKindCommissionReversed             = "commission_reversed"
	PromotionFundKindCommissionWithdrawalReserved   = "commission_withdrawal_reserved"
	PromotionFundKindCommissionWithdrawalReleased   = "commission_withdrawal_released"
	PromotionFundKindCommissionWithdrawalPaid       = "commission_withdrawal_paid"
	PromotionFundKindReversal                       = "reversal"
	PromotionFundKindLegacyAggregate                = PromotionFundSourceLegacyAggregate
)

const (
	promotionFundBackfillSourceGrowthRewards        = "growth_rewards"
	promotionFundBackfillSourceInvitationRewards    = "invitation_rewards"
	promotionFundBackfillSourceCommissionLedgers    = "promotion_commission_ledgers"
	promotionFundBackfillSourceWithdrawals          = "promotion_withdrawals"
	promotionFundBackfillSourceWithdrawnReversals   = "promotion_commission_withdrawn_reversals"
	promotionFundBackfillSourceLegacyTransferEvents = "promotion_events"
	promotionFundBackfillSourceRedemptions          = "redemptions"
	promotionFundBackfillSourceSubscriptionOrders   = PromotionFundSourceSubscriptionOrders
	promotionFundBackfillSourceTopUps               = PromotionFundSourceTopUps
)

type PromotionFundBackfillCheckpoint struct {
	Id           int    `json:"id"`
	BackfillKey  string `json:"backfill_key" gorm:"type:varchar(64);not null;uniqueIndex:idx_promotion_fund_backfill_key_version,priority:1"`
	Version      int    `json:"version" gorm:"not null;uniqueIndex:idx_promotion_fund_backfill_key_version,priority:2"`
	CursorSource string `json:"cursor_source" gorm:"type:varchar(64);not null"`
	CursorId     int    `json:"cursor_id" gorm:"not null"`
	Completed    bool   `json:"completed" gorm:"not null;index"`
	CreatedAt    int64  `json:"created_at" gorm:"not null"`
	UpdatedAt    int64  `json:"updated_at" gorm:"not null;index"`
}

type PromotionFundBackfillProgress struct {
	Version      int    `json:"version"`
	CursorSource string `json:"cursor_source"`
	CursorId     int    `json:"cursor_id"`
	Processed    int    `json:"processed"`
	Completed    bool   `json:"completed"`
}

type promotionFundCommissionTransferAggregate struct {
	UserId        int
	TransferredAt int64
	Currency      string
	AmountCents   int64
	Quota         int64
}

func (PromotionFundBackfillCheckpoint) TableName() string {
	return "promotion_fund_backfill_checkpoints"
}

func (checkpoint *PromotionFundBackfillCheckpoint) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if checkpoint.CreatedAt == 0 {
		checkpoint.CreatedAt = now
	}
	if checkpoint.UpdatedAt == 0 {
		checkpoint.UpdatedAt = now
	}
	return nil
}

// BackfillPromotionFundTransactionsBatch scans at most 200 source rows and
// advances one global, versioned checkpoint in the same transaction as the
// journal writes. A failed batch leaves both cursor and journal unchanged.
func BackfillPromotionFundTransactionsBatch(db *gorm.DB) (*PromotionFundBackfillProgress, error) {
	return backfillPromotionFundTransactionsBatch(db, PromotionFundBackfillVersion)
}

func backfillPromotionFundTransactionsBatch(db *gorm.DB, version int) (*PromotionFundBackfillProgress, error) {
	return backfillPromotionFundTransactionsBatchForKey(db, PromotionFundBackfillKey, version)
}

func backfillPromotionFundTransactionsBatchForKey(db *gorm.DB, backfillKey string, version int) (*PromotionFundBackfillProgress, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	if strings.TrimSpace(backfillKey) == "" {
		return nil, errors.New("backfill key is required")
	}
	if version <= 0 {
		return nil, errors.New("backfill version must be positive")
	}

	progress := &PromotionFundBackfillProgress{Version: version}
	err := db.Transaction(func(tx *gorm.DB) error {
		checkpoint := &PromotionFundBackfillCheckpoint{
			BackfillKey:  backfillKey,
			Version:      version,
			CursorSource: promotionFundBackfillSourceGrowthRewards,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "backfill_key"}, {Name: "version"}},
			DoNothing: true,
		}).Create(checkpoint).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).
			Where("backfill_key = ? AND version = ?", backfillKey, version).
			First(checkpoint).Error; err != nil {
			return err
		}
		if checkpoint.Completed {
			progress.CursorSource = checkpoint.CursorSource
			progress.CursorId = checkpoint.CursorId
			progress.Completed = true
			return nil
		}
		if checkpoint.CursorSource == "" {
			checkpoint.CursorSource = promotionFundBackfillSourceGrowthRewards
		}

		for progress.Processed < PromotionFundBackfillBatchSize && !checkpoint.Completed {
			limit := PromotionFundBackfillBatchSize - progress.Processed
			processedInSource := 0
			switch checkpoint.CursorSource {
			case promotionFundBackfillSourceGrowthRewards:
				var rows []GrowthReward
				if err := tx.Where("id > ?", checkpoint.CursorId).Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillGrowthRewardFundTransactionsTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceInvitationRewards:
				var rows []InvitationReward
				if err := tx.Where("id > ?", checkpoint.CursorId).Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillInvitationRewardFundTransactionsTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceCommissionLedgers:
				var rows []PromotionCommissionLedger
				if err := tx.Where("id > ?", checkpoint.CursorId).Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillPromotionCommissionFundTransactionsTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceWithdrawals:
				var rows []PromotionWithdrawal
				if err := tx.Where("id > ?", checkpoint.CursorId).Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				itemsByWithdrawalId := make(map[int][]PromotionWithdrawalItem, len(rows))
				if len(rows) > 0 {
					withdrawalIds := make([]int, len(rows))
					for i := range rows {
						withdrawalIds[i] = rows[i].Id
					}
					var items []PromotionWithdrawalItem
					if err := tx.Where("withdrawal_id IN ?", withdrawalIds).
						Order("withdrawal_id ASC, id ASC").Find(&items).Error; err != nil {
						return err
					}
					for i := range items {
						item := items[i]
						itemsByWithdrawalId[item.WithdrawalId] = append(itemsByWithdrawalId[item.WithdrawalId], item)
					}
				}
				for i := range rows {
					if err := backfillPromotionWithdrawalFundTransactionsTx(tx, &rows[i], itemsByWithdrawalId[rows[i].Id]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceWithdrawnReversals:
				var rows []PromotionCommissionLedger
				if err := tx.Where("id > ? AND status = ? AND withdrawn_at > ?", checkpoint.CursorId, PromotionCommissionStatusReversed, 0).
					Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillWithdrawnCommissionReversalTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceLegacyTransferEvents:
				var rows []PromotionEvent
				if err := tx.Where("id > ? AND event_type IN ?", checkpoint.CursorId, []string{
					PromotionEventTypePromotionRewardTransferred,
					PromotionEventTypeCommissionTransferred,
				}).Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				aggregates, err := loadPromotionCommissionTransferAggregatesTx(tx, rows)
				if err != nil {
					return err
				}
				for i := range rows {
					if err := backfillLegacyPromotionTransferEventTx(tx, &rows[i], aggregates); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceRedemptions:
				var rows []Redemption
				if err := tx.Unscoped().Where("id > ?", checkpoint.CursorId).
					Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillRedemptionFundTransactionTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceSubscriptionOrders:
				var rows []SubscriptionOrder
				if err := tx.Where("id > ?", checkpoint.CursorId).
					Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillSubscriptionBalanceFundTransactionTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			case promotionFundBackfillSourceTopUps:
				var rows []TopUp
				if err := tx.Where("id > ?", checkpoint.CursorId).
					Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
					return err
				}
				for i := range rows {
					if err := backfillTopUpFundTransactionTx(tx, &rows[i]); err != nil {
						return err
					}
					checkpoint.CursorId = rows[i].Id
				}
				processedInSource = len(rows)
			default:
				return fmt.Errorf("unknown promotion fund backfill cursor source %q", checkpoint.CursorSource)
			}

			progress.Processed += processedInSource
			if processedInSource < limit {
				checkpoint.CursorSource, checkpoint.Completed = nextPromotionFundBackfillSource(checkpoint.CursorSource)
				checkpoint.CursorId = 0
			}
		}

		checkpoint.UpdatedAt = common.GetTimestamp()
		result := tx.Model(&PromotionFundBackfillCheckpoint{}).
			Where("id = ?", checkpoint.Id).
			Updates(map[string]interface{}{
				"cursor_source": checkpoint.CursorSource,
				"cursor_id":     checkpoint.CursorId,
				"completed":     checkpoint.Completed,
				"updated_at":    checkpoint.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("promotion fund backfill checkpoint changed")
		}
		progress.CursorSource = checkpoint.CursorSource
		progress.CursorId = checkpoint.CursorId
		progress.Completed = checkpoint.Completed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return progress, nil
}

// BackfillPromotionFundTransactions completes the versioned promotion-fund
// history migration in bounded transactions. Each batch commits its own
// checkpoint, so an interrupted startup resumes instead of replaying work.
// A completed checkpoint is left untouched; periodic reconciliation owns
// replaying the source tables after startup.
func BackfillPromotionFundTransactions(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	return runPromotionFundBackfillToCompletion(db, PromotionFundBackfillKey)
}

// ReconcilePromotionFundTransactions replays the source tables after the
// initial migration. This catches legacy writes made by an older instance
// during a rolling deployment without forcing every process startup to scan
// the full history. Immutable transaction keys keep the replay idempotent.
func ReconcilePromotionFundTransactions(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var checkpoint PromotionFundBackfillCheckpoint
		err := lockForUpdate(tx).
			Where("backfill_key = ? AND version = ?", promotionFundReconcileKey, PromotionFundBackfillVersion).
			First(&checkpoint).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !checkpoint.Completed {
			return nil
		}
		return tx.Model(&PromotionFundBackfillCheckpoint{}).Where("id = ?", checkpoint.Id).Updates(map[string]interface{}{
			"cursor_source": promotionFundBackfillSourceGrowthRewards,
			"cursor_id":     0,
			"completed":     false,
			"updated_at":    common.GetTimestamp(),
		}).Error
	}); err != nil {
		return err
	}
	return runPromotionFundBackfillToCompletion(db, promotionFundReconcileKey)
}

func runPromotionFundBackfillToCompletion(db *gorm.DB, backfillKey string) error {
	for {
		progress, err := backfillPromotionFundTransactionsBatchForKey(db, backfillKey, PromotionFundBackfillVersion)
		if err != nil {
			return err
		}
		if progress.Completed {
			return nil
		}
		if progress.Processed == 0 {
			return errors.New("promotion fund backfill made no progress")
		}
	}
}

func nextPromotionFundBackfillSource(source string) (string, bool) {
	switch source {
	case promotionFundBackfillSourceGrowthRewards:
		return promotionFundBackfillSourceInvitationRewards, false
	case promotionFundBackfillSourceInvitationRewards:
		return promotionFundBackfillSourceCommissionLedgers, false
	case promotionFundBackfillSourceCommissionLedgers:
		return promotionFundBackfillSourceWithdrawals, false
	case promotionFundBackfillSourceWithdrawals:
		return promotionFundBackfillSourceWithdrawnReversals, false
	case promotionFundBackfillSourceWithdrawnReversals:
		return promotionFundBackfillSourceLegacyTransferEvents, false
	case promotionFundBackfillSourceLegacyTransferEvents:
		return promotionFundBackfillSourceRedemptions, false
	case promotionFundBackfillSourceRedemptions:
		return promotionFundBackfillSourceSubscriptionOrders, false
	case promotionFundBackfillSourceSubscriptionOrders:
		return promotionFundBackfillSourceTopUps, false
	case promotionFundBackfillSourceTopUps:
		return "", true
	default:
		return source, false
	}
}

func backfillSubscriptionBalanceFundTransactionTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if _, ok := subscriptionBalanceChargedQuotaForFundRecord(order); !ok {
		if order != nil && order.Id > 0 && order.UserId > 0 &&
			order.PaymentProvider == PaymentProviderBalance && order.Status == common.TopUpStatusSuccess {
			common.SysError(fmt.Sprintf(
				"promotion fund subscription evidence skipped: subscription_order_id=%d charged_quota snapshot is unavailable or invalid",
				order.Id,
			))
		}
		return nil
	}
	_, err := recordSubscriptionBalanceFundTransactionTx(tx, order, nil, true)
	return err
}

func backfillTopUpFundTransactionTx(tx *gorm.DB, topUp *TopUp) error {
	if _, ok := topUpCreditedQuotaForFundRecord(topUp); !ok {
		if topUp != nil && topUp.Id > 0 && topUp.UserId > 0 &&
			topUp.Purpose == TopUpPurposeAPIBalance && topUp.Status == common.TopUpStatusSuccess {
			common.SysError(fmt.Sprintf(
				"promotion fund top-up evidence skipped: top_up_id=%d credited quota snapshot is unavailable or invalid",
				topUp.Id,
			))
		}
		return nil
	}
	_, err := recordTopUpFundTransactionTx(tx, topUp, nil, true, topUpFundActor{})
	return err
}

func backfillRedemptionFundTransactionTx(tx *gorm.DB, redemption *Redemption) error {
	if redemption == nil || redemption.Status != common.RedemptionCodeStatusUsed || redemption.UsedUserId <= 0 {
		return nil
	}
	// Older installations allowed malformed redemption values. Their actual
	// wallet effect can differ by database overflow mode, so do not fabricate a
	// journal amount that cannot be reconstructed reliably. The source row is
	// retained for administrators, while all valid historical credits are safe
	// to backfill exactly.
	if ValidateRedemptionQuota(redemption.Quota) != nil {
		return nil
	}

	transaction := newPromotionFundBackfillTransaction(
		promotionFundBackfillSourceRedemptions,
		redemption.Id,
		redemption.UsedUserId,
		"credited",
		PromotionFundKindRedemptionCredited,
		promotionFundBackfillOccurredAt(redemption.RedeemedTime, redemption.CreatedTime),
	)
	transaction.ActorType = "user"
	transaction.ActorId = redemption.UsedUserId
	transaction.Remark = redemption.Name
	return createPromotionFundBackfillTransitionTx(tx, transaction, []PromotionFundTransactionLeg{{
		Account:    PromotionFundAccountAPIBalance,
		Asset:      PromotionFundAssetQuota,
		Amount:     int64(redemption.Quota),
		SourceType: promotionFundBackfillSourceRedemptions,
		SourceId:   redemption.Id,
	}}, fmt.Sprintf("redemption:%d:credited", redemption.Id))
}

func newPromotionFundBackfillTransaction(sourceType string, sourceId int, userId int, transition string, kind string, occurredAt int64) *PromotionFundTransaction {
	return &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("pfb:%s:%d:%s", sourceType, sourceId, transition),
		Kind:           kind,
		UserId:         userId,
		SourceType:     sourceType,
		SourceId:       sourceId,
		SourceKey:      fmt.Sprintf("%s:%d", sourceType, sourceId),
		ActorType:      "system",
		ActorRef:       promotionFundBackfillActorRef,
		OccurredAt:     occurredAt,
	}
}

// createPromotionFundBackfillTransitionTx prefers a realtime transaction for
// the same transition. Backfill metadata can evolve between checkpoint
// versions, so replay validation deliberately compares the immutable economic
// payload rather than version-specific actor or remark fields.
func createPromotionFundBackfillTransitionTx(tx *gorm.DB, transaction *PromotionFundTransaction, legs []PromotionFundTransactionLeg, canonicalKeys ...string) error {
	keys := make([]string, 0, len(canonicalKeys)+1)
	seenKeys := make(map[string]struct{}, len(canonicalKeys)+1)
	for _, key := range append(canonicalKeys, transaction.TransactionKey) {
		if key == "" {
			continue
		}
		if _, exists := seenKeys[key]; exists {
			continue
		}
		seenKeys[key] = struct{}{}
		keys = append(keys, key)
	}
	for _, key := range keys {
		var existing PromotionFundTransaction
		err := tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
			return legTx.Order("id ASC")
		}).Where("transaction_key = ?", key).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if !samePromotionFundBackfillEconomicPayload(&existing, transaction, legs) {
			return fmt.Errorf("%w: backfill transition %s", ErrPromotionFundTransactionConflict, transaction.TransactionKey)
		}
		*transaction = existing
		return nil
	}
	return CreatePromotionFundTransactionTx(tx, transaction, legs)
}

func samePromotionFundBackfillEconomicPayload(existing *PromotionFundTransaction, expected *PromotionFundTransaction, legs []PromotionFundTransactionLeg) bool {
	if existing == nil || expected == nil ||
		existing.Kind != expected.Kind ||
		existing.UserId != expected.UserId ||
		existing.SourceType != expected.SourceType ||
		existing.SourceId != expected.SourceId ||
		len(existing.Legs) != len(legs) {
		return false
	}
	for i := range legs {
		existingLeg := existing.Legs[i]
		expectedLeg := legs[i]
		if existingLeg.Account != expectedLeg.Account ||
			existingLeg.Asset != expectedLeg.Asset ||
			existingLeg.Currency != expectedLeg.Currency ||
			existingLeg.Amount != expectedLeg.Amount ||
			existingLeg.SourceType != expectedLeg.SourceType ||
			existingLeg.SourceId != expectedLeg.SourceId {
			return false
		}
	}
	return true
}

// loadPromotionFundBackfillReversalTx finds an already-recorded economic
// reversal by its source legs. Refund processing uses a provider-scoped
// transaction header while older direct transitions use the source row as the
// header, so a key-only lookup cannot safely identify the same event.
func loadPromotionFundBackfillReversalTx(tx *gorm.DB, sourceType string, sourceId int, userId int,
	refundTransition string, expectedKind string, directKeys ...string,
) (*PromotionFundTransaction, bool, error) {
	if tx == nil || sourceType == "" || sourceId <= 0 || userId <= 0 || refundTransition == "" || expectedKind == "" {
		return nil, false, errors.New("promotion fund reversal identity is required")
	}
	directKeySet := make(map[string]struct{}, len(directKeys))
	normalizedDirectKeys := make([]string, 0, len(directKeys))
	for _, key := range directKeys {
		if key = strings.TrimSpace(key); key != "" {
			if _, exists := directKeySet[key]; exists {
				continue
			}
			directKeySet[key] = struct{}{}
			normalizedDirectKeys = append(normalizedDirectKeys, key)
		}
	}

	var transactions []PromotionFundTransaction
	if err := tx.Distinct("promotion_fund_transactions.*").
		Joins("LEFT JOIN promotion_fund_legs ON promotion_fund_legs.transaction_id = promotion_fund_transactions.id").
		Where("(promotion_fund_legs.source_type = ? AND promotion_fund_legs.source_id = ?) OR promotion_fund_transactions.transaction_key IN ? OR promotion_fund_transactions.transaction_key LIKE ?",
			sourceType, sourceId, normalizedDirectKeys, fmt.Sprintf("refund:%%:%s:%d", refundTransition, sourceId)).
		Preload("Legs", func(legTx *gorm.DB) *gorm.DB { return legTx.Order("id ASC") }).
		Order("promotion_fund_transactions.id ASC").Find(&transactions).Error; err != nil {
		return nil, false, err
	}

	var found *PromotionFundTransaction
	for i := range transactions {
		transaction := &transactions[i]
		_, directAlias := directKeySet[transaction.TransactionKey]
		refundCaseId := 0
		if _, err := fmt.Sscanf(transaction.TransactionKey, "refund:%d:"+refundTransition+":"+fmt.Sprint(sourceId), &refundCaseId); err != nil ||
			refundCaseId <= 0 || transaction.TransactionKey != fmt.Sprintf("refund:%d:%s:%d", refundCaseId, refundTransition, sourceId) {
			refundCaseId = 0
		}
		allowedKind := transaction.Kind == expectedKind || transaction.Kind == PromotionFundKindReversal
		if !directAlias && refundCaseId == 0 && !allowedKind {
			continue
		}
		if !allowedKind || (!directAlias && refundCaseId == 0) || transaction.UserId != userId {
			return nil, false, fmt.Errorf("%w: reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
		}

		if directAlias {
			if transaction.SourceType != sourceType || transaction.SourceId != sourceId {
				return nil, false, fmt.Errorf("%w: reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
			}
		} else {
			var refundCase PromotionRefundCase
			if err := tx.Select("id", "commission_ledger_id").Where("id = ?", refundCaseId).First(&refundCase).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, fmt.Errorf("%w: reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
			} else if err != nil {
				return nil, false, err
			}
			headerMatches := transaction.SourceType == "promotion_refund_cases" && transaction.SourceId == refundCase.Id
			if sourceType == "promotion_commission_ledgers" {
				headerMatches = headerMatches || (transaction.SourceType == sourceType && transaction.SourceId == sourceId)
				headerMatches = headerMatches && refundCase.CommissionLedgerId == sourceId
			}
			if !headerMatches {
				return nil, false, fmt.Errorf("%w: reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
			}
		}

		if found != nil {
			return nil, false, fmt.Errorf("%w: duplicate reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
		}
		found = transaction
	}
	return found, found != nil, nil
}

func validatePromotionFundBackfillQuotaReversal(transaction *PromotionFundTransaction, sourceType string, sourceId int,
	userId int, amount int64, debitAccounts ...string,
) error {
	if transaction == nil || amount <= 0 || transaction.UserId != userId || len(transaction.Legs) == 0 {
		return fmt.Errorf("%w: quota reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
	}
	allowedDebitAccounts := make(map[string]struct{}, len(debitAccounts))
	for _, account := range debitAccounts {
		allowedDebitAccounts[account] = struct{}{}
	}
	var reversedAmount int64
	for _, leg := range transaction.Legs {
		if leg.SourceType != sourceType || leg.SourceId != sourceId || leg.Asset != PromotionFundAssetQuota || leg.Currency != "" {
			return fmt.Errorf("%w: quota reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
		}
		if _, allowed := allowedDebitAccounts[leg.Account]; allowed {
			if leg.Amount >= 0 || leg.Amount == math.MinInt64 || -leg.Amount > amount-reversedAmount {
				return fmt.Errorf("%w: quota reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
			}
			reversedAmount += -leg.Amount
			continue
		}
		if leg.Account != PromotionFundAccountRefundDebt || leg.Amount <= 0 || leg.Amount > amount-reversedAmount {
			return fmt.Errorf("%w: quota reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
		}
		reversedAmount += leg.Amount
	}
	if reversedAmount != amount {
		return fmt.Errorf("%w: quota reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
	}
	return nil
}

func validatePromotionFundBackfillCashReversal(transaction *PromotionFundTransaction, sourceType string, sourceId int,
	userId int, account string, currency string, amount int64,
) error {
	if transaction == nil || amount <= 0 || transaction.UserId != userId || len(transaction.Legs) != 1 {
		return fmt.Errorf("%w: cash reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
	}
	leg := transaction.Legs[0]
	if leg.SourceType != sourceType || leg.SourceId != sourceId || leg.Account != account ||
		leg.Asset != PromotionFundAssetCash || leg.Currency != currency || leg.Amount != -amount {
		return fmt.Errorf("%w: cash reversal for %s %d", ErrPromotionFundTransactionConflict, sourceType, sourceId)
	}
	return nil
}

func loadPromotionCommissionAccrualTx(tx *gorm.DB, ledger *PromotionCommissionLedger, currency string) (*PromotionFundTransaction, bool, error) {
	var transactions []PromotionFundTransaction
	err := tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
		return legTx.Order("id ASC")
	}).Where("transaction_key IN ?", []string{
		fmt.Sprintf("commission:%d:accrued", ledger.Id),
		fmt.Sprintf("pfb:promotion_commission_ledgers:%d:accrued", ledger.Id),
	}).Order("id ASC").Find(&transactions).Error
	if err != nil {
		return nil, false, err
	}
	if len(transactions) == 0 {
		return nil, false, nil
	}
	if len(transactions) > 1 {
		return nil, false, fmt.Errorf("%w: duplicate commission accrual %d", ErrPromotionFundTransactionConflict, ledger.Id)
	}
	if err := validatePromotionCommissionAccrual(&transactions[0], ledger, currency); err != nil {
		return nil, false, err
	}
	return &transactions[0], true, nil
}

func validatePromotionCommissionAccrual(transaction *PromotionFundTransaction, ledger *PromotionCommissionLedger, currency string) error {
	if transaction == nil || ledger == nil {
		return errors.New("promotion commission accrual is required")
	}
	if transaction.UserId != ledger.UserId ||
		transaction.SourceType != "promotion_commission_ledgers" ||
		transaction.SourceId != ledger.Id ||
		len(transaction.Legs) != 1 {
		return fmt.Errorf("%w: commission accrual %d", ErrPromotionFundTransactionConflict, ledger.Id)
	}
	leg := transaction.Legs[0]
	expectedAccount := ""
	switch transaction.Kind {
	case PromotionFundKindCommissionPendingAccrued:
		expectedAccount = PromotionFundAccountCommissionPending
	case PromotionFundKindCommissionAvailableAccrued:
		expectedAccount = PromotionFundAccountCommissionAvailable
	default:
		return fmt.Errorf("%w: commission accrual %d", ErrPromotionFundTransactionConflict, ledger.Id)
	}
	if leg.Account != expectedAccount || leg.Asset != PromotionFundAssetCash || leg.Currency != currency ||
		leg.Amount != ledger.NetAmountCents || leg.SourceType != "promotion_commission_ledgers" || leg.SourceId != ledger.Id {
		return fmt.Errorf("%w: commission accrual %d", ErrPromotionFundTransactionConflict, ledger.Id)
	}
	return nil
}

func backfillUncashablePromotionCommissionAccrualCorrectionTx(tx *gorm.DB, ledger *PromotionCommissionLedger) error {
	if tx == nil || ledger == nil {
		return errors.New("promotion commission ledger and transaction are required")
	}
	currency := strings.ToUpper(strings.TrimSpace(ledger.Currency))
	canonicalAccrualKey := fmt.Sprintf("commission:%d:accrued", ledger.Id)
	backfillAccrualKey := fmt.Sprintf("pfb:promotion_commission_ledgers:%d:accrued", ledger.Id)
	canonicalSettlementKey := fmt.Sprintf("commission:%d:settled", ledger.Id)
	backfillSettlementKey := fmt.Sprintf("pfb:promotion_commission_ledgers:%d:settled", ledger.Id)
	var transitions []PromotionFundTransaction
	if err := tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
		return legTx.Order("id ASC")
	}).Where("transaction_key IN ?", []string{
		canonicalAccrualKey,
		backfillAccrualKey,
		canonicalSettlementKey,
		backfillSettlementKey,
	}).Order("id ASC").Find(&transitions).Error; err != nil {
		return err
	}
	if len(transitions) == 0 {
		return nil
	}

	var accrual *PromotionFundTransaction
	var settlement *PromotionFundTransaction
	for i := range transitions {
		transition := &transitions[i]
		switch transition.TransactionKey {
		case canonicalAccrualKey, backfillAccrualKey:
			if accrual != nil {
				return fmt.Errorf("%w: duplicate commission accrual %d", ErrPromotionFundTransactionConflict, ledger.Id)
			}
			if err := validatePromotionCommissionAccrual(transition, ledger, currency); err != nil {
				return err
			}
			accrual = transition
		case canonicalSettlementKey, backfillSettlementKey:
			if settlement != nil {
				return fmt.Errorf("%w: duplicate commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
			}
			if transition.Kind != PromotionFundKindCommissionSettled ||
				transition.UserId != ledger.UserId ||
				transition.SourceType != "promotion_commission_ledgers" ||
				transition.SourceId != ledger.Id || len(transition.Legs) != 2 {
				return fmt.Errorf("%w: commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
			}
			seenPending := false
			seenAvailable := false
			for _, leg := range transition.Legs {
				if leg.Asset != PromotionFundAssetCash || leg.Currency != currency ||
					leg.SourceType != "promotion_commission_ledgers" || leg.SourceId != ledger.Id {
					return fmt.Errorf("%w: commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
				}
				switch leg.Account {
				case PromotionFundAccountCommissionPending:
					if seenPending || leg.Amount != -ledger.NetAmountCents {
						return fmt.Errorf("%w: commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
					}
					seenPending = true
				case PromotionFundAccountCommissionAvailable:
					if seenAvailable || leg.Amount != ledger.NetAmountCents {
						return fmt.Errorf("%w: commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
					}
					seenAvailable = true
				default:
					return fmt.Errorf("%w: commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
				}
			}
			if !seenPending || !seenAvailable {
				return fmt.Errorf("%w: commission settlement %d", ErrPromotionFundTransactionConflict, ledger.Id)
			}
			settlement = transition
		}
	}
	if accrual == nil {
		return fmt.Errorf("%w: commission settlement %d has no accrual", ErrPromotionFundTransactionConflict, ledger.Id)
	}

	account := accrual.Legs[0].Account
	reversedTransactionId := accrual.Id
	occurredAt := accrual.OccurredAt
	if settlement != nil {
		if account != PromotionFundAccountCommissionPending {
			return fmt.Errorf("%w: available commission accrual %d has a settlement", ErrPromotionFundTransactionConflict, ledger.Id)
		}
		account = PromotionFundAccountCommissionAvailable
		reversedTransactionId = settlement.Id
		occurredAt = settlement.OccurredAt
	}
	correction := newPromotionFundBackfillTransaction(
		"promotion_commission_ledgers",
		ledger.Id,
		ledger.UserId,
		"uncashable_accrual_correction",
		PromotionFundKindReversal,
		occurredAt,
	)
	correction.ReversesTransactionId = reversedTransactionId
	correction.ExternalRef = ledger.SourceTradeNo
	correction.Remark = "neutralize legacy commission accrual frozen after unverified payment"
	return createPromotionFundBackfillTransitionTx(tx, correction, []PromotionFundTransactionLeg{{
		Account: account, Asset: PromotionFundAssetCash, Currency: currency, Amount: -ledger.NetAmountCents,
		SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
	}})
}

func promotionFundBackfillOccurredAt(primary int64, fallback int64) int64 {
	if primary > 0 {
		return primary
	}
	if fallback > 0 {
		return fallback
	}
	// Legacy rows without any timestamp remain visibly unknown instead of being
	// presented as if they occurred when the backfill happened.
	return 1
}

func joinPromotionFundBackfillRemarks(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "; ")
}

func backfillGrowthRewardFundTransactionsTx(tx *gorm.DB, reward *GrowthReward) error {
	if reward == nil || reward.RewardQuota == 0 {
		return nil
	}
	if reward.UserId <= 0 || reward.RewardQuota < 0 {
		return fmt.Errorf("invalid growth reward %d for promotion fund backfill", reward.Id)
	}
	switch reward.Status {
	case GrowthRewardStatusSettled, GrowthRewardStatusTransferred, GrowthRewardStatusReversed:
	case GrowthRewardStatusPending, GrowthRewardStatusFrozen, GrowthRewardStatusRejected:
		return nil
	default:
		return fmt.Errorf("unknown growth reward status %q for row %d", reward.Status, reward.Id)
	}

	issuedAt := promotionFundBackfillOccurredAt(reward.SettledAt, reward.CreatedAt)
	issued := newPromotionFundBackfillTransaction("growth_rewards", reward.Id, reward.UserId, "issued", PromotionFundKindGrowthRewardIssued, issuedAt)
	issued.Remark = reward.Remark
	if err := createPromotionFundBackfillTransitionTx(tx, issued, []PromotionFundTransactionLeg{{
		Account:    PromotionFundAccountAPIBalance,
		Asset:      PromotionFundAssetQuota,
		Amount:     int64(reward.RewardQuota),
		SourceType: "growth_rewards",
		SourceId:   reward.Id,
	}}, fmt.Sprintf("growth_reward:%d:issued", reward.Id)); err != nil {
		return err
	}
	if reward.Status != GrowthRewardStatusReversed {
		return nil
	}

	reversal := newPromotionFundBackfillTransaction("growth_rewards", reward.Id, reward.UserId, "reversed", PromotionFundKindGrowthRewardReversed, issuedAt)
	reversal.ReversesTransactionId = issued.Id
	reversal.Remark = joinPromotionFundBackfillRemarks(reward.Remark, "legacy growth reward does not record reversal time; occurred_at uses settlement time")
	existing, found, err := loadPromotionFundBackfillReversalTx(tx, "growth_rewards", reward.Id, reward.UserId,
		"growth_reward", PromotionFundKindGrowthRewardReversed,
		reversal.TransactionKey, fmt.Sprintf("growth_reward:%d:reversed", reward.Id))
	if err != nil {
		return err
	}
	if found {
		return validatePromotionFundBackfillQuotaReversal(existing, "growth_rewards", reward.Id, reward.UserId,
			int64(reward.RewardQuota), PromotionFundAccountAPIBalance)
	}
	return createPromotionFundBackfillTransitionTx(tx, reversal, []PromotionFundTransactionLeg{{
		Account:    PromotionFundAccountAPIBalance,
		Asset:      PromotionFundAssetQuota,
		Amount:     -int64(reward.RewardQuota),
		SourceType: "growth_rewards",
		SourceId:   reward.Id,
	}})
}

func backfillInvitationRewardFundTransactionsTx(tx *gorm.DB, reward *InvitationReward) error {
	if reward == nil || reward.RewardQuota == 0 {
		return nil
	}
	if reward.InviterId <= 0 || reward.RewardQuota < 0 {
		return fmt.Errorf("invalid invitation reward %d for promotion fund backfill", reward.Id)
	}
	switch reward.Status {
	case InvitationRewardStatusSettled, InvitationRewardStatusReversed:
	case InvitationRewardStatusPending:
		return nil
	default:
		return fmt.Errorf("unknown invitation reward status %q for row %d", reward.Status, reward.Id)
	}

	issuedAt := promotionFundBackfillOccurredAt(reward.SettledAt, promotionFundBackfillOccurredAt(reward.TriggerAt, reward.CreatedAt))
	issued := newPromotionFundBackfillTransaction("invitation_rewards", reward.Id, reward.InviterId, "issued", PromotionFundKindInvitationRewardIssued, issuedAt)
	issued.Remark = reward.Remark
	if err := createPromotionFundBackfillTransitionTx(tx, issued, []PromotionFundTransactionLeg{{
		Account:    PromotionFundAccountReferralCredit,
		Asset:      PromotionFundAssetQuota,
		Amount:     int64(reward.RewardQuota),
		SourceType: "invitation_rewards",
		SourceId:   reward.Id,
	}}, fmt.Sprintf("invitation_reward:%d:issued", reward.Id)); err != nil {
		return err
	}
	if reward.Status != InvitationRewardStatusReversed {
		return nil
	}
	if reward.TransferredQuota < 0 || reward.TransferredQuota > reward.RewardQuota {
		return fmt.Errorf("invalid transferred quota on reversed invitation reward %d", reward.Id)
	}

	legs := make([]PromotionFundTransactionLeg, 0, 2)
	remainingReferralQuota := reward.RewardQuota - reward.TransferredQuota
	if remainingReferralQuota > 0 {
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota,
			Amount: -int64(remainingReferralQuota), SourceType: "invitation_rewards", SourceId: reward.Id,
		})
	}
	if reward.TransferredQuota > 0 {
		legs = append(legs, PromotionFundTransactionLeg{
			Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
			Amount: -int64(reward.TransferredQuota), SourceType: "invitation_rewards", SourceId: reward.Id,
		})
	}
	reversal := newPromotionFundBackfillTransaction("invitation_rewards", reward.Id, reward.InviterId, "reversed", PromotionFundKindReversal, issuedAt)
	reversal.ReversesTransactionId = issued.Id
	reversal.Remark = joinPromotionFundBackfillRemarks(reward.Remark, "legacy invitation reward reversal reconstructed from stored transferred_quota; reversal time is unavailable")
	existing, found, err := loadPromotionFundBackfillReversalTx(tx, "invitation_rewards", reward.Id, reward.InviterId,
		"invitation_reward", PromotionFundKindReversal,
		reversal.TransactionKey, fmt.Sprintf("invitation_reward:%d:reversed", reward.Id))
	if err != nil {
		return err
	}
	if found {
		return validatePromotionFundBackfillQuotaReversal(existing, "invitation_rewards", reward.Id, reward.InviterId,
			int64(reward.RewardQuota), PromotionFundAccountReferralCredit, PromotionFundAccountAPIBalance)
	}
	return createPromotionFundBackfillTransitionTx(tx, reversal, legs)
}

func backfillPromotionCommissionFundTransactionsTx(tx *gorm.DB, ledger *PromotionCommissionLedger) error {
	if ledger == nil || ledger.NetAmountCents == 0 {
		return nil
	}
	requiresQuarantine, err := promotionCommissionReconciliationQuarantineRequired(ledger)
	if err != nil {
		return err
	}
	if requiresQuarantine {
		quarantined, err := isPromotionCommissionReconciliationQuarantinedTx(tx, ledger)
		if err != nil {
			return err
		}
		if quarantined {
			return nil
		}
		return fmt.Errorf("unknown promotion commission status %q for row %d", ledger.Status, ledger.Id)
	}
	if !ledger.Cashable && ledger.TransferredAt == 0 && ledger.WithdrawnAt == 0 &&
		ledger.Status != PromotionCommissionStatusWithdrawing && ledger.Status != PromotionCommissionStatusWithdrawn && ledger.Status != PromotionCommissionStatusReversed {
		return backfillUncashablePromotionCommissionAccrualCorrectionTx(tx, ledger)
	}
	currency := strings.ToUpper(strings.TrimSpace(ledger.Currency))
	createdAt := promotionFundBackfillOccurredAt(ledger.CreatedAt, ledger.AvailableAt)
	reachedAvailable := ledger.Status == PromotionCommissionStatusSettled ||
		ledger.Status == PromotionCommissionStatusTransferred ||
		ledger.Status == PromotionCommissionStatusWithdrawing ||
		ledger.Status == PromotionCommissionStatusWithdrawn ||
		ledger.SettledAt > 0 || ledger.TransferredAt > 0 || ledger.WithdrawnAt > 0
	startsPending := ledger.Status == PromotionCommissionStatusPending ||
		(ledger.Status == PromotionCommissionStatusReversed && !reachedAvailable) ||
		ledger.SettledAt > createdAt
	accrualKind := PromotionFundKindCommissionAvailableAccrued
	accrualAccount := PromotionFundAccountCommissionAvailable
	if startsPending {
		accrualKind = PromotionFundKindCommissionPendingAccrued
		accrualAccount = PromotionFundAccountCommissionPending
	}
	accrual, foundAccrual, err := loadPromotionCommissionAccrualTx(tx, ledger, currency)
	if err != nil {
		return err
	}
	if foundAccrual {
		startsPending = accrual.Kind == PromotionFundKindCommissionPendingAccrued
		accrualAccount = accrual.Legs[0].Account
	} else {
		accrual = newPromotionFundBackfillTransaction("promotion_commission_ledgers", ledger.Id, ledger.UserId, "accrued", accrualKind, createdAt)
		accrual.ExternalRef = ledger.SourceTradeNo
		accrual.Remark = ledger.Remark
		if ledger.Status == PromotionCommissionStatusTransferred && ledger.TransferredAt == 0 {
			accrual.Remark = joinPromotionFundBackfillRemarks(accrual.Remark, "per-ledger transfer not reconstructed because transferred_at is missing")
		}
		if err := createPromotionFundBackfillTransitionTx(tx, accrual, []PromotionFundTransactionLeg{{
			Account:    accrualAccount,
			Asset:      PromotionFundAssetCash,
			Currency:   currency,
			Amount:     ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers",
			SourceId:   ledger.Id,
		}}); err != nil {
			return err
		}
	}

	if startsPending && reachedAvailable {
		settlement := newPromotionFundBackfillTransaction("promotion_commission_ledgers", ledger.Id, ledger.UserId, "settled", PromotionFundKindCommissionSettled, promotionFundBackfillOccurredAt(ledger.SettledAt, createdAt))
		settlement.ExternalRef = ledger.SourceTradeNo
		settlement.Remark = ledger.Remark
		if err := createPromotionFundBackfillTransitionTx(tx, settlement, []PromotionFundTransactionLeg{
			{
				Account:    PromotionFundAccountCommissionPending,
				Asset:      PromotionFundAssetCash,
				Currency:   currency,
				Amount:     -ledger.NetAmountCents,
				SourceType: "promotion_commission_ledgers",
				SourceId:   ledger.Id,
			},
			{
				Account:    PromotionFundAccountCommissionAvailable,
				Asset:      PromotionFundAssetCash,
				Currency:   currency,
				Amount:     ledger.NetAmountCents,
				SourceType: "promotion_commission_ledgers",
				SourceId:   ledger.Id,
			},
		}, fmt.Sprintf("commission:%d:settled", ledger.Id)); err != nil {
			return err
		}
	}

	var transfer *PromotionFundTransaction
	if ledger.TransferredAt > 0 {
		transfer = newPromotionFundBackfillTransaction("promotion_commission_ledgers", ledger.Id, ledger.UserId, "transferred", PromotionFundKindCommissionTransferredToBalance, ledger.TransferredAt)
		transfer.ExternalRef = ledger.SourceTradeNo
		transfer.Remark = ledger.Remark
		legs := []PromotionFundTransactionLeg{{
			Account:    PromotionFundAccountCommissionAvailable,
			Asset:      PromotionFundAssetCash,
			Currency:   currency,
			Amount:     -ledger.NetAmountCents,
			SourceType: "promotion_commission_ledgers",
			SourceId:   ledger.Id,
		}}
		if ledger.QuotaEquivalent > 0 {
			legs = append(legs, PromotionFundTransactionLeg{
				Account:    PromotionFundAccountAPIBalance,
				Asset:      PromotionFundAssetQuota,
				Amount:     int64(ledger.QuotaEquivalent),
				SourceType: "promotion_commission_ledgers",
				SourceId:   ledger.Id,
			})
		}
		if err := createPromotionFundBackfillTransitionTx(tx, transfer, legs, fmt.Sprintf("commission:%d:transferred", ledger.Id)); err != nil {
			return err
		}
	}

	if ledger.Status != PromotionCommissionStatusReversed {
		return nil
	}
	// A paid commission has already left commission_available through the
	// withdrawal reserve and payout transitions. The post-withdrawal pass only
	// repairs obsolete backfill entries; it never infers a recovery debt without
	// an administrator-assessed obligation.
	if ledger.WithdrawnAt > 0 {
		return nil
	}
	reversedAt := promotionFundBackfillOccurredAt(ledger.ReversedAt, promotionFundBackfillOccurredAt(ledger.SettledAt, createdAt))
	reversal := newPromotionFundBackfillTransaction("promotion_commission_ledgers", ledger.Id, ledger.UserId, "reversed", PromotionFundKindCommissionReversed, reversedAt)
	reversal.ExternalRef = ledger.RefundTradeNo
	reversal.Remark = ledger.Remark
	if ledger.ReversedAt == 0 {
		reversal.Remark = joinPromotionFundBackfillRemarks(reversal.Remark, "legacy commission ledger does not record reversal time; occurred_at uses the nearest source timestamp")
	}
	existing, found, err := loadPromotionFundBackfillReversalTx(tx, "promotion_commission_ledgers", ledger.Id, ledger.UserId,
		"commission", PromotionFundKindCommissionReversed,
		reversal.TransactionKey, fmt.Sprintf("commission:%d:reversed", ledger.Id))
	if err != nil {
		return err
	}
	if transfer != nil {
		reversalQuota := ledger.ReversalQuota
		if reversalQuota == 0 {
			reversalQuota = ledger.QuotaEquivalent
		}
		if reversalQuota <= 0 {
			return nil
		}
		if found {
			return validatePromotionFundBackfillQuotaReversal(existing, "promotion_commission_ledgers", ledger.Id, ledger.UserId,
				int64(reversalQuota), PromotionFundAccountAPIBalance)
		}
		reversal.ReversesTransactionId = transfer.Id
		return createPromotionFundBackfillTransitionTx(tx, reversal, []PromotionFundTransactionLeg{{
			Account:    PromotionFundAccountAPIBalance,
			Asset:      PromotionFundAssetQuota,
			Amount:     -int64(reversalQuota),
			SourceType: "promotion_commission_ledgers",
			SourceId:   ledger.Id,
		}}, fmt.Sprintf("commission:%d:reversed", ledger.Id))
	}

	reversal.ReversesTransactionId = accrual.Id
	reversalAmount := ledger.ReversalAmountCents
	if reversalAmount == 0 {
		reversalAmount = ledger.NetAmountCents
	}
	if reversalAmount <= 0 {
		return nil
	}
	reversalAccount := PromotionFundAccountCommissionAvailable
	if !reachedAvailable {
		reversalAccount = PromotionFundAccountCommissionPending
	}
	if found {
		return validatePromotionFundBackfillCashReversal(existing, "promotion_commission_ledgers", ledger.Id, ledger.UserId,
			reversalAccount, currency, reversalAmount)
	}
	return createPromotionFundBackfillTransitionTx(tx, reversal, []PromotionFundTransactionLeg{{
		Account:    reversalAccount,
		Asset:      PromotionFundAssetCash,
		Currency:   currency,
		Amount:     -reversalAmount,
		SourceType: "promotion_commission_ledgers",
		SourceId:   ledger.Id,
	}}, fmt.Sprintf("commission:%d:reversed", ledger.Id))
}

// promotionCommissionReconciliationQuarantineRequired is shared by the
// reconciliation and Root-review paths so an exception can never be recorded
// for a row that reconciliation would still reject before checking quarantine.
func promotionCommissionReconciliationQuarantineRequired(ledger *PromotionCommissionLedger) (bool, error) {
	if ledger == nil || ledger.NetAmountCents == 0 {
		return false, nil
	}
	if ledger.UserId <= 0 || ledger.NetAmountCents < 0 {
		return false, fmt.Errorf("invalid promotion commission ledger %d for fund backfill", ledger.Id)
	}
	return !isKnownPromotionCommissionStatus(ledger.Status), nil
}

func isPromotionCommissionReconciliationQuarantinedTx(tx *gorm.DB, ledger *PromotionCommissionLedger) (bool, error) {
	if tx == nil || ledger == nil || ledger.Id <= 0 || isKnownPromotionCommissionStatus(ledger.Status) {
		return false, nil
	}
	linkedCases := tx.Model(&PromotionRefundCase{}).
		Select("id").
		Where("commission_ledger_id = ?", ledger.Id)
	var actionCount int64
	err := tx.Model(&PromotionRefundAction{}).
		Where("action = ? AND commission_ledger_id = ? AND commission_ledger_status = ?", PromotionRefundActionQuarantineUnknownCommission, ledger.Id, ledger.Status).
		Where("refund_case_id IN (?)", linkedCases).
		Count(&actionCount).Error
	return actionCount > 0, err
}

func backfillWithdrawnCommissionReversalTx(tx *gorm.DB, ledger *PromotionCommissionLedger) error {
	if ledger == nil || ledger.Status != PromotionCommissionStatusReversed || ledger.WithdrawnAt <= 0 {
		return nil
	}
	if ledger.UserId <= 0 || ledger.NetAmountCents <= 0 {
		return fmt.Errorf("invalid withdrawn commission reversal %d for fund backfill", ledger.Id)
	}
	currency := strings.ToUpper(strings.TrimSpace(ledger.Currency))
	amount := ledger.ReversalAmountCents
	if amount == 0 {
		amount = ledger.NetAmountCents
	}
	if amount <= 0 {
		return nil
	}

	// Older backfill versions inferred a cash debt solely from the reversed
	// ledger. That is not evidence of an actual recovery obligation: Root may
	// still need to decide whether the already-paid commission was recovered or
	// waived. Neutralize that invented debt append-only and leave all new debt
	// assessment to the explicit obligation workflow.
	var obsoleteDebt PromotionFundTransaction
	obsoleteDebtErr := tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
		return legTx.Order("id ASC")
	}).Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:paid_refund_debt", ledger.Id)).First(&obsoleteDebt).Error
	if obsoleteDebtErr == nil {
		headerMatches := obsoleteDebt.SourceType == "promotion_commission_ledgers" && obsoleteDebt.SourceId == ledger.Id
		if obsoleteDebt.SourceType == "promotion_refund_cases" && obsoleteDebt.SourceId > 0 {
			var refundCase PromotionRefundCase
			caseErr := tx.Select("id", "commission_ledger_id").Where("id = ?", obsoleteDebt.SourceId).First(&refundCase).Error
			if caseErr != nil && !errors.Is(caseErr, gorm.ErrRecordNotFound) {
				return caseErr
			}
			headerMatches = caseErr == nil && refundCase.CommissionLedgerId == ledger.Id
		}
		if obsoleteDebt.Kind != PromotionFundKindReversal || obsoleteDebt.UserId != ledger.UserId || !headerMatches || len(obsoleteDebt.Legs) != 1 {
			return fmt.Errorf("%w: obsolete paid commission debt %d", ErrPromotionFundTransactionConflict, ledger.Id)
		}
		leg := obsoleteDebt.Legs[0]
		if leg.Account != PromotionFundAccountRefundDebt || leg.Asset != PromotionFundAssetCash || leg.Currency != currency ||
			leg.Amount != amount || leg.SourceType != "promotion_commission_ledgers" || leg.SourceId != ledger.Id {
			return fmt.Errorf("%w: obsolete paid commission debt %d", ErrPromotionFundTransactionConflict, ledger.Id)
		}
		correction := newPromotionFundBackfillTransaction(
			"promotion_commission_ledgers",
			ledger.Id,
			ledger.UserId,
			"paid_refund_debt_correction",
			PromotionFundKindReversal,
			promotionFundBackfillOccurredAt(ledger.ReversedAt, ledger.WithdrawnAt),
		)
		correction.ReversesTransactionId = obsoleteDebt.Id
		correction.ExternalRef = ledger.RefundTradeNo
		correction.Remark = "reverse obsolete backfill-invented paid-commission debt pending Root assessment"
		if err := createPromotionFundBackfillTransitionTx(tx, correction, []PromotionFundTransactionLeg{{
			Account: PromotionFundAccountRefundDebt, Asset: PromotionFundAssetCash,
			Currency: currency, Amount: -amount,
			SourceType: "promotion_commission_ledgers", SourceId: ledger.Id,
		}}); err != nil {
			return err
		}
	} else if !errors.Is(obsoleteDebtErr, gorm.ErrRecordNotFound) {
		return obsoleteDebtErr
	}

	// Version 1 could also debit commission_available after a paid withdrawal
	// had already consumed it. Reverse only that obsolete debit; do not create a
	// replacement debt without an administrator-assessed obligation.
	var obsolete PromotionFundTransaction
	obsoleteErr := tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
		return legTx.Order("id ASC")
	}).Where("transaction_key = ?", fmt.Sprintf("pfb:promotion_commission_ledgers:%d:reversed", ledger.Id)).First(&obsolete).Error
	if errors.Is(obsoleteErr, gorm.ErrRecordNotFound) {
		return nil
	}
	if obsoleteErr != nil {
		return obsoleteErr
	}
	correctionLegs := make([]PromotionFundTransactionLeg, 0, len(obsolete.Legs))
	for _, leg := range obsolete.Legs {
		if leg.Asset != PromotionFundAssetCash || leg.Currency != currency || leg.Amount >= 0 || leg.Amount == math.MinInt64 ||
			(leg.Account != PromotionFundAccountCommissionAvailable && leg.Account != PromotionFundAccountCommissionPending) ||
			leg.SourceType != "promotion_commission_ledgers" || leg.SourceId != ledger.Id {
			return fmt.Errorf("%w: obsolete paid commission reversal %d", ErrPromotionFundTransactionConflict, ledger.Id)
		}
		correctionLegs = append(correctionLegs, PromotionFundTransactionLeg{
			Account: leg.Account, Asset: leg.Asset, Currency: leg.Currency, Amount: -leg.Amount,
			SourceType: leg.SourceType, SourceId: leg.SourceId,
		})
	}
	if len(correctionLegs) == 0 {
		return fmt.Errorf("%w: obsolete paid commission reversal %d has no legs", ErrPromotionFundTransactionConflict, ledger.Id)
	}
	correction := newPromotionFundBackfillTransaction(
		"promotion_commission_ledgers",
		ledger.Id,
		ledger.UserId,
		"paid_refund_reversal_correction",
		PromotionFundKindReversal,
		promotionFundBackfillOccurredAt(ledger.ReversedAt, ledger.WithdrawnAt),
	)
	correction.ReversesTransactionId = obsolete.Id
	correction.ExternalRef = ledger.RefundTradeNo
	correction.Remark = "reverse obsolete version 1 paid-commission available-balance debit"
	return createPromotionFundBackfillTransitionTx(tx, correction, correctionLegs)
}

func backfillPromotionWithdrawalFundTransactionsTx(tx *gorm.DB, withdrawal *PromotionWithdrawal, items []PromotionWithdrawalItem) error {
	if withdrawal == nil {
		return nil
	}
	if withdrawal.UserId <= 0 {
		return fmt.Errorf("invalid promotion withdrawal %d for fund backfill", withdrawal.Id)
	}
	switch withdrawal.Status {
	case PromotionWithdrawalStatusPendingReview, PromotionWithdrawalStatusApproved, PromotionWithdrawalStatusProcessing, PromotionWithdrawalStatusRejected, PromotionWithdrawalStatusFailed, PromotionWithdrawalStatusPaid:
	default:
		return fmt.Errorf("unknown promotion withdrawal status %q for row %d", withdrawal.Status, withdrawal.Id)
	}

	currency := strings.ToUpper(strings.TrimSpace(withdrawal.Currency))
	sourceType := "promotion_commission_ledgers"
	amounts := make([]PromotionWithdrawalItem, 0, len(items))
	var itemTotal int64
	remark := withdrawal.ReviewNote
	if len(items) == 0 {
		itemTotal = withdrawal.GrossAmountCents
		if itemTotal == 0 {
			itemTotal = withdrawal.NetAmountCents
		}
		if itemTotal == 0 {
			return nil
		}
		if itemTotal < 0 {
			return fmt.Errorf("invalid amount on promotion withdrawal %d", withdrawal.Id)
		}
		sourceType = PromotionFundSourceLegacyAggregate
		amounts = append(amounts, PromotionWithdrawalItem{LedgerId: withdrawal.Id, AmountCents: itemTotal})
		remark = joinPromotionFundBackfillRemarks(remark, "legacy withdrawal has no per-ledger items; legs are aggregate")
	} else {
		validatedItems, _, err := validatePromotionWithdrawalLedgerIntegrityTx(tx, withdrawal)
		if err != nil {
			return fmt.Errorf("invalid promotion withdrawal %d ledger allocation: %w", withdrawal.Id, err)
		}
		amounts = validatedItems
	}

	reserve := newPromotionFundBackfillTransaction("promotion_withdrawals", withdrawal.Id, withdrawal.UserId, "reserved", PromotionFundKindCommissionWithdrawalReserved, promotionFundBackfillOccurredAt(withdrawal.AppliedAt, withdrawal.CreatedAt))
	reserve.ActorType = "user"
	reserve.ActorId = withdrawal.UserId
	reserve.ExternalRef = withdrawal.TradeNo
	reserve.Remark = remark
	reserveLegs := make([]PromotionFundTransactionLeg, 0, len(amounts)*2)
	for _, item := range amounts {
		reserveLegs = append(reserveLegs,
			PromotionFundTransactionLeg{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: currency, Amount: -item.AmountCents, SourceType: sourceType, SourceId: item.LedgerId},
			PromotionFundTransactionLeg{Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash, Currency: currency, Amount: item.AmountCents, SourceType: sourceType, SourceId: item.LedgerId},
		)
	}
	if err := createPromotionFundBackfillTransitionTx(tx, reserve, reserveLegs, fmt.Sprintf("withdrawal:%d:reserved", withdrawal.Id)); err != nil {
		return err
	}

	if withdrawal.Status == PromotionWithdrawalStatusRejected || withdrawal.Status == PromotionWithdrawalStatusFailed {
		release := newPromotionFundBackfillTransaction("promotion_withdrawals", withdrawal.Id, withdrawal.UserId, "released", PromotionFundKindCommissionWithdrawalReleased, promotionFundBackfillOccurredAt(withdrawal.ReviewedAt, withdrawal.CreatedAt))
		release.ReversesTransactionId = reserve.Id
		release.ActorType = "admin"
		release.ActorId = withdrawal.ReviewerId
		release.ExternalRef = withdrawal.TradeNo
		release.Remark = remark
		releaseLegs := make([]PromotionFundTransactionLeg, 0, len(amounts)*2)
		for _, item := range amounts {
			releaseLegs = append(releaseLegs,
				PromotionFundTransactionLeg{Account: PromotionFundAccountCommissionReserved, Asset: PromotionFundAssetCash, Currency: currency, Amount: -item.AmountCents, SourceType: sourceType, SourceId: item.LedgerId},
				PromotionFundTransactionLeg{Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash, Currency: currency, Amount: item.AmountCents, SourceType: sourceType, SourceId: item.LedgerId},
			)
		}
		return createPromotionFundBackfillTransitionTx(tx, release, releaseLegs, fmt.Sprintf("withdrawal:%d:released", withdrawal.Id))
	}

	if withdrawal.Status != PromotionWithdrawalStatusPaid {
		return nil
	}
	payout := newPromotionFundBackfillTransaction("promotion_withdrawals", withdrawal.Id, withdrawal.UserId, "paid", PromotionFundKindCommissionWithdrawalPaid, promotionFundBackfillOccurredAt(withdrawal.PaidAt, promotionFundBackfillOccurredAt(withdrawal.ReviewedAt, withdrawal.CreatedAt)))
	payout.ActorType = "admin"
	payout.ActorId = withdrawal.ReviewerId
	payout.ExternalRef = withdrawal.TradeNo
	payout.Remark = remark
	payoutLegs := make([]PromotionFundTransactionLeg, 0, len(amounts))
	for _, item := range amounts {
		payoutLegs = append(payoutLegs, PromotionFundTransactionLeg{
			Account:    PromotionFundAccountCommissionReserved,
			Asset:      PromotionFundAssetCash,
			Currency:   currency,
			Amount:     -item.AmountCents,
			SourceType: sourceType,
			SourceId:   item.LedgerId,
		})
	}
	return createPromotionFundBackfillTransitionTx(tx, payout, payoutLegs, fmt.Sprintf("withdrawal:%d:paid", withdrawal.Id))
}

func loadPromotionCommissionTransferAggregatesTx(tx *gorm.DB, events []PromotionEvent) (map[string]promotionFundCommissionTransferAggregate, error) {
	userIds := make([]int, 0)
	transferredAts := make([]int64, 0)
	seenUsers := make(map[int]struct{})
	seenTimes := make(map[int64]struct{})
	for _, event := range events {
		if event.EventType != PromotionEventTypeCommissionTransferred || event.UserId <= 0 || event.CreatedAt <= 0 {
			continue
		}
		if _, exists := seenUsers[event.UserId]; !exists {
			seenUsers[event.UserId] = struct{}{}
			userIds = append(userIds, event.UserId)
		}
		if _, exists := seenTimes[event.CreatedAt]; !exists {
			seenTimes[event.CreatedAt] = struct{}{}
			transferredAts = append(transferredAts, event.CreatedAt)
		}
	}
	aggregates := make(map[string]promotionFundCommissionTransferAggregate)
	if len(userIds) == 0 || len(transferredAts) == 0 {
		return aggregates, nil
	}
	var rows []promotionFundCommissionTransferAggregate
	err := tx.Model(&PromotionCommissionLedger{}).
		Select("user_id, transferred_at, currency, COALESCE(SUM(net_amount_cents), 0) AS amount_cents, COALESCE(SUM(quota_equivalent), 0) AS quota").
		Where("user_id IN ? AND transferred_at IN ?", userIds, transferredAts).
		Group("user_id, transferred_at, currency").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		aggregates[promotionFundCommissionTransferAggregateKey(row.UserId, row.TransferredAt, row.Currency)] = row
	}
	return aggregates, nil
}

func promotionFundCommissionTransferAggregateKey(userId int, transferredAt int64, currency string) string {
	return fmt.Sprintf("%d:%d:%s", userId, transferredAt, strings.ToUpper(strings.TrimSpace(currency)))
}

func backfillLegacyPromotionTransferEventTx(tx *gorm.DB, event *PromotionEvent, aggregates map[string]promotionFundCommissionTransferAggregate) error {
	if event == nil {
		return nil
	}
	if event.UserId <= 0 {
		return fmt.Errorf("invalid promotion transfer event %d for fund backfill", event.Id)
	}
	if event.EventType == PromotionEventTypePromotionRewardTransferred {
		var realtime PromotionFundTransaction
		err := tx.Preload("Legs", func(legTx *gorm.DB) *gorm.DB {
			return legTx.Order("id ASC")
		}).Where("transaction_key = ?", fmt.Sprintf("invitation_transfer:%d", event.Id)).First(&realtime).Error
		if err == nil {
			if realtime.Kind != PromotionFundKindInvitationRewardTransferred ||
				realtime.UserId != event.UserId ||
				realtime.SourceType != "promotion_events" ||
				realtime.SourceId != event.Id {
				return fmt.Errorf("%w: realtime invitation transfer %d", ErrPromotionFundTransactionConflict, event.Id)
			}
			var referralAmount int64
			var walletAmount int64
			for _, leg := range realtime.Legs {
				if leg.Asset != PromotionFundAssetQuota || leg.Currency != "" {
					return fmt.Errorf("%w: realtime invitation transfer %d", ErrPromotionFundTransactionConflict, event.Id)
				}
				switch leg.Account {
				case PromotionFundAccountReferralCredit:
					referralAmount += leg.Amount
				case PromotionFundAccountAPIBalance:
					walletAmount += leg.Amount
				default:
					return fmt.Errorf("%w: realtime invitation transfer %d", ErrPromotionFundTransactionConflict, event.Id)
				}
			}
			if event.QuotaDelta <= 0 || referralAmount != -int64(event.QuotaDelta) || walletAmount != int64(event.QuotaDelta) {
				return fmt.Errorf("%w: realtime invitation transfer %d", ErrPromotionFundTransactionConflict, event.Id)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if event.EventType == PromotionEventTypeCommissionTransferred {
		currency := strings.ToUpper(strings.TrimSpace(event.Currency))
		aggregate, exists := aggregates[promotionFundCommissionTransferAggregateKey(event.UserId, event.CreatedAt, currency)]
		if exists && aggregate.AmountCents == event.CashAmountCents && aggregate.Quota == int64(event.QuotaDelta) {
			return nil
		}
	}

	transaction := &PromotionFundTransaction{
		TransactionKey: fmt.Sprintf("pfb:promotion_events:%d:legacy_aggregate", event.Id),
		Kind:           PromotionFundKindLegacyAggregate,
		UserId:         event.UserId,
		SourceType:     PromotionFundSourceLegacyAggregate,
		SourceId:       event.Id,
		SourceKey:      fmt.Sprintf("promotion_events:%d", event.Id),
		ActorType:      "system",
		ActorRef:       promotionFundBackfillActorRef,
		ExternalRef:    event.EventKey,
		OccurredAt:     promotionFundBackfillOccurredAt(event.CreatedAt, 0),
		Remark:         joinPromotionFundBackfillRemarks(event.Remark, "legacy aggregate transfer; per-source allocation is unavailable"),
	}
	legs := make([]PromotionFundTransactionLeg, 0, 2)
	switch event.EventType {
	case PromotionEventTypePromotionRewardTransferred:
		if event.QuotaDelta <= 0 {
			return fmt.Errorf("invalid quota on legacy promotion transfer event %d", event.Id)
		}
		amount := int64(event.QuotaDelta)
		legs = append(legs,
			PromotionFundTransactionLeg{Account: PromotionFundAccountReferralCredit, Asset: PromotionFundAssetQuota, Amount: -amount, SourceType: PromotionFundSourceLegacyAggregate, SourceId: event.Id},
			PromotionFundTransactionLeg{Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota, Amount: amount, SourceType: PromotionFundSourceLegacyAggregate, SourceId: event.Id},
		)
	case PromotionEventTypeCommissionTransferred:
		if event.CashAmountCents < 0 || event.QuotaDelta < 0 || (event.CashAmountCents == 0 && event.QuotaDelta == 0) {
			return fmt.Errorf("invalid amounts on legacy commission transfer event %d", event.Id)
		}
		if event.CashAmountCents > 0 {
			legs = append(legs, PromotionFundTransactionLeg{
				Account: PromotionFundAccountCommissionAvailable, Asset: PromotionFundAssetCash,
				Currency: strings.ToUpper(strings.TrimSpace(event.Currency)), Amount: -event.CashAmountCents,
				SourceType: PromotionFundSourceLegacyAggregate, SourceId: event.Id,
			})
		}
		if event.QuotaDelta > 0 {
			legs = append(legs, PromotionFundTransactionLeg{
				Account: PromotionFundAccountAPIBalance, Asset: PromotionFundAssetQuota,
				Amount: int64(event.QuotaDelta), SourceType: PromotionFundSourceLegacyAggregate, SourceId: event.Id,
			})
		}
	default:
		return nil
	}
	return createPromotionFundBackfillTransitionTx(tx, transaction, legs)
}
