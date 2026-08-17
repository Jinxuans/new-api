package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateDBUpgradesLegacySubscriptionOrdersSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:subscription_order_migration_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)

	previousDB := DB
	previousLogDB := LOG_DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		initCol()
	})

	require.NoError(t, db.Exec("CREATE TABLE `subscription_orders` (`id` integer,`user_id` integer,`plan_id` integer,`money` real,`trade_no` varchar(255) UNIQUE,`payment_method` varchar(50),`payment_provider` varchar(50) DEFAULT \"\",`status` text,`create_time` integer,`complete_time` integer,`provider_payload` text,PRIMARY KEY (`id`))").Error)
	require.NoError(t, db.Exec(`INSERT INTO subscription_orders
		(id, user_id, plan_id, trade_no, status) VALUES (1, 1, 1, 'legacy-order', 'success')`).Error)

	require.NoError(t, migrateSubscriptionOrderUserSubscriptionLinkSQLite())
	require.NoError(t, db.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, migrateSubscriptionOrderUserSubscriptionLinkSQLite(), "the compatibility migration must be idempotent")
	assert.True(t, db.Migrator().HasColumn(&SubscriptionOrder{}, "user_subscription_id"))

	var legacyOrder SubscriptionOrder
	require.NoError(t, db.First(&legacyOrder, 1).Error)
	assert.Nil(t, legacyOrder.UserSubscriptionId)

	userSubscriptionID := 42
	require.NoError(t, db.Model(&SubscriptionOrder{}).
		Where("id = ?", legacyOrder.Id).
		Update("user_subscription_id", userSubscriptionID).Error)
	duplicate := SubscriptionOrder{
		Id:                 2,
		UserId:             2,
		PlanId:             1,
		TradeNo:            "duplicate-link",
		Status:             "success",
		UserSubscriptionId: &userSubscriptionID,
	}
	assert.Error(t, db.Create(&duplicate).Error, "the entitlement link must remain unique after migration")
}
