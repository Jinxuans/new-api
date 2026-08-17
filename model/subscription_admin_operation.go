package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SubscriptionAdminOperationGrant      = "grant"
	SubscriptionAdminOperationInvalidate = "invalidate"
	SubscriptionAdminOperationUserReset  = "user_reset"
	SubscriptionAdminOperationPlanReset  = "plan_reset"

	maxSubscriptionAdminReasonRunes = 1000
	maxSubscriptionAdminKeyRunes    = 128
)

var (
	ErrSubscriptionAdminOperationInvalid    = errors.New("subscription administrator operation is invalid")
	ErrSubscriptionAdminReasonRequired      = errors.New("subscription administrator operation reason is required")
	ErrSubscriptionAdminOperationConflict   = errors.New("subscription administrator idempotency key conflicts with a different operation")
	ErrSubscriptionAdminOperationImmutable  = errors.New("subscription administrator operation evidence is immutable")
	ErrSubscriptionAdminEntitlementInactive = errors.New("subscription entitlement is not active")
)

type SubscriptionAdminOperation struct {
	Id               int                              `json:"id"`
	OperationKey     string                           `json:"operation_key" gorm:"type:varchar(128);not null;uniqueIndex"`
	Kind             string                           `json:"kind" gorm:"type:varchar(32);not null;index"`
	ActorId          int                              `json:"actor_id" gorm:"not null;index"`
	ActorRole        int                              `json:"actor_role" gorm:"not null"`
	ActorRef         string                           `json:"actor_ref" gorm:"type:varchar(191)"`
	TargetUserId     int                              `json:"target_user_id" gorm:"index"`
	PlanId           int                              `json:"plan_id" gorm:"not null;index"`
	PlanTitle        string                           `json:"plan_title" gorm:"type:varchar(255)"`
	AdvanceResetTime bool                             `json:"advance_reset_time"`
	Reason           string                           `json:"reason" gorm:"type:text;not null"`
	CreatedAt        int64                            `json:"created_at" gorm:"not null;index"`
	Items            []SubscriptionAdminOperationItem `json:"items" gorm:"foreignKey:OperationId"`
}

type SubscriptionAdminOperationItem struct {
	Id                    int    `json:"id"`
	OperationId           int    `json:"operation_id" gorm:"not null;uniqueIndex:idx_subscription_admin_operation_item,priority:1"`
	UserSubscriptionId    int    `json:"user_subscription_id" gorm:"not null;uniqueIndex:idx_subscription_admin_operation_item,priority:2;index"`
	UserId                int    `json:"user_id" gorm:"not null;index"`
	AmountUsedBefore      int64  `json:"amount_used_before" gorm:"type:bigint;not null"`
	AmountUsedAfter       int64  `json:"amount_used_after" gorm:"type:bigint;not null"`
	QuotaGenerationBefore int64  `json:"quota_generation_before" gorm:"type:bigint;not null"`
	QuotaGenerationAfter  int64  `json:"quota_generation_after" gorm:"type:bigint;not null"`
	StatusBefore          string `json:"status_before" gorm:"type:varchar(32);not null"`
	StatusAfter           string `json:"status_after" gorm:"type:varchar(32);not null"`
	EndTimeBefore         int64  `json:"end_time_before" gorm:"type:bigint;not null"`
	EndTimeAfter          int64  `json:"end_time_after" gorm:"type:bigint;not null"`
	UserGroupBefore       string `json:"user_group_before" gorm:"type:varchar(64);not null"`
	UserGroupAfter        string `json:"user_group_after" gorm:"type:varchar(64);not null"`
	CreatedAt             int64  `json:"created_at" gorm:"not null;index"`
}

type AdminSubscriptionOperationInput struct {
	UserSubscriptionId int
	UserId             int
	PlanId             int
	AdvanceResetTime   bool
	ActorId            int
	ActorRole          int
	ActorRef           string
	Reason             string
	IdempotencyKey     string
}

type SubscriptionInvalidationResult struct {
	UserSubscriptionId int    `json:"user_subscription_id"`
	UserId             int    `json:"user_id"`
	PlanId             int    `json:"plan_id"`
	StatusBefore       string `json:"status_before"`
	StatusAfter        string `json:"status_after"`
	EndTimeBefore      int64  `json:"end_time_before"`
	EndTimeAfter       int64  `json:"end_time_after"`
	UserGroupBefore    string `json:"user_group_before"`
	UserGroupAfter     string `json:"user_group_after"`
	Message            string `json:"message,omitempty"`
	Replayed           bool   `json:"replayed"`
}

func (*SubscriptionAdminOperation) BeforeUpdate(_ *gorm.DB) error {
	return ErrSubscriptionAdminOperationImmutable
}

func (*SubscriptionAdminOperation) BeforeDelete(_ *gorm.DB) error {
	return ErrSubscriptionAdminOperationImmutable
}

func (*SubscriptionAdminOperationItem) BeforeUpdate(_ *gorm.DB) error {
	return ErrSubscriptionAdminOperationImmutable
}

func (*SubscriptionAdminOperationItem) BeforeDelete(_ *gorm.DB) error {
	return ErrSubscriptionAdminOperationImmutable
}

func normalizeAdminSubscriptionEvidence(input *AdminSubscriptionOperationInput) error {
	if input == nil || input.ActorId <= 0 ||
		(input.ActorRole != common.RoleAdminUser && input.ActorRole != common.RoleRootUser) {
		return ErrSubscriptionAdminOperationInvalid
	}
	input.ActorRef = strings.TrimSpace(input.ActorRef)
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if utf8.RuneCountInString(input.ActorRef) > 191 || input.IdempotencyKey == "" ||
		utf8.RuneCountInString(input.IdempotencyKey) > maxSubscriptionAdminKeyRunes {
		return ErrSubscriptionAdminOperationInvalid
	}
	if input.Reason == "" {
		return ErrSubscriptionAdminReasonRequired
	}
	if utf8.RuneCountInString(input.Reason) > maxSubscriptionAdminReasonRunes {
		return ErrSubscriptionAdminOperationInvalid
	}
	return nil
}

func normalizeAdminSubscriptionOperationInput(input *AdminSubscriptionOperationInput, requireUser bool) error {
	if err := normalizeAdminSubscriptionEvidence(input); err != nil {
		return err
	}
	if input.PlanId <= 0 || input.UserSubscriptionId != 0 ||
		(requireUser && input.UserId <= 0) || (!requireUser && input.UserId != 0) {
		return ErrSubscriptionAdminOperationInvalid
	}
	return nil
}

func validateSubscriptionAdminActor(lockedUsers map[int]*User, input *AdminSubscriptionOperationInput) error {
	if input == nil || lockedUsers == nil {
		return ErrSubscriptionAdminOperationInvalid
	}
	actor := lockedUsers[input.ActorId]
	if actor == nil || actor.Role != input.ActorRole {
		return ErrSubscriptionAdminOperationInvalid
	}
	return nil
}

func claimSubscriptionAdminOperationTx(tx *gorm.DB, kind string, input *AdminSubscriptionOperationInput, plan *SubscriptionPlan) (*SubscriptionAdminOperation, bool, error) {
	if tx == nil || input == nil || plan == nil {
		return nil, false, ErrSubscriptionAdminOperationInvalid
	}
	operation := &SubscriptionAdminOperation{
		OperationKey:     fmt.Sprintf("subscription_admin:%s:%s", kind, common.Sha1([]byte(input.IdempotencyKey))),
		Kind:             kind,
		ActorId:          input.ActorId,
		ActorRole:        input.ActorRole,
		ActorRef:         input.ActorRef,
		TargetUserId:     input.UserId,
		PlanId:           input.PlanId,
		PlanTitle:        plan.Title,
		AdvanceResetTime: input.AdvanceResetTime,
		Reason:           input.Reason,
		CreatedAt:        getDBTimestamp(tx),
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "operation_key"}},
		DoNothing: true,
	}).Create(operation)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 && operation.Id > 0 {
		return operation, false, nil
	}

	var existing SubscriptionAdminOperation
	if err := lockForUpdate(tx).Preload("Items", func(itemTx *gorm.DB) *gorm.DB {
		return itemTx.Order("id ASC")
	}).Where("operation_key = ?", operation.OperationKey).First(&existing).Error; err != nil {
		return nil, false, err
	}
	if existing.Kind != operation.Kind || existing.ActorId != operation.ActorId ||
		existing.ActorRole != operation.ActorRole || existing.ActorRef != operation.ActorRef ||
		existing.TargetUserId != operation.TargetUserId || existing.PlanId != operation.PlanId ||
		existing.AdvanceResetTime != operation.AdvanceResetTime || existing.Reason != operation.Reason {
		return nil, false, ErrSubscriptionAdminOperationConflict
	}
	return &existing, true, nil
}

func subscriptionGrantMessageTx(tx *gorm.DB, operation *SubscriptionAdminOperation) (string, error) {
	if operation == nil || len(operation.Items) != 1 {
		return "", ErrSubscriptionAdminOperationConflict
	}
	var subscription UserSubscription
	if err := tx.Where("id = ? AND user_id = ?", operation.Items[0].UserSubscriptionId, operation.TargetUserId).
		First(&subscription).Error; err != nil {
		return "", err
	}
	if subscription.PrevUserGroup != "" && strings.TrimSpace(subscription.UpgradeGroup) != "" {
		return fmt.Sprintf("用户分组将升级到 %s", strings.TrimSpace(subscription.UpgradeGroup)), nil
	}
	return "", nil
}

func subscriptionInvalidationResultFromOperation(operation *SubscriptionAdminOperation, userSubscriptionId int) (*SubscriptionInvalidationResult, error) {
	if operation == nil || len(operation.Items) != 1 ||
		operation.Items[0].UserSubscriptionId != userSubscriptionId ||
		operation.Items[0].UserId != operation.TargetUserId {
		return nil, ErrSubscriptionAdminOperationConflict
	}
	item := operation.Items[0]
	result := &SubscriptionInvalidationResult{
		UserSubscriptionId: item.UserSubscriptionId,
		UserId:             item.UserId,
		PlanId:             operation.PlanId,
		StatusBefore:       item.StatusBefore,
		StatusAfter:        item.StatusAfter,
		EndTimeBefore:      item.EndTimeBefore,
		EndTimeAfter:       item.EndTimeAfter,
		UserGroupBefore:    item.UserGroupBefore,
		UserGroupAfter:     item.UserGroupAfter,
	}
	if result.UserGroupAfter != "" && result.UserGroupAfter != result.UserGroupBefore {
		result.Message = fmt.Sprintf("用户分组将回退到 %s", result.UserGroupAfter)
	}
	return result, nil
}

// InvalidateUserSubscriptionByAdmin records the actor, reason, idempotency
// boundary, entitlement transition, and user-group transition in the same
// transaction as the cancellation itself.
func InvalidateUserSubscriptionByAdmin(input AdminSubscriptionOperationInput) (result *SubscriptionInvalidationResult, replayed bool, err error) {
	if err := normalizeAdminSubscriptionEvidence(&input); err != nil {
		return nil, false, err
	}
	if input.UserSubscriptionId <= 0 || input.UserId != 0 || input.PlanId != 0 {
		return nil, false, ErrSubscriptionAdminOperationInvalid
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		subscription, lockedUsers, txErr := lockUserSubscriptionForAdminWriteTx(tx, input.UserSubscriptionId, input.ActorId)
		if txErr != nil {
			return txErr
		}
		if txErr = validateSubscriptionAdminActor(lockedUsers, &input); txErr != nil {
			return txErr
		}
		input.UserId = subscription.UserId
		input.PlanId = subscription.PlanId

		plan := &SubscriptionPlan{Id: subscription.PlanId, Title: fmt.Sprintf("#%d", subscription.PlanId)}
		var planSnapshot SubscriptionPlan
		txErr = tx.Select("id", "title").Where("id = ?", subscription.PlanId).First(&planSnapshot).Error
		if txErr == nil {
			plan = &planSnapshot
		} else if !errors.Is(txErr, gorm.ErrRecordNotFound) {
			return txErr
		}

		operation, isReplay, txErr := claimSubscriptionAdminOperationTx(tx, SubscriptionAdminOperationInvalidate, &input, plan)
		if txErr != nil {
			return txErr
		}
		if isReplay {
			result, txErr = subscriptionInvalidationResultFromOperation(operation, input.UserSubscriptionId)
			if txErr == nil {
				replayed = true
			}
			return txErr
		}
		if subscription.Status != "active" {
			return ErrSubscriptionAdminEntitlementInactive
		}

		groupBefore, txErr := getUserGroupByIdTx(tx, subscription.UserId)
		if txErr != nil {
			return txErr
		}
		item := SubscriptionAdminOperationItem{
			OperationId: operation.Id, UserSubscriptionId: subscription.Id, UserId: subscription.UserId,
			AmountUsedBefore: subscription.AmountUsed, QuotaGenerationBefore: subscription.QuotaGeneration,
			StatusBefore: subscription.Status, EndTimeBefore: subscription.EndTime, UserGroupBefore: groupBefore,
			CreatedAt: operation.CreatedAt,
		}
		_, changed, txErr := invalidateUserSubscriptionTx(tx, subscription, getDBTimestamp(tx))
		if txErr != nil {
			return txErr
		}
		if !changed {
			return ErrSubscriptionAdminEntitlementInactive
		}
		groupAfter, txErr := getUserGroupByIdTx(tx, subscription.UserId)
		if txErr != nil {
			return txErr
		}
		item.AmountUsedAfter = subscription.AmountUsed
		item.QuotaGenerationAfter = subscription.QuotaGeneration
		item.StatusAfter = subscription.Status
		item.EndTimeAfter = subscription.EndTime
		item.UserGroupAfter = groupAfter
		if txErr = tx.Create(&item).Error; txErr != nil {
			return txErr
		}
		operation.Items = []SubscriptionAdminOperationItem{item}
		result, txErr = subscriptionInvalidationResultFromOperation(operation, input.UserSubscriptionId)
		return txErr
	})
	if err != nil {
		return nil, false, err
	}
	result.Replayed = replayed
	if !replayed && result.UserGroupAfter != result.UserGroupBefore {
		refreshSubscriptionUserGroupCache(result.UserId, "admin subscription invalidation")
	}
	return result, replayed, nil
}

// GrantUserSubscriptionByAdmin records the actor, reason, idempotency boundary,
// and created entitlement in the same transaction as the grant itself.
func GrantUserSubscriptionByAdmin(input AdminSubscriptionOperationInput) (message string, replayed bool, err error) {
	if err := normalizeAdminSubscriptionOperationInput(&input, true); err != nil {
		return "", false, err
	}
	groupChanged := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		lockedUsers, err := lockActiveUsersForFinancialWriteTx(tx, input.ActorId, input.UserId)
		if err != nil {
			return err
		}
		if err = validateSubscriptionAdminActor(lockedUsers, &input); err != nil {
			return err
		}
		plan, err := getSubscriptionPlanByIdTx(tx, input.PlanId)
		if err != nil {
			return err
		}
		operation, isReplay, err := claimSubscriptionAdminOperationTx(tx, SubscriptionAdminOperationGrant, &input, plan)
		if err != nil {
			return err
		}
		if isReplay {
			replayed = true
			message, err = subscriptionGrantMessageTx(tx, operation)
			return err
		}
		subscription, err := CreateUserSubscriptionFromPlanTx(tx, input.UserId, plan, "admin")
		if err != nil {
			return err
		}
		groupChanged = subscription.PrevUserGroup != ""
		item := &SubscriptionAdminOperationItem{
			OperationId: operation.Id, UserSubscriptionId: subscription.Id, UserId: subscription.UserId,
			AmountUsedBefore: 0, AmountUsedAfter: subscription.AmountUsed,
			QuotaGenerationBefore: 0, QuotaGenerationAfter: subscription.QuotaGeneration,
			CreatedAt: operation.CreatedAt,
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		operation.Items = []SubscriptionAdminOperationItem{*item}
		message, err = subscriptionGrantMessageTx(tx, operation)
		return err
	})
	if err != nil {
		return "", false, err
	}
	if groupChanged && !replayed {
		refreshSubscriptionUserGroupCache(input.UserId, "admin subscription creation")
	}
	return message, replayed, nil
}

func subscriptionResetResultFromOperation(operation *SubscriptionAdminOperation) *SubscriptionResetResult {
	if operation == nil {
		return nil
	}
	userIds := make([]int, 0, len(operation.Items))
	seen := make(map[int]struct{}, len(operation.Items))
	for _, item := range operation.Items {
		if _, ok := seen[item.UserId]; ok {
			continue
		}
		seen[item.UserId] = struct{}{}
		userIds = append(userIds, item.UserId)
	}
	return &SubscriptionResetResult{
		PlanId: operation.PlanId, PlanTitle: operation.PlanTitle,
		MatchedCount: len(operation.Items), ResetCount: len(operation.Items), UserCount: len(userIds),
		AdvanceResetTime: operation.AdvanceResetTime, AffectedUserIds: userIds,
	}
}

func resetSubscriptionsByAdminTx(tx *gorm.DB, kind string, input *AdminSubscriptionOperationInput) (*SubscriptionResetResult, bool, error) {
	now := getDBTimestamp(tx)
	userIds := []int{input.ActorId}
	if kind == SubscriptionAdminOperationUserReset {
		userIds = append(userIds, input.UserId)
	} else {
		var targetUserIds []int
		if err := tx.Model(&UserSubscription{}).Distinct("user_id").
			Where("plan_id = ? AND status = ? AND end_time > ?", input.PlanId, "active", now).
			Pluck("user_id", &targetUserIds).Error; err != nil {
			return nil, false, err
		}
		userIds = append(userIds, targetUserIds...)
	}
	lockedUsers, err := lockActiveUsersForFinancialWriteTx(tx, userIds...)
	if err != nil {
		return nil, false, err
	}
	if err := validateSubscriptionAdminActor(lockedUsers, input); err != nil {
		return nil, false, err
	}

	plan, err := getSubscriptionPlanByIdTx(tx, input.PlanId)
	if err != nil {
		return nil, false, err
	}
	operation, replayed, err := claimSubscriptionAdminOperationTx(tx, kind, input, plan)
	if err != nil {
		return nil, false, err
	}
	if replayed {
		return subscriptionResetResultFromOperation(operation), true, nil
	}

	query := lockForUpdate(tx).
		Where("plan_id = ? AND status = ? AND end_time > ?", plan.Id, "active", now)
	if kind == SubscriptionAdminOperationUserReset {
		query = query.Where("user_id = ?", input.UserId)
	}
	var subscriptions []UserSubscription
	if err := query.Order("user_id ASC, end_time ASC, id ASC").Find(&subscriptions).Error; err != nil {
		return nil, false, err
	}
	if kind == SubscriptionAdminOperationUserReset && len(subscriptions) == 0 {
		return nil, false, errors.New("该用户没有有效的此套餐订阅")
	}

	operation.Items = make([]SubscriptionAdminOperationItem, 0, len(subscriptions))
	for i := range subscriptions {
		subscription := &subscriptions[i]
		if lockedUsers[subscription.UserId] == nil {
			return nil, false, ErrSubscriptionAdminOperationConflict
		}
		item := SubscriptionAdminOperationItem{
			OperationId: operation.Id, UserSubscriptionId: subscription.Id, UserId: subscription.UserId,
			AmountUsedBefore: subscription.AmountUsed, QuotaGenerationBefore: subscription.QuotaGeneration,
			CreatedAt: operation.CreatedAt,
		}
		if err := resetUserSubscriptionTx(tx, subscription, plan, now, input.AdvanceResetTime); err != nil {
			return nil, false, err
		}
		item.AmountUsedAfter = subscription.AmountUsed
		item.QuotaGenerationAfter = subscription.QuotaGeneration
		if err := tx.Create(&item).Error; err != nil {
			return nil, false, err
		}
		operation.Items = append(operation.Items, item)
	}
	return subscriptionResetResultFromOperation(operation), false, nil
}

func ResetUserSubscriptionsByPlanByAdmin(input AdminSubscriptionOperationInput) (result *SubscriptionResetResult, replayed bool, err error) {
	if err := normalizeAdminSubscriptionOperationInput(&input, true); err != nil {
		return nil, false, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		result, replayed, txErr = resetSubscriptionsByAdminTx(tx, SubscriptionAdminOperationUserReset, &input)
		return txErr
	})
	return result, replayed, err
}

func ResetPlanSubscriptionsByAdmin(input AdminSubscriptionOperationInput) (result *SubscriptionResetResult, replayed bool, err error) {
	if err := normalizeAdminSubscriptionOperationInput(&input, false); err != nil {
		return nil, false, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		result, replayed, txErr = resetSubscriptionsByAdminTx(tx, SubscriptionAdminOperationPlanReset, &input)
		return txErr
	})
	return result, replayed, err
}
