package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	InvitationRebateStatusPending  = "pending"
	InvitationRebateStatusSettled  = "settled"
	InvitationRebateStatusFrozen   = "frozen"
	InvitationRebateStatusReversed = "reversed"
)

type InvitationRebate struct {
	Id                   int     `json:"id"`
	InviterId            int     `json:"inviter_id" gorm:"index"`
	InviteeId            int     `json:"invitee_id" gorm:"index"`
	TopUpId              int     `json:"top_up_id" gorm:"uniqueIndex"`
	TradeNo              string  `json:"trade_no" gorm:"type:varchar(255);index"`
	TopUpMoney           float64 `json:"top_up_money"`
	PaymentMethod        string  `json:"payment_method" gorm:"type:varchar(50);index"`
	PaymentProvider      string  `json:"payment_provider" gorm:"type:varchar(50);index"`
	PaidAmountMinor      int64   `json:"paid_amount_minor"`
	PaidCurrency         string  `json:"paid_currency" gorm:"type:varchar(3);index"`
	PaidAmountVerified   bool    `json:"paid_amount_verified"`
	RebatePercentage     float64 `json:"rebate_percentage"`
	QuotaPerUnitSnapshot float64 `json:"quota_per_unit_snapshot"`
	RebateAmount         float64 `json:"rebate_amount"`
	RebateAmountMinor    int64   `json:"rebate_amount_minor"`
	RebateCurrency       string  `json:"rebate_currency" gorm:"type:varchar(3);index"`
	Cashable             bool    `json:"cashable"`
	RebateQuota          int     `json:"rebate_quota"`
	FreezeDays           int     `json:"freeze_days"`
	SettleAfter          int64   `json:"settle_after" gorm:"index"`
	RuleSnapshot         string  `json:"rule_snapshot" gorm:"type:text"`
	RiskStatus           string  `json:"risk_status" gorm:"type:varchar(32);index"`
	RefundTradeNo        string  `json:"refund_trade_no" gorm:"type:varchar(255);index"`
	ReversalQuota        int     `json:"reversal_quota"`
	ReversedAt           int64   `json:"reversed_at" gorm:"index"`
	Remark               string  `json:"remark" gorm:"type:text"`
	ReviewBy             int     `json:"review_by" gorm:"index"`
	Status               string  `json:"status" gorm:"type:varchar(32);index"`
	CreatedAt            int64   `json:"created_at" gorm:"index"`
	SettledAt            int64   `json:"settled_at" gorm:"index"`
}

type UserInvitationRebateRecord struct {
	InviteeName       string  `json:"invitee_name"`
	RebatePercentage  float64 `json:"rebate_percentage"`
	RebateAmount      float64 `json:"rebate_amount"`
	RebateAmountMinor int64   `json:"rebate_amount_minor"`
	RebateCurrency    string  `json:"rebate_currency"`
	Cashable          bool    `json:"cashable"`
	RebateQuota       int     `json:"rebate_quota"`
	SettleAfter       int64   `json:"settle_after"`
	ReversalQuota     int     `json:"reversal_quota"`
	ReversedAt        int64   `json:"reversed_at"`
	Status            string  `json:"status"`
	CreatedAt         int64   `json:"created_at"`
	SettledAt         int64   `json:"settled_at"`
}

func SettleInvitationRebateTx(tx *gorm.DB, topUp *TopUp) (*InvitationRebate, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if topUp == nil {
		return nil, errors.New("topup is required")
	}
	if topUp.Id == 0 || topUp.Purpose != TopUpPurposeAPIBalance || topUp.Status != common.TopUpStatusSuccess {
		return nil, nil
	}
	if topUp.RefundStatus != "" {
		return nil, nil
	}
	percentage := operation_setting.GetInviteRebatePercentage()
	if !operation_setting.IsPaymentComplianceConfirmed() || percentage <= 0 || percentage > 100 || math.IsNaN(percentage) || math.IsInf(percentage, 0) {
		return nil, nil
	}
	calculation, err := calculateInvitationRebate(topUp, percentage)
	if err != nil {
		return nil, err
	}
	if calculation == nil {
		return nil, nil
	}

	var existing InvitationRebate
	err = tx.Where("top_up_id = ?", topUp.Id).First(&existing).Error
	if err == nil {
		if existing.Status == InvitationRebateStatusPending && existing.SettleAfter <= common.GetTimestamp() {
			if err = settleInvitationRebateTx(tx, &existing, common.GetTimestamp()); err != nil {
				return nil, err
			}
		}
		return &existing, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var invitee User
	if err = tx.Select("id", "inviter_id").Where("id = ?", topUp.UserId).First(&invitee).Error; err != nil {
		return nil, err
	}
	if invitee.InviterId == 0 {
		return nil, nil
	}

	createdAt := topUp.CompleteTime
	if createdAt == 0 {
		createdAt = common.GetTimestamp()
	}
	now := common.GetTimestamp()
	freezeDays := operation_setting.GetGrowthSetting().RebateFreezeDays
	if freezeDays < 0 {
		freezeDays = 0
	}
	settleAfter := createdAt + int64(freezeDays)*24*60*60
	status := InvitationRebateStatusPending
	settledAt := int64(0)
	if freezeDays == 0 || settleAfter <= now {
		status = InvitationRebateStatusSettled
		settledAt = now
		if freezeDays == 0 {
			settledAt = createdAt
		}
	}

	rebate := &InvitationRebate{
		InviterId:            invitee.InviterId,
		InviteeId:            invitee.Id,
		TopUpId:              topUp.Id,
		TradeNo:              topUp.TradeNo,
		TopUpMoney:           calculation.paidAmountMajor.InexactFloat64(),
		PaymentMethod:        topUp.PaymentMethod,
		PaymentProvider:      topUp.PaymentProvider,
		PaidAmountMinor:      calculation.paidAmountMinor,
		PaidCurrency:         calculation.paidCurrency,
		PaidAmountVerified:   calculation.paidAmountVerified,
		RebatePercentage:     percentage,
		QuotaPerUnitSnapshot: common.QuotaPerUnit,
		RebateAmount:         calculation.rebateAmount.InexactFloat64(),
		RebateAmountMinor:    calculation.rebateAmountMinor,
		RebateCurrency:       calculation.rebateCurrency,
		Cashable:             calculation.cashable,
		RebateQuota:          calculation.rebateQuota,
		FreezeDays:           freezeDays,
		SettleAfter:          settleAfter,
		RuleSnapshot:         buildInvitationRebateRuleSnapshot(percentage, common.QuotaPerUnit, freezeDays, calculation),
		Status:               status,
		CreatedAt:            createdAt,
		SettledAt:            settledAt,
	}
	if !calculation.paidAmountVerified {
		rebate.RiskStatus = "unverified_payment"
		rebate.Remark = "cash commission requires a verified provider payment snapshot"
	} else if !calculation.cashable {
		rebate.RiskStatus = "unsupported_currency"
		rebate.Remark = fmt.Sprintf("cash commission is not supported for %s payments", calculation.paidCurrency)
	}
	if err = tx.Create(rebate).Error; err != nil {
		return nil, err
	}

	if err = createPromotionCommissionLedgerForRebateTx(tx, rebate, topUp); err != nil {
		return nil, err
	}

	return rebate, nil
}

type invitationRebateCalculation struct {
	paidAmountMajor    decimal.Decimal
	paidAmountMinor    int64
	paidCurrency       string
	paidAmountVerified bool
	rebateAmount       decimal.Decimal
	rebateAmountMinor  int64
	rebateCurrency     string
	rebateQuota        int
	cashable           bool
	usdToCny           float64
}

func calculateInvitationRebate(topUp *TopUp, percentage float64) (*invitationRebateCalculation, error) {
	if topUp == nil || percentage <= 0 || percentage > 100 || math.IsNaN(percentage) || math.IsInf(percentage, 0) {
		return nil, nil
	}

	payment, verified := topUp.verifiedPayment()
	if !verified {
		if topUp.Money <= 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) {
			return nil, nil
		}
		payment = VerifiedPayment{Currency: "CNY"}
	}
	paidAmount := payment.majorAmount()
	if !verified {
		paidAmount = decimal.NewFromFloat(topUp.Money)
	}
	percentageDecimal := decimal.NewFromFloat(percentage).Div(decimal.NewFromInt(100))
	sourceRebate := paidAmount.Mul(percentageDecimal)
	if !sourceRebate.IsPositive() {
		return nil, nil
	}

	calculation := &invitationRebateCalculation{
		paidAmountMajor:    paidAmount,
		paidAmountMinor:    payment.AmountMinor,
		paidCurrency:       payment.Currency,
		paidAmountVerified: verified,
		rebateCurrency:     payment.Currency,
		rebateAmount:       sourceRebate,
		cashable:           false,
	}
	if !verified {
		calculation.paidCurrency = "CNY"
	}

	sourceRebateMinor := sourceRebate.Shift(int32(paymentCurrencyExponent(calculation.paidCurrency))).Round(0)
	if sourceRebateMinor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, ErrInvalidVerifiedPayment
	}
	calculation.rebateAmountMinor = sourceRebateMinor.IntPart()

	usdToCny := operation_setting.USDExchangeRate
	if usdToCny <= 0 || math.IsNaN(usdToCny) || math.IsInf(usdToCny, 0) {
		return calculation, nil
	}
	calculation.usdToCny = usdToCny

	var rebateUSD decimal.Decimal
	switch calculation.paidCurrency {
	case "CNY":
		rebateUSD = sourceRebate.Div(decimal.NewFromFloat(usdToCny))
		calculation.rebateCurrency = "CNY"
		calculation.cashable = true
	case "USD":
		rebateUSD = sourceRebate
		calculation.rebateAmount = sourceRebate.Mul(decimal.NewFromFloat(usdToCny))
		amountMinor := calculation.rebateAmount.Shift(2).Round(0)
		if amountMinor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
			return nil, ErrInvalidVerifiedPayment
		}
		calculation.rebateAmountMinor = amountMinor.IntPart()
		calculation.rebateCurrency = "CNY"
		calculation.cashable = true
	default:
		return calculation, nil
	}

	rebateQuota, err := common.QuotaFromDecimalStrict(rebateUSD.Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if err != nil {
		return nil, err
	}
	if rebateQuota <= 0 {
		return nil, nil
	}
	calculation.rebateQuota = rebateQuota
	if !verified {
		calculation.cashable = false
	}
	return calculation, nil
}

func buildInvitationRebateRuleSnapshot(rebatePercentage float64, quotaPerUnit float64, freezeDays int, calculation *invitationRebateCalculation) string {
	snapshot := map[string]interface{}{
		"invite_rebate_percentage": rebatePercentage,
		"quota_per_unit":           quotaPerUnit,
		"rebate_freeze_days":       freezeDays,
	}
	if calculation != nil {
		snapshot["paid_amount_verified"] = calculation.paidAmountVerified
		snapshot["paid_currency"] = calculation.paidCurrency
		snapshot["rebate_currency"] = calculation.rebateCurrency
		snapshot["cashable"] = calculation.cashable
		if calculation.usdToCny > 0 {
			snapshot["usd_exchange_rate"] = calculation.usdToCny
		}
	}
	data, err := common.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(data)
}

func createPromotionCommissionLedgerForRebateTx(tx *gorm.DB, rebate *InvitationRebate, topUp *TopUp) error {
	if rebate == nil || topUp == nil {
		return nil
	}
	if !rebate.Cashable {
		common.SysLog(fmt.Sprintf("promotion commission skipped for non-cashable rebate: top_up_id=%d risk_status=%s currency=%s", topUp.Id, rebate.RiskStatus, rebate.PaidCurrency))
		return nil
	}
	grossAmountCents := rebate.RebateAmountMinor
	if grossAmountCents <= 0 {
		return nil
	}
	status := PromotionCommissionStatusPending
	settledAt := int64(0)
	if rebate.Status == InvitationRebateStatusSettled {
		status = PromotionCommissionStatusSettled
		settledAt = rebate.SettledAt
	}
	paymentSnapshot, err := common.Marshal(map[string]interface{}{
		"top_up_id":            topUp.Id,
		"trade_no":             topUp.TradeNo,
		"paid_amount_minor":    rebate.PaidAmountMinor,
		"paid_currency":        rebate.PaidCurrency,
		"paid_amount_verified": rebate.PaidAmountVerified,
		"payment_method":       topUp.PaymentMethod,
		"payment_provider":     topUp.PaymentProvider,
	})
	if err != nil {
		return err
	}
	ledger := &PromotionCommissionLedger{
		UserId:           rebate.InviterId,
		InviteeId:        rebate.InviteeId,
		SourceType:       PromotionCommissionSourceTopUpRebate,
		SourceId:         rebate.Id,
		SourceTradeNo:    rebate.TradeNo,
		Cashable:         rebate.Cashable,
		Currency:         rebate.RebateCurrency,
		GrossAmountCents: grossAmountCents,
		NetAmountCents:   grossAmountCents,
		QuotaEquivalent:  rebate.RebateQuota,
		Status:           status,
		AvailableAt:      rebate.SettleAfter,
		SettledAt:        settledAt,
		RuleSnapshot:     rebate.RuleSnapshot,
		PaymentSnapshot:  string(paymentSnapshot),
		Remark:           rebate.Remark,
		CreatedAt:        rebate.CreatedAt,
	}
	return CreatePromotionCommissionLedgerTx(tx, ledger)
}

func settleInvitationRebateTx(tx *gorm.DB, rebate *InvitationRebate, settledAt int64) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if rebate == nil || rebate.Id == 0 || rebate.Status != InvitationRebateStatusPending {
		return nil
	}
	if settledAt <= 0 {
		settledAt = common.GetTimestamp()
	}
	res := tx.Model(&InvitationRebate{}).
		Where("id = ? AND status = ?", rebate.Id, InvitationRebateStatusPending).
		Updates(map[string]interface{}{
			"status":     InvitationRebateStatusSettled,
			"settled_at": settledAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}
	if err := SettlePromotionCommissionLedgerTx(tx, PromotionCommissionSourceTopUpRebate, rebate.Id, settledAt); err != nil {
		return err
	}
	rebate.Status = InvitationRebateStatusSettled
	rebate.SettledAt = settledAt
	return nil
}

func SettleDueInvitationRebatesForInviter(inviterId int) error {
	if inviterId <= 0 {
		return nil
	}
	now := common.GetTimestamp()
	for {
		var rebates []InvitationRebate
		if err := DB.Where("inviter_id = ? AND status = ? AND settle_after <= ?", inviterId, InvitationRebateStatusPending, now).
			Order("id ASC").
			Limit(200).
			Find(&rebates).Error; err != nil {
			return err
		}
		if len(rebates) == 0 {
			return nil
		}
		for _, rebate := range rebates {
			if err := DB.Transaction(func(tx *gorm.DB) error {
				lockedRebate := &InvitationRebate{}
				if err := lockForUpdate(tx).Where("id = ?", rebate.Id).First(lockedRebate).Error; err != nil {
					return err
				}
				return settleInvitationRebateTx(tx, lockedRebate, now)
			}); err != nil {
				return err
			}
		}
	}
}

func ReverseInvitationRebateByTopUpTx(tx *gorm.DB, topUpId int, refundTradeNo string, remark string) (*InvitationRebate, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if topUpId <= 0 {
		return nil, nil
	}
	rebate := &InvitationRebate{}
	if err := lockForUpdate(tx).Where("top_up_id = ?", topUpId).First(rebate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if rebate.Status == InvitationRebateStatusReversed {
		return rebate, nil
	}

	now := common.GetTimestamp()
	reversalQuota := rebate.RebateQuota

	result := tx.Model(&InvitationRebate{}).
		Where("id = ? AND status = ?", rebate.Id, rebate.Status).
		Updates(map[string]interface{}{
			"status":          InvitationRebateStatusReversed,
			"refund_trade_no": refundTradeNo,
			"reversal_quota":  reversalQuota,
			"reversed_at":     now,
			"remark":          remark,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, errors.New("invitation rebate status changed, please retry")
	}
	if err := ReversePromotionCommissionLedgerTx(tx, PromotionCommissionSourceTopUpRebate, rebate.Id, refundTradeNo, remark); err != nil {
		return nil, err
	}
	rebate.Status = InvitationRebateStatusReversed
	rebate.RefundTradeNo = refundTradeNo
	rebate.ReversalQuota = reversalQuota
	rebate.ReversedAt = now
	rebate.Remark = remark
	return rebate, nil
}

func ReverseInvitationRebateByTopUp(topUpId int, refundTradeNo string, remark string) (*InvitationRebate, error) {
	var rebate *InvitationRebate
	err := DB.Transaction(func(tx *gorm.DB) error {
		reversedRebate, err := ReverseInvitationRebateByTopUpTx(tx, topUpId, refundTradeNo, remark)
		if err != nil {
			return err
		}
		rebate = reversedRebate
		return nil
	})
	if err == nil && rebate != nil {
		_ = InvalidateUserCache(rebate.InviterId)
	}
	return rebate, err
}

func ReverseInvitationRebateByTradeNo(tradeNo string, refundTradeNo string, remark string) (*InvitationRebate, error) {
	if tradeNo == "" {
		return nil, nil
	}
	var rebate *InvitationRebate
	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(topUp).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		reversedRebate, err := ReverseInvitationRebateByTopUpTx(tx, topUp.Id, refundTradeNo, remark)
		if err != nil {
			return err
		}
		rebate = reversedRebate
		return nil
	})
	if err == nil && rebate != nil {
		_ = InvalidateUserCache(rebate.InviterId)
	}
	return rebate, err
}

func RecordInvitationRebateLog(rebate *InvitationRebate) {
	if rebate == nil {
		return
	}

	content := fmt.Sprintf(
		"Invitation rebate %s: %s from user #%d top-up %.2f",
		rebate.Status,
		logger.LogQuota(rebate.RebateQuota),
		rebate.InviteeId,
		rebate.TopUpMoney,
	)
	RecordLog(rebate.InviterId, LogTypeSystem, content)
}

func GetUserInvitationRebateRecords(inviterId int, pageInfo *common.PageInfo) (
	records []*UserInvitationRebateRecord,
	total int64,
	err error,
) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Model(&InvitationRebate{}).Where("inviter_id = ?", inviterId).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Table("invitation_rebates").
		Select("invitation_rebates.rebate_percentage, invitation_rebates.rebate_amount, invitation_rebates.rebate_amount_minor, invitation_rebates.rebate_currency, invitation_rebates.cashable, invitation_rebates.rebate_quota, invitation_rebates.settle_after, invitation_rebates.reversal_quota, invitation_rebates.reversed_at, invitation_rebates.status, invitation_rebates.created_at, invitation_rebates.settled_at, COALESCE(NULLIF(users.display_name, ''), users.username) AS invitee_name").
		Joins("LEFT JOIN users ON users.id = invitation_rebates.invitee_id").
		Where("invitation_rebates.inviter_id = ?", inviterId).
		Order("invitation_rebates.id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&records).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

func SyncInvitationRebatesForInviter(inviterId int) error {
	if inviterId <= 0 {
		return nil
	}
	if err := SettleDueInvitationRebatesForInviter(inviterId); err != nil {
		return err
	}
	if operation_setting.GetInviteRebatePercentage() <= 0 || !operation_setting.IsPaymentComplianceConfirmed() {
		return nil
	}

	for {
		var topUps []TopUp
		err := DB.Table("top_ups").
			Select("top_ups.id, top_ups.user_id, top_ups.purpose, top_ups.amount, top_ups.money, top_ups.paid_amount_minor, top_ups.paid_currency, top_ups.paid_amount_verified, top_ups.trade_no, top_ups.payment_method, top_ups.payment_provider, top_ups.create_time, top_ups.complete_time, top_ups.status").
			Joins("INNER JOIN users ON users.id = top_ups.user_id").
			Joins("LEFT JOIN invitation_rebates ON invitation_rebates.top_up_id = top_ups.id").
			Joins("LEFT JOIN subscription_orders ON subscription_orders.trade_no = top_ups.trade_no").
			Where("users.inviter_id = ? AND top_ups.purpose = ? AND top_ups.status = ? AND invitation_rebates.id IS NULL AND subscription_orders.id IS NULL", inviterId, TopUpPurposeAPIBalance, common.TopUpStatusSuccess).
			Where("top_ups.refund_status IS NULL OR top_ups.refund_status = ?", "").
			Order("top_ups.id ASC").
			Limit(200).
			Scan(&topUps).Error
		if err != nil {
			return err
		}
		if len(topUps) == 0 {
			return nil
		}

		for _, topUp := range topUps {
			fencedUserIds := newRefundHoldFenceScope()
			if err = preparePromotionRefundTopUpAccounting(&topUp, fencedUserIds); err != nil {
				return errors.Join(err, reconcilePromotionRefundHoldFences(fencedUserIds))
			}
			err = DB.Transaction(func(tx *gorm.DB) error {
				lockedTopUp := &TopUp{}
				if err := lockForUpdate(tx).Where("id = ?", topUp.Id).First(lockedTopUp).Error; err != nil {
					return err
				}
				_, err := SettleInvitationRebateTx(tx, lockedTopUp)
				if err != nil {
					return err
				}
				return reconcilePromotionRefundForTopUpTx(tx, lockedTopUp, fencedUserIds, true)
			})
			reconcileErr := reconcilePromotionRefundHoldFences(fencedUserIds)
			if err != nil || reconcileErr != nil {
				return errors.Join(err, reconcileErr)
			}
		}
	}
}
