package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                  int     `json:"id"`
	UserId              int     `json:"user_id" gorm:"index"`
	Purpose             string  `json:"purpose" gorm:"type:varchar(32);index"`
	Amount              int64   `json:"amount"`
	Money               float64 `json:"money"`
	CreditedQuota       int     `json:"credited_quota" gorm:"type:int"`
	PaidAmountMinor     int64   `json:"paid_amount_minor"`
	PaidCurrency        string  `json:"paid_currency" gorm:"type:varchar(3);index"`
	PaidAmountVerified  bool    `json:"paid_amount_verified"`
	ProviderPaymentId   string  `json:"provider_payment_id" gorm:"type:varchar(191);index"`
	PaymentVerifiedAt   int64   `json:"payment_verified_at" gorm:"index"`
	RefundStatus        string  `json:"refund_status" gorm:"type:varchar(32);index"`
	RefundedAmountMinor int64   `json:"refunded_amount_minor"`
	RefundedQuota       int     `json:"refunded_quota" gorm:"type:int"`
	RefundedAt          int64   `json:"refunded_at" gorm:"index"`
	TradeNo             string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod       string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider     string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime          int64   `json:"create_time"`
	CompleteTime        int64   `json:"complete_time"`
	Status              string  `json:"status"`
}

const (
	TopUpPurposeAPIBalance   = "api_balance"
	TopUpPurposeSubscription = "subscription"

	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

func (topUp *TopUp) BeforeCreate(_ *gorm.DB) error {
	if topUp.Purpose == "" {
		topUp.Purpose = TopUpPurposeAPIBalance
	}
	return nil
}

// BackfillTopUpPurposes classifies rows created before TopUp.Purpose existed.
// A matching subscription order is authoritative; every other legacy row is
// an API-balance top-up. This keeps subscription compatibility rows out of
// first-top-up rewards without guessing from payment method or amount.
func BackfillTopUpPurposes() error {
	matchingSubscriptionOrder := DB.Model(&SubscriptionOrder{}).Select("1").
		Where("subscription_orders.trade_no = top_ups.trade_no").
		Where("subscription_orders.user_id = top_ups.user_id").
		Where("top_ups.payment_provider = ? OR subscription_orders.payment_provider = ? OR subscription_orders.payment_provider = top_ups.payment_provider", "", "")
	if err := DB.Model(&TopUp{}).
		Where("(purpose IS NULL OR purpose = ?) AND EXISTS (?)", "", matchingSubscriptionOrder).
		Update("purpose", TopUpPurposeSubscription).Error; err != nil {
		return err
	}
	return DB.Model(&TopUp{}).
		Where("purpose IS NULL OR purpose = ?", "").
		Update("purpose", TopUpPurposeAPIBalance).Error
}

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch    = errors.New("payment method mismatch")
	ErrTopUpNotFound            = errors.New("topup not found")
	ErrTopUpStatusInvalid       = errors.New("topup status invalid")
	ErrInvalidTopUpQuota        = errors.New("invalid top-up quota")
	ErrTopUpQuotaLimitExceeded  = errors.New("top-up quota limit exceeded")
	ErrWalletQuotaLimitExceeded = errors.New("wallet quota limit exceeded")
	ErrManualTopUpInvalid       = errors.New("manual top-up completion audit is invalid")
	ErrManualTopUpReasonNeeded  = errors.New("manual top-up completion reason is required")
)

func (topUp *TopUp) Insert() error {
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccounting(topUp, fencedUserIds); err != nil {
		return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockActiveUserForFinancialWriteTx(tx, topUp.UserId); err != nil {
			return err
		}
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
	})
	return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
}

func topUpQuotaMaxCurrent(creditedQuota int) (int, error) {
	if creditedQuota <= 0 || creditedQuota >= common.MaxQuota {
		return 0, ErrInvalidTopUpQuota
	}
	return common.MaxQuota - 1 - creditedQuota, nil
}

// ValidateTopUpQuotaCapacity performs the user-facing pre-payment check. The
// settlement path repeats the same invariant with an atomic conditional
// update, because the wallet balance can change after checkout creation.
func ValidateTopUpQuotaCapacity(userId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota > maxCurrentQuota {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

// creditTopUpQuota atomically enforces the wallet ceiling while adding quota.
// Keeping the predicate and increment in one UPDATE prevents two
// concurrent callbacks from both passing a separate read/check.
func creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int, updates map[string]interface{}) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	updateFields := make(map[string]interface{}, len(updates)+1)
	for key, value := range updates {
		updateFields[key] = value
	}
	updateFields["quota"] = gorm.Expr("quota + ?", creditedQuota)

	result := tx.Model(&User{}).
		Where("id = ? AND quota <= ?", userId, maxCurrentQuota).
		Updates(updateFields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	var count int64
	if err := tx.Model(&User{}).Where("id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrTopUpQuotaLimitExceeded
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

// RechargeEpay 原子完成易支付订单：订单行锁、状态校验、成功更新与用户额度增加
// 在同一个事务内完成，因此同一订单的并发/重复回调（包括多实例部署下）最多充值一次。
// alreadyDone=true 表示订单此前已完成，本次为幂等重复回调。
// 进程内的 LockOrder 只是优化，正确性由本函数的数据库行锁保证。
func RechargeEpay(tradeNo string, actualPaymentMethod string, payment VerifiedPayment, callerIp string) (alreadyDone bool, err error) {
	if tradeNo == "" {
		return false, errors.New("未提供支付单号")
	}
	if _, err = NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency); err != nil {
		return false, err
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var quotaToAdd int
	var rebate *InvitationRebate
	var firstTopUpReward *InvitationReward
	topUp := &TopUp{}
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccountingByTrade(tradeNo, PaymentProviderEpay, fencedUserIds); err != nil {
		return false, errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			if err := topUp.verifyStoredPayment(payment); err != nil {
				return err
			}
			alreadyDone = true
			if _, err := ensureTopUpFundTransactionTx(tx, topUp); err != nil {
				return err
			}
			return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if actualPaymentMethod != "" && topUp.PaymentMethod != actualPaymentMethod {
			topUp.PaymentMethod = actualPaymentMethod
		}
		if err := topUp.setVerifiedPayment(payment); err != nil {
			return err
		}
		var quotaErr error
		quotaToAdd, quotaErr = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if quotaErr != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.CreditedQuota = quotaToAdd
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		if err := creditTopUpQuotaWithFundTransactionTx(tx, topUp, quotaToAdd, nil, topUpFundActor{
			ActorType: "provider", ActorRef: topUp.PaymentProvider,
		}); err != nil {
			return err
		}
		rebate, err = SettleInvitationRebateTx(tx, topUp)
		if err != nil {
			return err
		}
		firstTopUpReward, err = SettleInvitationMilestoneRewardTx(tx, topUp.UserId, InvitationRewardTypeFirstTopUp)
		if err != nil {
			return err
		}
		return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
	})
	err = errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	if err != nil {
		if !errors.Is(err, ErrTopUpNotFound) && !errors.Is(err, ErrPaymentMethodMismatch) && !errors.Is(err, ErrTopUpStatusInvalid) {
			common.SysError("epay topup failed: " + err.Error())
		}
		return false, err
	}
	if alreadyDone {
		return true, nil
	}
	invalidateUserQuotaCacheAfterDBWrite(topUp.UserId, "epay topup")

	common.SysLog(fmt.Sprintf("易支付充值成功 trade_no=%s user_id=%d quota_to_add=%d money=%.2f", topUp.TradeNo, topUp.UserId, quotaToAdd, topUp.Money))
	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentProviderEpay)
	RecordInvitationRebateLog(rebate)
	RecordInvitationMilestoneRewardLog(firstTopUpReward)
	return false, nil
}

func RechargeStripe(referenceId string, customerId string, payment VerifiedPayment, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}
	if _, err = NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency); err != nil {
		return err
	}

	var quota int
	var rebate *InvitationRebate
	var firstTopUpReward *InvitationReward
	alreadyDone := false
	topUp := &TopUp{}
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccountingByTrade(referenceId, PaymentProviderStripe, fencedUserIds); err != nil {
		return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			if err := topUp.verifyStoredPayment(payment); err != nil {
				return err
			}
			alreadyDone = true
			if _, err := ensureTopUpFundTransactionTx(tx, topUp); err != nil {
				return err
			}
			return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		if err := topUp.setVerifiedPayment(payment); err != nil {
			return err
		}

		quota, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quota <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.CreditedQuota = quota
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}
		if err := creditTopUpQuotaWithFundTransactionTx(tx, topUp, quota, map[string]interface{}{
			"stripe_customer": customerId,
		}, topUpFundActor{ActorType: "provider", ActorRef: topUp.PaymentProvider}); err != nil {
			return err
		}
		rebate, err = SettleInvitationRebateTx(tx, topUp)
		if err != nil {
			return err
		}
		firstTopUpReward, err = SettleInvitationMilestoneRewardTx(tx, topUp.UserId, InvitationRewardTypeFirstTopUp)
		if err != nil {
			return err
		}
		return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
	})
	err = errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if alreadyDone {
		return nil
	}
	invalidateUserQuotaCacheAfterDBWrite(topUp.UserId, "stripe topup")

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(quota), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentMethodStripe)
	RecordInvitationRebateLog(rebate)
	RecordInvitationMilestoneRewardLog(firstTopUpReward)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

type ManualTopUpCompletionInput struct {
	TradeNo  string
	CallerIp string
	ActorId  int
	ActorRef string
	Reason   string
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(input ManualTopUpCompletionInput) error {
	input.TradeNo = strings.TrimSpace(input.TradeNo)
	input.ActorRef = strings.TrimSpace(input.ActorRef)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.TradeNo == "" || input.ActorId <= 0 || input.ActorRef == "" || len(input.ActorRef) > 191 ||
		utf8.RuneCountInString(input.Reason) > 1000 {
		return ErrManualTopUpInvalid
	}
	if input.Reason == "" {
		return ErrManualTopUpReasonNeeded
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string
	var rebate *InvitationRebate
	var firstTopUpReward *InvitationReward
	alreadyDone := false
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccountingByTrade(input.TradeNo, "", fencedUserIds); err != nil {
		return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := lockForUpdate(tx).Where(refCol+" = ?", input.TradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}
		if topUp.Purpose != TopUpPurposeAPIBalance {
			return ErrTopUpFundTransactionInvalid
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			alreadyDone = true
			if _, err := ensureTopUpFundTransactionTx(tx, topUp); err != nil {
				return err
			}
			return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}
		lockedUsers, err := lockActiveUsersForFinancialWriteTx(tx, input.ActorId, topUp.UserId)
		if err != nil {
			return err
		}
		actor := lockedUsers[input.ActorId]
		if actor.Role != common.RoleAdminUser && actor.Role != common.RoleRootUser {
			return ErrManualTopUpInvalid
		}

		// 计算应充值额度：
		// - Stripe 订单：Money 代表经分组倍率换算后的美元数量，直接 * QuotaPerUnit
		// - 其他订单（如易支付）：Amount 为美元数量，* QuotaPerUnit
		var quotaErr error
		if topUp.PaymentProvider == PaymentProviderStripe {
			quotaToAdd, quotaErr = common.WalletQuotaFromDecimalStrict(
				decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
			)
		} else {
			quotaToAdd, quotaErr = common.WalletQuotaFromDecimalStrict(
				decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
			)
		}
		if quotaErr != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		// 标记完成
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.CreditedQuota = quotaToAdd
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := creditTopUpQuotaWithFundTransactionTx(tx, topUp, quotaToAdd, nil, topUpFundActor{
			ActorType: "admin", ActorId: input.ActorId, ActorRef: input.ActorRef, Remark: input.Reason,
		}); err != nil {
			return err
		}
		settledRebate, settleErr := SettleInvitationRebateTx(tx, topUp)
		if settleErr != nil {
			return settleErr
		}
		rebate = settledRebate
		settledReward, settleErr := SettleInvitationMilestoneRewardTx(tx, topUp.UserId, InvitationRewardTypeFirstTopUp)
		if settleErr != nil {
			return settleErr
		}
		firstTopUpReward = settledReward
		if err := reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true); err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})
	err = errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))

	if err != nil {
		return err
	}
	if alreadyDone {
		return nil
	}

	// 事务外记录日志，避免阻塞
	invalidateUserQuotaCacheAfterDBWrite(userId, "manual topup")
	RecordTopupLog(userId, fmt.Sprintf("管理员 %s (#%d) 补单成功，充值金额: %v，支付金额：%f，原因：%s",
		input.ActorRef, input.ActorId, logger.FormatQuota(quotaToAdd), payMoney, input.Reason), input.CallerIp, paymentMethod, "admin")
	RecordInvitationRebateLog(rebate)
	RecordInvitationMilestoneRewardLog(firstTopUpReward)
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, payment VerifiedPayment, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}
	if _, err = NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency); err != nil {
		return err
	}

	var quota int
	var rebate *InvitationRebate
	var firstTopUpReward *InvitationReward
	alreadyDone := false
	topUp := &TopUp{}
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccountingByTrade(referenceId, PaymentProviderCreem, fencedUserIds); err != nil {
		return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			if err := topUp.verifyStoredPayment(payment); err != nil {
				return err
			}
			alreadyDone = true
			if _, err := ensureTopUpFundTransactionTx(tx, topUp); err != nil {
				return err
			}
			return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		if err := topUp.setVerifiedPayment(payment); err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota, err = common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount))
		if err != nil || quota <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.CreditedQuota = quota
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		if err := creditTopUpQuotaWithFundTransactionTx(tx, topUp, quota, updateFields, topUpFundActor{
			ActorType: "provider", ActorRef: topUp.PaymentProvider,
		}); err != nil {
			return err
		}
		rebate, err = SettleInvitationRebateTx(tx, topUp)
		if err != nil {
			return err
		}
		firstTopUpReward, err = SettleInvitationMilestoneRewardTx(tx, topUp.UserId, InvitationRewardTypeFirstTopUp)
		if err != nil {
			return err
		}
		return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
	})
	err = errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if alreadyDone {
		return nil
	}
	invalidateUserQuotaCacheAfterDBWrite(topUp.UserId, "creem topup")

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)
	RecordInvitationRebateLog(rebate)
	RecordInvitationMilestoneRewardLog(firstTopUpReward)

	return nil
}

func RechargeWaffo(tradeNo string, payment VerifiedPayment, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}
	if _, err = NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency); err != nil {
		return err
	}

	var quotaToAdd int
	var rebate *InvitationRebate
	var firstTopUpReward *InvitationReward
	alreadyDone := false
	topUp := &TopUp{}
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccountingByTrade(tradeNo, PaymentProviderWaffo, fencedUserIds); err != nil {
		return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			if err := topUp.verifyStoredPayment(payment); err != nil {
				return err
			}
			alreadyDone = true
			if _, err := ensureTopUpFundTransactionTx(tx, topUp); err != nil {
				return err
			}
			return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		if err := topUp.setVerifiedPayment(payment); err != nil {
			return err
		}

		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.CreditedQuota = quotaToAdd
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := creditTopUpQuotaWithFundTransactionTx(tx, topUp, quotaToAdd, nil, topUpFundActor{
			ActorType: "provider", ActorRef: topUp.PaymentProvider,
		}); err != nil {
			return err
		}
		rebate, err = SettleInvitationRebateTx(tx, topUp)
		if err != nil {
			return err
		}
		firstTopUpReward, err = SettleInvitationMilestoneRewardTx(tx, topUp.UserId, InvitationRewardTypeFirstTopUp)
		if err != nil {
			return err
		}
		return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
	})
	err = errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if alreadyDone {
		return nil
	}
	invalidateUserQuotaCacheAfterDBWrite(topUp.UserId, "waffo topup")

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}
	RecordInvitationRebateLog(rebate)
	RecordInvitationMilestoneRewardLog(firstTopUpReward)

	return nil
}

func RechargeWaffoPancake(tradeNo string, payment VerifiedPayment) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}
	if _, err = NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency); err != nil {
		return err
	}

	var quotaToAdd int
	var rebate *InvitationRebate
	var firstTopUpReward *InvitationReward
	alreadyDone := false
	topUp := &TopUp{}
	fencedUserIds := newRefundHoldFenceScope()
	if err := preparePromotionRefundTopUpAccountingByTrade(tradeNo, PaymentProviderWaffoPancake, fencedUserIds); err != nil {
		return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			if err := topUp.verifyStoredPayment(payment); err != nil {
				return err
			}
			alreadyDone = true
			if _, err := ensureTopUpFundTransactionTx(tx, topUp); err != nil {
				return err
			}
			return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		if err := topUp.setVerifiedPayment(payment); err != nil {
			return err
		}

		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		topUp.CreditedQuota = quotaToAdd
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := creditTopUpQuotaWithFundTransactionTx(tx, topUp, quotaToAdd, nil, topUpFundActor{
			ActorType: "provider", ActorRef: topUp.PaymentProvider,
		}); err != nil {
			return err
		}
		rebate, err = SettleInvitationRebateTx(tx, topUp)
		if err != nil {
			return err
		}
		firstTopUpReward, err = SettleInvitationMilestoneRewardTx(tx, topUp.UserId, InvitationRewardTypeFirstTopUp)
		if err != nil {
			return err
		}
		return reconcilePromotionRefundForTopUpTx(tx, topUp, fencedUserIds, true)
	})
	err = errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if alreadyDone {
		return nil
	}
	invalidateUserQuotaCacheAfterDBWrite(topUp.UserId, "waffo pancake topup")

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}
	RecordInvitationRebateLog(rebate)
	RecordInvitationMilestoneRewardLog(firstTopUpReward)

	return nil
}
