package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// UserGrowthRewardItem is the customer-facing reward definition. Database
// identity, claim rules, and administrative timestamps stay on the admin API.
type UserGrowthRewardItem struct {
	Code                 string `json:"code"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	Introduction         string `json:"introduction,omitempty"`
	RewardQuota          int    `json:"reward_quota"`
	RewardQuotaMin       int    `json:"reward_quota_min"`
	RewardQuotaMax       int    `json:"reward_quota_max"`
	ProgressCurrentQuota int64  `json:"progress_current_quota,omitempty"`
	ProgressTargetQuota  int64  `json:"progress_target_quota,omitempty"`
	ItemType             string `json:"item_type"`
	ActionURL            string `json:"action_url,omitempty"`
	Status               string `json:"status"`
	Claimable            bool   `json:"claimable"`
	Reason               string `json:"reason,omitempty"`
}

func ToUserGrowthRewardItem(item *GrowthRewardItemStatus) *UserGrowthRewardItem {
	if item == nil || item.GrowthRewardItem == nil {
		return nil
	}
	return &UserGrowthRewardItem{
		Code:                 item.Code,
		Title:                item.Title,
		Description:          item.Description,
		Introduction:         item.Introduction,
		RewardQuota:          item.RewardQuota,
		RewardQuotaMin:       item.RewardQuotaMin,
		RewardQuotaMax:       item.RewardQuotaMax,
		ProgressCurrentQuota: item.ProgressCurrentQuota,
		ProgressTargetQuota:  item.ProgressTargetQuota,
		ItemType:             item.ItemType,
		ActionURL:            item.ActionURL,
		Status:               item.Status,
		Claimable:            item.Claimable,
		Reason:               item.Reason,
	}
}

func ToUserGrowthRewardItems(items []*GrowthRewardItemStatus) []*UserGrowthRewardItem {
	records := make([]*UserGrowthRewardItem, 0, len(items))
	for _, item := range items {
		if record := ToUserGrowthRewardItem(item); record != nil {
			records = append(records, record)
		}
	}
	return records
}

// UserGrowthReward exposes the lifecycle and amount of a customer's reward,
// without database keys, source rows, or internal remarks.
type UserGrowthReward struct {
	ItemCode    string `json:"item_code"`
	RewardQuota int    `json:"reward_quota"`
	Status      string `json:"status"`
	AvailableAt int64  `json:"available_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	SettledAt   int64  `json:"settled_at,omitempty"`
}

func ToUserGrowthReward(reward *model.GrowthReward) *UserGrowthReward {
	if reward == nil {
		return nil
	}
	return &UserGrowthReward{
		ItemCode:    reward.ItemCode,
		RewardQuota: reward.RewardQuota,
		Status:      reward.Status,
		AvailableAt: reward.AvailableAt,
		CreatedAt:   reward.CreatedAt,
		SettledAt:   reward.SettledAt,
	}
}

func ListUserGrowthRewards(userId int, pageInfo *common.PageInfo) ([]*UserGrowthReward, int64, error) {
	rewards, total, err := model.ListGrowthRewards(userId, pageInfo)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*UserGrowthReward, 0, len(rewards))
	for _, reward := range rewards {
		records = append(records, ToUserGrowthReward(reward))
	}
	return records, total, nil
}

// UserGrowthSubmission keeps the proof and note supplied by the customer plus
// the user-facing review outcome. Reviewer identity and the database key are
// intentionally admin-only.
type UserGrowthSubmission struct {
	ItemCode   string `json:"item_code"`
	Platform   string `json:"platform"`
	URL        string `json:"url"`
	Remark     string `json:"remark"`
	Status     string `json:"status"`
	ReviewNote string `json:"review_note,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	ReviewedAt int64  `json:"reviewed_at,omitempty"`
}

func ToUserGrowthSubmission(submission *model.GrowthSubmission) *UserGrowthSubmission {
	if submission == nil {
		return nil
	}
	return &UserGrowthSubmission{
		ItemCode:   submission.ItemCode,
		Platform:   submission.Platform,
		URL:        submission.Url,
		Remark:     submission.Remark,
		Status:     submission.Status,
		ReviewNote: submission.ReviewNote,
		CreatedAt:  submission.CreatedAt,
		ReviewedAt: submission.ReviewedAt,
	}
}

func ListUserGrowthSubmissions(userId int, pageInfo *common.PageInfo) ([]*UserGrowthSubmission, int64, error) {
	submissions, total, err := model.ListGrowthSubmissions(userId, pageInfo)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*UserGrowthSubmission, 0, len(submissions))
	for _, submission := range submissions {
		records = append(records, ToUserGrowthSubmission(submission))
	}
	return records, total, nil
}
