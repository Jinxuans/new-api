package service

import (
	"errors"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	promotionPayoutMethodMaxRunes     = 32
	promotionPayoutAccountMaxRunes    = 200
	promotionWithdrawalRemarkMaxRunes = 500
	growthSubmissionPlatformMaxRunes  = 64
	growthSubmissionURLMaxRunes       = 2048
	growthSubmissionRemarkMaxRunes    = 500
)

func normalizePromotionWithdrawalRequest(req PromotionWithdrawalRequest) (PromotionWithdrawalRequest, error) {
	req.PayoutMethod = strings.TrimSpace(req.PayoutMethod)
	req.PayoutAccount = strings.TrimSpace(req.PayoutAccount)
	req.Remark = strings.TrimSpace(req.Remark)
	if req.PayoutMethod == "" || req.PayoutAccount == "" {
		return PromotionWithdrawalRequest{}, errors.New("payout method and account are required")
	}
	if utf8.RuneCountInString(req.PayoutMethod) > promotionPayoutMethodMaxRunes {
		return PromotionWithdrawalRequest{}, errors.New("payout method is too long")
	}
	if utf8.RuneCountInString(req.PayoutAccount) > promotionPayoutAccountMaxRunes {
		return PromotionWithdrawalRequest{}, errors.New("payout account is too long")
	}
	if utf8.RuneCountInString(req.Remark) > promotionWithdrawalRemarkMaxRunes {
		return PromotionWithdrawalRequest{}, errors.New("withdrawal remark is too long")
	}
	return req, nil
}

func normalizeGrowthSubmissionRequest(req GrowthSubmissionRequest) (GrowthSubmissionRequest, error) {
	req.ItemCode = strings.TrimSpace(req.ItemCode)
	req.LegacyTaskCode = strings.TrimSpace(req.LegacyTaskCode)
	req.Platform = strings.TrimSpace(req.Platform)
	req.Url = strings.TrimSpace(req.Url)
	req.Remark = strings.TrimSpace(req.Remark)
	if utf8.RuneCountInString(req.Platform) > growthSubmissionPlatformMaxRunes {
		return GrowthSubmissionRequest{}, errors.New("submission platform is too long")
	}
	if req.Url == "" || utf8.RuneCountInString(req.Url) > growthSubmissionURLMaxRunes {
		return GrowthSubmissionRequest{}, errors.New("invalid submission URL")
	}
	parsedURL, err := url.ParseRequestURI(req.Url)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return GrowthSubmissionRequest{}, errors.New("invalid submission URL")
	}
	if utf8.RuneCountInString(req.Remark) > growthSubmissionRemarkMaxRunes {
		return GrowthSubmissionRequest{}, errors.New("submission remark is too long")
	}
	return req, nil
}

func addPromotionCommissionLedgerTotals(amountCents int64, quota int64, ledger *model.PromotionCommissionLedger) (int64, int64, error) {
	if ledger == nil || ledger.NetAmountCents <= 0 || ledger.QuotaEquivalent <= 0 {
		return 0, 0, errors.New("invalid promotion commission ledger amount")
	}
	if amountCents > math.MaxInt64-ledger.NetAmountCents {
		return 0, 0, errors.New("promotion commission amount overflow")
	}
	ledgerQuota := int64(ledger.QuotaEquivalent)
	if quota > int64(common.MaxQuota-1)-ledgerQuota {
		return 0, 0, model.ErrTopUpQuotaLimitExceeded
	}
	return amountCents + ledger.NetAmountCents, quota + ledgerQuota, nil
}
