package model

import "errors"

const (
	PromotionRefundKindFull    = "full_refund"
	PromotionRefundKindPartial = "partial_refund"
	PromotionRefundKindDispute = "dispute"

	PromotionRefundIntakeProviderWebhook = "provider_webhook"
	PromotionRefundIntakeOfflineRefund   = "offline_refund"
	PromotionRefundIntakeProviderRefund  = "provider_refund"
	PromotionRefundIntakeChargeback      = "chargeback"
	PromotionRefundIntakeMissedCallback  = "missed_callback"

	PromotionRefundCaseStatusPendingReview = "pending_review"
	PromotionRefundCaseStatusResolved      = "resolved"

	TopUpRefundStatusPartial  = "partial"
	TopUpRefundStatusFull     = "full"
	TopUpRefundStatusDisputed = "disputed"
)

var ErrPromotionRefundEventConflict = errors.New("refund event key was already used for a different payload")

type PromotionRefundCase struct {
	Id                               int                              `json:"id"`
	EventKey                         string                           `json:"event_key" gorm:"type:varchar(96);uniqueIndex"`
	PayloadHash                      string                           `json:"payload_hash" gorm:"type:char(40);index"`
	Provider                         string                           `json:"provider" gorm:"type:varchar(50);index"`
	TradeNo                          string                           `json:"trade_no" gorm:"type:varchar(255);index"`
	RefundTradeNo                    string                           `json:"refund_trade_no" gorm:"type:varchar(255);index"`
	Kind                             string                           `json:"kind" gorm:"type:varchar(32);index"`
	PaidAmountMinor                  int64                            `json:"paid_amount_minor"`
	RefundedAmountMinor              int64                            `json:"refunded_amount_minor"`
	Currency                         string                           `json:"currency" gorm:"type:varchar(3);index"`
	TopUpId                          int                              `json:"top_up_id" gorm:"index"`
	UserId                           int                              `json:"user_id" gorm:"index"`
	QuotaAmount                      int                              `json:"quota_amount" gorm:"type:int"`
	WalletDebitedQuota               int                              `json:"wallet_debited_quota" gorm:"type:int"`
	DebtCreatedQuota                 int64                            `json:"debt_created_quota" gorm:"type:bigint"`
	CashDebtCreatedMinor             int64                            `json:"cash_debt_created_minor" gorm:"type:bigint"`
	InvitationRebateId               int                              `json:"invitation_rebate_id" gorm:"index"`
	CommissionLedgerId               int                              `json:"commission_ledger_id" gorm:"index"`
	ResponsibilityFingerprint        string                           `json:"responsibility_fingerprint,omitempty" gorm:"type:char(40);index"`
	ResponsibilityIntegrityError     string                           `json:"responsibility_integrity_error,omitempty" gorm:"-"`
	Status                           string                           `json:"status" gorm:"type:varchar(32);index"`
	RequiresRootReview               bool                             `json:"requires_root_review" gorm:"index"`
	AccountingMigrationVersion       int                              `json:"accounting_migration_version" gorm:"index"`
	AccountingMigratedAt             int64                            `json:"accounting_migrated_at" gorm:"index"`
	Reason                           string                           `json:"reason" gorm:"type:text"`
	IntakeSource                     string                           `json:"intake_source" gorm:"type:varchar(32);index"`
	IntakeFingerprint                string                           `json:"intake_fingerprint" gorm:"type:char(40);index"`
	InitiatorType                    string                           `json:"initiator_type" gorm:"type:varchar(32);index"`
	InitiatorId                      int                              `json:"initiator_id" gorm:"index"`
	InitiatorRole                    int                              `json:"initiator_role" gorm:"index"`
	ReviewerId                       int                              `json:"reviewer_id" gorm:"index"`
	ReviewNote                       string                           `json:"review_note" gorm:"type:text"`
	CreatedAt                        int64                            `json:"created_at" gorm:"index"`
	ResolvedAt                       int64                            `json:"resolved_at" gorm:"index"`
	Obligations                      []*PromotionRefundObligation     `json:"obligations,omitempty" gorm:"-"`
	Actions                          []*PromotionRefundAction         `json:"actions,omitempty" gorm:"-"`
	ResponsibleUsers                 []PromotionRefundResponsibleUser `json:"responsible_users,omitempty" gorm:"-"`
	CommissionLedgerStatus           string                           `json:"commission_ledger_status" gorm:"-"`
	CommissionReconciliationRequired bool                             `json:"commission_reconciliation_required" gorm:"-"`
	SubscriptionOrderId              int                              `json:"subscription_order_id" gorm:"-"`
	UserSubscriptionId               int                              `json:"user_subscription_id" gorm:"-"`
	SubscriptionPlanId               int                              `json:"subscription_plan_id" gorm:"-"`
	SubscriptionStatus               string                           `json:"subscription_status" gorm:"-"`
	SubscriptionStartTime            int64                            `json:"subscription_start_time" gorm:"-"`
	SubscriptionEndTime              int64                            `json:"subscription_end_time" gorm:"-"`
	SubscriptionAmountTotal          int64                            `json:"subscription_amount_total" gorm:"-"`
	SubscriptionAmountUsed           int64                            `json:"subscription_amount_used" gorm:"-"`
}

// PromotionRefundCaseUser preserves every user who was put on hold for a
// refund case. The public recovery projection uses this durable relationship
// because a responsibility may exist before an obligation is created and the
// source rebate or commission rows can change during review.
type PromotionRefundCaseUser struct {
	Id           int   `json:"id"`
	RefundCaseId int   `json:"refund_case_id" gorm:"not null;uniqueIndex:idx_promotion_refund_case_user,priority:1"`
	UserId       int   `json:"user_id" gorm:"not null;uniqueIndex:idx_promotion_refund_case_user,priority:2;index:idx_promotion_refund_user_case,priority:1"`
	CreatedAt    int64 `json:"created_at" gorm:"not null;index"`
}

func (PromotionRefundCaseUser) TableName() string {
	return "promotion_refund_case_users"
}

type PromotionRefundResponsibleUser struct {
	UserId                      int    `json:"user_id"`
	Username                    string `json:"username"`
	IsTopUpUser                 bool   `json:"is_top_up_user"`
	IsRebateRecipient           bool   `json:"is_rebate_recipient"`
	InvitationRebateId          int    `json:"invitation_rebate_id"`
	RebateAmountMinor           int64  `json:"rebate_amount_minor"`
	RebateQuota                 int    `json:"rebate_quota"`
	RebateCurrency              string `json:"rebate_currency"`
	IsCommissionRecipient       bool   `json:"is_commission_recipient"`
	CommissionLedgerId          int    `json:"commission_ledger_id"`
	CommissionAmountMinor       int64  `json:"commission_amount_minor"`
	CommissionQuota             int    `json:"commission_quota"`
	CommissionCurrency          string `json:"commission_currency"`
	IsInvitationRewardRecipient bool   `json:"is_invitation_reward_recipient"`
	InvitationRewardId          int    `json:"invitation_reward_id"`
	InvitationRewardQuota       int    `json:"invitation_reward_quota"`
	InvitationTransferredQuota  int    `json:"invitation_transferred_quota"`
}

type PromotionRefundInput struct {
	Provider      string
	TradeNo       string
	RefundTradeNo string
	// EquivalentRefundTradeNos are provider references taken from the same
	// verified webhook as RefundTradeNo. They allow webhook reference-selection
	// upgrades to recognize a case created by an older application version.
	EquivalentRefundTradeNos []string
	Kind                     string
	PaidAmountMinor          int64
	RefundedAmountMinor      int64
	Currency                 string
	Remark                   string
	AmountIsCumulative       bool
	adminIdempotencyKey      string
	intakeSource             string
	initiatorId              int
	initiatorRole            int
}
