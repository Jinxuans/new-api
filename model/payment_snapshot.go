package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

var (
	ErrInvalidVerifiedPayment  = errors.New("invalid verified payment")
	ErrVerifiedPaymentMismatch = errors.New("verified payment does not match stored snapshot")
)

// VerifiedPayment is the amount and ISO currency confirmed by a signed
// provider callback. AmountMinor is expressed in the currency's smallest
// unit, so downstream commission calculations never depend on the mutable
// checkout estimate stored in TopUp.Money.
type VerifiedPayment struct {
	AmountMinor       int64
	Currency          string
	ProviderPaymentId string
}

func NewVerifiedPaymentFromMinor(amountMinor int64, currency string) (VerifiedPayment, error) {
	payment := VerifiedPayment{
		AmountMinor: amountMinor,
		Currency:    strings.ToUpper(strings.TrimSpace(currency)),
	}
	if payment.AmountMinor <= 0 || !isISOCurrencyCode(payment.Currency) {
		return VerifiedPayment{}, ErrInvalidVerifiedPayment
	}
	return payment, nil
}

// ParseVerifiedPayment converts an exact provider-supplied major-unit decimal
// into minor units. It rejects fractional minor units and int64 overflow.
func ParseVerifiedPayment(amount string, currency string) (VerifiedPayment, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !isISOCurrencyCode(currency) {
		return VerifiedPayment{}, ErrInvalidVerifiedPayment
	}
	value, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || !value.IsPositive() {
		return VerifiedPayment{}, ErrInvalidVerifiedPayment
	}
	scaled := value.Shift(int32(paymentCurrencyExponent(currency)))
	if !scaled.Equal(scaled.Truncate(0)) || scaled.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return VerifiedPayment{}, ErrInvalidVerifiedPayment
	}
	return NewVerifiedPaymentFromMinor(scaled.IntPart(), currency)
}

func (payment VerifiedPayment) majorAmount() decimal.Decimal {
	return decimal.NewFromInt(payment.AmountMinor).Shift(-int32(paymentCurrencyExponent(payment.Currency)))
}

func (topUp *TopUp) setVerifiedPayment(payment VerifiedPayment) error {
	validated, err := NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency)
	if err != nil {
		return err
	}
	topUp.PaidAmountMinor = validated.AmountMinor
	topUp.PaidCurrency = validated.Currency
	topUp.PaidAmountVerified = true
	topUp.ProviderPaymentId = strings.TrimSpace(payment.ProviderPaymentId)
	topUp.PaymentVerifiedAt = common.GetTimestamp()
	return nil
}

func (topUp *TopUp) verifyStoredPayment(payment VerifiedPayment) error {
	validated, err := NewVerifiedPaymentFromMinor(payment.AmountMinor, payment.Currency)
	if err != nil {
		return err
	}
	if !topUp.PaidAmountVerified {
		return nil
	}
	if topUp.PaidAmountMinor != validated.AmountMinor || topUp.PaidCurrency != validated.Currency {
		return ErrVerifiedPaymentMismatch
	}
	providerPaymentId := strings.TrimSpace(payment.ProviderPaymentId)
	if topUp.ProviderPaymentId != "" && providerPaymentId != "" && topUp.ProviderPaymentId != providerPaymentId {
		return ErrVerifiedPaymentMismatch
	}
	return nil
}

func (topUp *TopUp) verifiedPayment() (VerifiedPayment, bool) {
	if topUp == nil || !topUp.PaidAmountVerified {
		return VerifiedPayment{}, false
	}
	payment, err := NewVerifiedPaymentFromMinor(topUp.PaidAmountMinor, topUp.PaidCurrency)
	return payment, err == nil
}

func isISOCurrencyCode(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
}

func paymentCurrencyExponent(currency string) int {
	switch currency {
	case "BIF", "CLP", "DJF", "GNF", "JPY", "KMF", "KRW", "MGA", "PYG", "RWF", "UGX", "VND", "VUV", "XAF", "XOF", "XPF":
		return 0
	case "BHD", "JOD", "KWD", "OMR", "TND":
		return 3
	default:
		return 2
	}
}
