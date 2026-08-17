package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	GrowthRewardItemTypeAuto       = "auto"
	GrowthRewardItemTypeManual     = "manual"
	GrowthRewardItemTypeSemiAuto   = "semi_auto"
	GrowthRewardItemTypeInvitation = "invitation"
)

const (
	GrowthRewardItemDailyCheckin       = "daily_checkin"
	GrowthRewardItemCreateFirstAPIKey  = "create_first_api_key"
	GrowthRewardItemFirstAPIRequest    = "first_api_request"
	GrowthRewardItemFirstTopUp         = "first_topup"
	GrowthRewardItemThreeDayUsage      = "three_day_usage"
	GrowthRewardItemMonthlySpendTarget = "monthly_spend_target"
	GrowthRewardItemJoinCommunity      = "join_community"
	GrowthRewardItemContentPublish     = "content_publish"
	GrowthRewardItemBacklinkSubmission = "backlink_submission"
)

type GrowthRewardItem struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	Code          string `json:"code" gorm:"type:varchar(64);uniqueIndex;not null"`
	Title         string `json:"title" gorm:"type:varchar(128);not null"`
	Description   string `json:"description" gorm:"type:text"`
	Introduction  string `json:"introduction,omitempty" gorm:"type:text"`
	RewardQuota   int    `json:"reward_quota" gorm:"default:0"`
	ItemType      string `json:"item_type" gorm:"type:varchar(32);index;not null"`
	ActionURL     string `json:"action_url,omitempty" gorm:"type:text"`
	ClaimPassword string `json:"-" gorm:"type:varchar(128);default:''"`
	Enabled       bool   `json:"enabled" gorm:"default:true;index"`
	OncePerUser   bool   `json:"once_per_user" gorm:"default:true"`
	DailyLimit    int    `json:"daily_limit" gorm:"default:0"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;index"`
}

func (GrowthRewardItem) TableName() string {
	return "growth_reward_items"
}

func (item *GrowthRewardItem) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return nil
}

func (item *GrowthRewardItem) BeforeUpdate(_ *gorm.DB) error {
	item.UpdatedAt = time.Now().Unix()
	return nil
}

func GetDefaultGrowthRewardItems() []*GrowthRewardItem {
	return []*GrowthRewardItem{
		{
			Code:        GrowthRewardItemDailyCheckin,
			Title:       "Daily check-in",
			Description: "Check in once per day to keep your account active.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     true,
			OncePerUser: false,
			DailyLimit:  1,
		},
		{
			Code:        GrowthRewardItemCreateFirstAPIKey,
			Title:       "Create your first API key",
			Description: "Create an API key and prepare your first integration.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     true,
			OncePerUser: true,
		},
		{
			Code:        GrowthRewardItemFirstAPIRequest,
			Title:       "Complete your first API request",
			Description: "Send one successful API request through the gateway.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     true,
			OncePerUser: true,
		},
		{
			Code:        GrowthRewardItemFirstTopUp,
			Title:       "Complete your first top-up",
			Description: "Add funds for the first time.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     true,
			OncePerUser: true,
		},
		{
			Code:        GrowthRewardItemThreeDayUsage,
			Title:       "Use the API for 3 consecutive days",
			Description: "Send requests on 3 consecutive days.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     true,
			OncePerUser: true,
		},
		{
			Code:        GrowthRewardItemMonthlySpendTarget,
			Title:       "Reach this month's spend target",
			Description: "Reach the configured monthly consumption target.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     true,
			OncePerUser: false,
			DailyLimit:  1,
		},
		{
			Code:        GrowthRewardItemJoinCommunity,
			Title:       "Join the community",
			Description: "Join the community and enter the task password to claim the reward.",
			ItemType:    GrowthRewardItemTypeAuto,
			Enabled:     false,
			OncePerUser: true,
		},
		{
			Code:        GrowthRewardItemContentPublish,
			Title:       "Publish an article, video, or tutorial",
			Description: "Share content that helps others use the API service.",
			Introduction: "Best for articles, videos, reviews, and tutorials published on public platforms.\n" +
				"Topics can include setup guides, API usage, model reviews, free quota collection, and integration examples.\n" +
				"Suitable platforms include blogs, CSDN, Zhihu, video sites, documentation sites, and community posts.",
			ItemType:    GrowthRewardItemTypeManual,
			Enabled:     true,
			OncePerUser: false,
		},
		{
			Code:        GrowthRewardItemBacklinkSubmission,
			Title:       "Submit a website backlink or directory listing",
			Description: "Submit an approved backlink or directory listing.",
			Introduction: "Add a public backlink to this site from your website, directory, navigation page, docs, or resource list.\n" +
				"Reward quality depends on page relevance, visibility, and long-term availability.\n" +
				"Higher-quality pages with stable access and relevant surrounding content are easier to approve.",
			ItemType:    GrowthRewardItemTypeManual,
			Enabled:     true,
			OncePerUser: false,
		},
	}
}

func EnsureDefaultGrowthRewardItems() error {
	for _, item := range GetDefaultGrowthRewardItems() {
		row := *item
		if err := DB.Where("code = ?", item.Code).FirstOrCreate(&row).Error; err != nil {
			return err
		}
	}
	if err := DB.Model(&GrowthRewardItem{}).
		Where("code = ? AND item_type <> ?", GrowthRewardItemJoinCommunity, GrowthRewardItemTypeAuto).
		Update("item_type", GrowthRewardItemTypeAuto).Error; err != nil {
		return err
	}
	return DB.Model(&GrowthRewardItem{}).
		Where("code = ? AND enabled = ? AND (claim_password IS NULL OR claim_password = ?)", GrowthRewardItemJoinCommunity, true, "").
		Update("enabled", false).Error
}

func migrateGrowthRewardTables() error {
	migrator := DB.Migrator()
	if migrator.HasTable("growth_tasks") && !migrator.HasTable(&GrowthRewardItem{}) {
		if err := migrator.RenameTable("growth_tasks", "growth_reward_items"); err != nil {
			return err
		}
	}
	if migrator.HasTable(&GrowthRewardItem{}) && migrator.HasColumn(&GrowthRewardItem{}, "task_type") && !migrator.HasColumn(&GrowthRewardItem{}, "item_type") {
		if err := migrator.RenameColumn(&GrowthRewardItem{}, "task_type", "item_type"); err != nil {
			return err
		}
	}
	if migrator.HasTable(&GrowthRewardItem{}) && !migrator.HasColumn(&GrowthRewardItem{}, "introduction") {
		if err := migrator.AddColumn(&GrowthRewardItem{}, "Introduction"); err != nil {
			return err
		}
		if err := backfillDefaultGrowthRewardItemIntroductions(); err != nil {
			return err
		}
	}
	if migrator.HasTable(&GrowthReward{}) && migrator.HasColumn(&GrowthReward{}, "task_code") && !migrator.HasColumn(&GrowthReward{}, "item_code") {
		if err := migrator.RenameColumn(&GrowthReward{}, "task_code", "item_code"); err != nil {
			return err
		}
	}
	if err := dropLegacyGrowthCodeColumn(&GrowthReward{}, "growth_rewards"); err != nil {
		return err
	}
	if migrator.HasTable(&GrowthSubmission{}) && migrator.HasColumn(&GrowthSubmission{}, "task_code") && !migrator.HasColumn(&GrowthSubmission{}, "item_code") {
		if err := migrator.RenameColumn(&GrowthSubmission{}, "task_code", "item_code"); err != nil {
			return err
		}
	}
	if err := dropLegacyGrowthCodeColumn(&GrowthSubmission{}, "growth_submissions"); err != nil {
		return err
	}
	return nil
}

func backfillDefaultGrowthRewardItemIntroductions() error {
	for _, item := range GetDefaultGrowthRewardItems() {
		if item.Introduction == "" {
			continue
		}
		if err := DB.Model(&GrowthRewardItem{}).
			Where("code = ?", item.Code).
			Update("introduction", item.Introduction).Error; err != nil {
			return err
		}
	}
	return nil
}

func dropLegacyGrowthCodeColumn(modelValue interface{}, tableName string) error {
	migrator := DB.Migrator()
	if !migrator.HasTable(modelValue) ||
		!migrator.HasColumn(modelValue, "task_code") ||
		!migrator.HasColumn(modelValue, "item_code") {
		return nil
	}
	if err := DB.Exec(
		"UPDATE " + tableName + " SET item_code = task_code WHERE (item_code IS NULL OR item_code = '') AND task_code IS NOT NULL AND task_code <> ''",
	).Error; err != nil {
		return err
	}
	return migrator.DropColumn(modelValue, "task_code")
}
