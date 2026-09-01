package model

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	subscriptionOrderUserSubscriptionColumn = "user_subscription_id"
	subscriptionOrderUserSubscriptionIndex  = "idx_subscription_orders_user_subscription_id"
)

type subscriptionOrderUserSubscriptionConstraint struct {
	Name string `gorm:"column:constraint_name"`
}

type subscriptionOrderUserSubscriptionIndexState struct {
	exists          bool
	definitionValid bool
	standaloneValid bool
}

func inspectSubscriptionOrderUserSubscriptionConstraints(db *gorm.DB, tableName string) ([]subscriptionOrderUserSubscriptionConstraint, error) {
	var constraints []subscriptionOrderUserSubscriptionConstraint
	if err := db.Raw(`
SELECT constraint_meta.conname AS constraint_name
FROM pg_catalog.pg_constraint AS constraint_meta
WHERE constraint_meta.conrelid = to_regclass(?)
  AND constraint_meta.contype = 'u'
  AND cardinality(constraint_meta.conkey) = 1
  AND EXISTS (
      SELECT 1
      FROM pg_catalog.pg_attribute AS attribute_meta
      WHERE attribute_meta.attrelid = constraint_meta.conrelid
        AND attribute_meta.attnum = constraint_meta.conkey[1]
        AND attribute_meta.attname = ?
  )
ORDER BY constraint_meta.conname`, tableName, subscriptionOrderUserSubscriptionColumn).Scan(&constraints).Error; err != nil {
		return nil, fmt.Errorf("inspect subscription order user_subscription_id unique constraints: %w", err)
	}
	return constraints, nil
}

func inspectSubscriptionOrderUserSubscriptionIndex(db *gorm.DB, tableName string) (subscriptionOrderUserSubscriptionIndexState, error) {
	var state struct {
		Exists          bool `gorm:"column:index_exists"`
		DefinitionValid bool `gorm:"column:definition_valid"`
		StandaloneValid bool `gorm:"column:standalone_valid"`
	}
	if err := db.Raw(`
SELECT count(*) > 0 AS index_exists,
       COALESCE(bool_or(
           index_meta.indisunique
           AND index_meta.indisvalid
           AND index_meta.indisready
           AND NOT index_meta.indisprimary
           AND index_meta.indpred IS NULL
           AND index_meta.indexprs IS NULL
           AND index_meta.indnatts = 1
           AND attribute_meta.attname = ?
       ), false) AS definition_valid,
       COALESCE(bool_or(
           index_meta.indisunique
           AND index_meta.indisvalid
           AND index_meta.indisready
           AND NOT index_meta.indisprimary
           AND index_meta.indpred IS NULL
           AND index_meta.indexprs IS NULL
           AND index_meta.indnatts = 1
           AND attribute_meta.attname = ?
           AND NOT EXISTS (
               SELECT 1
               FROM pg_catalog.pg_constraint AS constraint_meta
               WHERE constraint_meta.conindid = index_meta.indexrelid
           )
       ), false) AS standalone_valid
FROM pg_catalog.pg_index AS index_meta
JOIN pg_catalog.pg_class AS index_class
  ON index_class.oid = index_meta.indexrelid
LEFT JOIN pg_catalog.pg_attribute AS attribute_meta
  ON attribute_meta.attrelid = index_meta.indrelid
 AND attribute_meta.attnum = index_meta.indkey[0]
WHERE index_meta.indrelid = to_regclass(?)
  AND index_class.relname = ?`, subscriptionOrderUserSubscriptionColumn, subscriptionOrderUserSubscriptionColumn, tableName, subscriptionOrderUserSubscriptionIndex).Scan(&state).Error; err != nil {
		return subscriptionOrderUserSubscriptionIndexState{}, fmt.Errorf("inspect subscription order user_subscription_id unique index: %w", err)
	}
	return subscriptionOrderUserSubscriptionIndexState{
		exists:          state.Exists,
		definitionValid: state.DefinitionValid,
		standaloneValid: state.StandaloneValid,
	}, nil
}

// migrateSubscriptionOrderUserSubscriptionUniqueness converts known
// PostgreSQL UNIQUE constraints left on subscription_orders.user_subscription_id
// into the standalone uniqueIndex represented by the current model. GORM's
// AutoMigrate reconciles uniqueness by dropping a constraint named after its
// own naming strategy, which does not exist on databases where the legacy
// guarantee is a constraint with a different name, so the reconciliation must
// happen before AutoMigrate. Multi-column constraints and partial indexes on
// the column are not touched.
func migrateSubscriptionOrderUserSubscriptionUniqueness(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate subscription order user_subscription_id uniqueness: database is nil")
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&SubscriptionOrder{}); err != nil {
		return fmt.Errorf("parse subscription order schema: %w", err)
	}
	tableName := statement.Schema.Table
	if !db.Migrator().HasTable(&SubscriptionOrder{}) {
		return nil
	}

	constraints, err := inspectSubscriptionOrderUserSubscriptionConstraints(db, tableName)
	if err != nil {
		return err
	}
	if len(constraints) == 0 {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		migrator := tx.Migrator()

		if err := tx.Exec(
			"LOCK TABLE ? IN ACCESS EXCLUSIVE MODE",
			clause.Table{Name: tableName},
		).Error; err != nil {
			return fmt.Errorf("lock subscription orders for user_subscription_id uniqueness migration: %w", err)
		}

		constraints, err := inspectSubscriptionOrderUserSubscriptionConstraints(tx, tableName)
		if err != nil {
			return err
		}
		if len(constraints) == 0 {
			return nil
		}

		targetIndex, err := inspectSubscriptionOrderUserSubscriptionIndex(tx, tableName)
		if err != nil {
			return err
		}
		if targetIndex.exists && !targetIndex.definitionValid {
			return fmt.Errorf("subscription order index %q has an unexpected definition", subscriptionOrderUserSubscriptionIndex)
		}

		for _, constraint := range constraints {
			if err := migrator.DropConstraint(&SubscriptionOrder{}, constraint.Name); err != nil {
				return fmt.Errorf("drop subscription order user_subscription_id unique constraint %q: %w", constraint.Name, err)
			}
		}

		targetIndex, err = inspectSubscriptionOrderUserSubscriptionIndex(tx, tableName)
		if err != nil {
			return err
		}
		if !targetIndex.exists {
			if err := migrator.CreateIndex(&SubscriptionOrder{}, subscriptionOrderUserSubscriptionIndex); err != nil {
				return fmt.Errorf("create subscription order user_subscription_id unique index: %w", err)
			}
			targetIndex, err = inspectSubscriptionOrderUserSubscriptionIndex(tx, tableName)
			if err != nil {
				return err
			}
		}
		if !targetIndex.standaloneValid {
			return fmt.Errorf("subscription order index %q has an unexpected definition", subscriptionOrderUserSubscriptionIndex)
		}

		remainingConstraints, err := inspectSubscriptionOrderUserSubscriptionConstraints(tx, tableName)
		if err != nil {
			return err
		}
		if len(remainingConstraints) != 0 {
			return fmt.Errorf("subscription_orders.user_subscription_id still has unique constraints after migration")
		}
		return nil
	})
}
