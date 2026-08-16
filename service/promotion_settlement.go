package service

import (
	"errors"

	"github.com/QuantumNous/new-api/model"
)

// SettleDuePromotionCommissions is the explicit write path used before the
// UI reads balances. Keeping settlement behind POST leaves summary GETs free
// of hidden database mutations while still making newly matured funds usable.
func SettleDuePromotionCommissions(userId int) (*GrowthSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user")
	}
	if err := model.SettleDueInvitationRebatesForInviter(userId); err != nil {
		return nil, err
	}
	return GetGrowthSummary(userId)
}
