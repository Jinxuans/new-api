package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func assertSubscriptionOrderUserSubscriptionUniqueness(t *testing.T, tx *gorm.DB) {
	t.Helper()

	if tx.Dialector.Name() == "postgres" {
		constraints, err := inspectSubscriptionOrderUserSubscriptionConstraints(tx, "subscription_orders")
		require.NoError(t, err)
		assert.Empty(t, constraints, "no single-column unique constraint may remain on user_subscription_id")

		targetIndex, err := inspectSubscriptionOrderUserSubscriptionIndex(tx, "subscription_orders")
		require.NoError(t, err)
		assert.True(t, targetIndex.standaloneValid, "user_subscription_id must be covered by the standalone unique index")
	}

	var boundOrders int64
	require.NoError(t, tx.Model(&SubscriptionOrder{}).Where("user_subscription_id IS NOT NULL").Count(&boundOrders).Error)
	assert.EqualValues(t, 1, boundOrders)

	duplicateError := tx.Transaction(func(duplicateTx *gorm.DB) error {
		return duplicateTx.Create(&SubscriptionOrder{
			UserId:             3,
			PlanId:             1,
			TradeNo:            "duplicate-link",
			Status:             "success",
			UserSubscriptionId: &[]int{42}[0],
		}).Error
	})
	require.Error(t, duplicateError, "the entitlement link must stay unique")

	nullLinkError := tx.Transaction(func(nullTx *gorm.DB) error {
		return nullTx.Create(&SubscriptionOrder{
			UserId:  4,
			PlanId:  1,
			TradeNo: "pending-link",
			Status:  "pending",
		}).Error
	})
	require.NoError(t, nullLinkError, "nullable orders must stay allowed next to the unique link")
}

func testSubscriptionOrderUserSubscriptionUniquenessNonPostgreSQL(t *testing.T, db *gorm.DB) {
	t.Helper()

	tableName := fmt.Sprintf("subscription_order_uniqueness_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = db.Migrator().DropTable(tableName) })

	tableDB := db.Table(tableName)
	require.NoError(t, tableDB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, tableDB.Create(&SubscriptionOrder{
		UserId:             1,
		PlanId:             1,
		TradeNo:            "bound-order",
		Status:             "success",
		UserSubscriptionId: &[]int{42}[0],
	}).Error)

	for range 2 {
		require.NoError(t, migrateSubscriptionOrderUserSubscriptionUniqueness(db))
		require.NoError(t, tableDB.AutoMigrate(&SubscriptionOrder{}))
	}

	assertSubscriptionOrderUserSubscriptionUniqueness(t, tableDB)
}

func TestMigrateSubscriptionOrderUserSubscriptionUniquenessSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testSubscriptionOrderUserSubscriptionUniquenessNonPostgreSQL(t, db)
}

func TestMigrateSubscriptionOrderUserSubscriptionUniquenessMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	tableName := fmt.Sprintf("subscription_order_uniqueness_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = db.Migrator().DropTable(tableName) })

	tableDB := db.Table(tableName)
	require.NoError(t, tableDB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, tableDB.Create(&SubscriptionOrder{
		UserId:             1,
		PlanId:             1,
		TradeNo:            "bound-order",
		Status:             "success",
		UserSubscriptionId: &[]int{42}[0],
	}).Error)

	// Legacy state: the guarantee is a unique key with a foreign name and the
	// GORM-managed standalone index is absent. MySQL must survive this state
	// without the GORM-named constraint drop failing startup.
	expectedIndex := db.NamingStrategy.IndexName(tableName, subscriptionOrderUserSubscriptionColumn)
	require.NoError(t, tableDB.Migrator().DropIndex(&SubscriptionOrder{}, expectedIndex))
	require.NoError(t, tableDB.Exec(
		"ALTER TABLE ? ADD UNIQUE KEY ? (user_subscription_id)",
		clause.Table{Name: tableName},
		clause.Column{Name: "subscription_orders_user_subscription_id_key"},
	).Error)

	for range 2 {
		require.NoError(t, migrateSubscriptionOrderUserSubscriptionUniqueness(db))
		require.NoError(t, tableDB.AutoMigrate(&SubscriptionOrder{}))
	}

	assertSubscriptionOrderUserSubscriptionUniqueness(t, tableDB)
}

func TestMigrateSubscriptionOrderUserSubscriptionUniquenessPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	tests := []struct {
		name       string
		prepareOld func(t *testing.T, tx *gorm.DB)
	}{
		{name: "fresh"},
		{
			// The production failure: a single-column UNIQUE constraint whose
			// name GORM does not predict, without the standalone index.
			name: "postgres_default_named_constraint_without_target_index",
			prepareOld: func(t *testing.T, tx *gorm.DB) {
				t.Helper()
				require.NoError(t, tx.Migrator().DropIndex(&SubscriptionOrder{}, subscriptionOrderUserSubscriptionIndex))
				require.NoError(t, tx.Exec(
					"ALTER TABLE ? ADD CONSTRAINT ? UNIQUE (?)",
					clause.Table{Name: "subscription_orders"},
					clause.Column{Name: "subscription_orders_user_subscription_id_key"},
					clause.Column{Name: subscriptionOrderUserSubscriptionColumn},
				).Error)
			},
		},
		{
			name: "gorm_named_constraint",
			prepareOld: func(t *testing.T, tx *gorm.DB) {
				t.Helper()
				require.NoError(t, tx.Migrator().DropIndex(&SubscriptionOrder{}, subscriptionOrderUserSubscriptionIndex))
				require.NoError(t, tx.Exec(
					"ALTER TABLE ? ADD CONSTRAINT ? UNIQUE (?)",
					clause.Table{Name: "subscription_orders"},
					clause.Column{Name: "uni_subscription_orders_user_subscription_id"},
					clause.Column{Name: subscriptionOrderUserSubscriptionColumn},
				).Error)
			},
		},
		{
			name: "constraint_and_target_index",
			prepareOld: func(t *testing.T, tx *gorm.DB) {
				t.Helper()
				require.NoError(t, tx.Exec(
					"ALTER TABLE ? ADD CONSTRAINT ? UNIQUE (?)",
					clause.Table{Name: "subscription_orders"},
					clause.Column{Name: "subscription_orders_user_subscription_id_key"},
					clause.Column{Name: subscriptionOrderUserSubscriptionColumn},
				).Error)
			},
		},
		{
			name: "composite_constraint_is_preserved",
			prepareOld: func(t *testing.T, tx *gorm.DB) {
				t.Helper()
				require.NoError(t, tx.Exec(
					"ALTER TABLE ? ADD CONSTRAINT ? UNIQUE (?, ?)",
					clause.Table{Name: "subscription_orders"},
					clause.Column{Name: "keep_subscription_orders_user_subscription_plan"},
					clause.Column{Name: subscriptionOrderUserSubscriptionColumn},
					clause.Column{Name: "plan_id"},
				).Error)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			t.Cleanup(func() { _ = tx.Rollback().Error })

			schemaName := fmt.Sprintf("subscription_uniqueness_%d", time.Now().UnixNano())
			require.NoError(t, tx.Exec("CREATE SCHEMA ?", clause.Table{Name: schemaName}).Error)
			require.NoError(t, tx.Exec("SET LOCAL search_path TO ?", clause.Table{Name: schemaName}).Error)

			require.NoError(t, tx.AutoMigrate(&SubscriptionOrder{}), "fresh AutoMigrate must succeed")
			require.NoError(t, tx.Create(&SubscriptionOrder{
				UserId:             1,
				PlanId:             1,
				TradeNo:            "bound-order",
				Status:             "success",
				UserSubscriptionId: &[]int{42}[0],
			}).Error)
			if test.prepareOld != nil {
				test.prepareOld(t, tx)
			}

			for range 2 {
				require.NoError(t, migrateSubscriptionOrderUserSubscriptionUniqueness(tx))
				require.NoError(t, tx.AutoMigrate(&SubscriptionOrder{}))
			}

			assertSubscriptionOrderUserSubscriptionUniqueness(t, tx)

			if test.name == "composite_constraint_is_preserved" {
				var count int64
				require.NoError(t, tx.Raw(
					"SELECT count(*) FROM pg_catalog.pg_constraint WHERE conrelid = to_regclass(?) AND conname = ?",
					"subscription_orders", "keep_subscription_orders_user_subscription_plan",
				).Scan(&count).Error)
				assert.EqualValues(t, 1, count, "multi-column unique constraints must not be touched")
			}
		})
	}
}
