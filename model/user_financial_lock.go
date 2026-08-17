package model

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// lockUsersForFinancialWriteTx locks the requested user rows in one stable
// order. Callers that must retain access to a soft-deleted financial owner can
// use this helper and apply their own active-user checks to the actor rows.
func lockUsersForFinancialWriteTx(tx *gorm.DB, userIds ...int) (map[int]*User, error) {
	if tx == nil || len(userIds) == 0 {
		return nil, errors.New("users and transaction are required")
	}

	uniqueUserIds := make(map[int]struct{}, len(userIds))
	orderedUserIds := make([]int, 0, len(userIds))
	for _, userId := range userIds {
		if userId <= 0 {
			return nil, errors.New("users and transaction are required")
		}
		if _, exists := uniqueUserIds[userId]; exists {
			continue
		}
		uniqueUserIds[userId] = struct{}{}
		orderedUserIds = append(orderedUserIds, userId)
	}
	sort.Ints(orderedUserIds)

	lockedUsers := make(map[int]*User, len(orderedUserIds))
	for _, userId := range orderedUserIds {
		user := &User{}
		if err := lockForUpdate(tx.Unscoped()).
			Select("id", "role", "quota", "refund_hold", "refund_debt_quota", "deleted_at").
			Where("id = ?", userId).
			First(user).Error; err != nil {
			return nil, err
		}

		if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			result := tx.Unscoped().Model(&User{}).
				Where("id = ?", userId).
				UpdateColumn("id", gorm.Expr("id"))
			if result.Error != nil {
				return nil, result.Error
			}
			if result.RowsAffected != 1 {
				return nil, gorm.ErrRecordNotFound
			}
		}
		lockedUsers[userId] = user
	}
	return lockedUsers, nil
}

// lockActiveUsersForFinancialWriteTx serializes durable financial writes for
// multiple active users. IDs are de-duplicated and locked in ascending order
// so opposite actor/target pairs cannot acquire the same rows in reverse.
func lockActiveUsersForFinancialWriteTx(tx *gorm.DB, userIds ...int) (map[int]*User, error) {
	lockedUsers, err := lockUsersForFinancialWriteTx(tx, userIds...)
	if err != nil {
		return nil, err
	}
	for _, user := range lockedUsers {
		if user.DeletedAt.Valid {
			return nil, gorm.ErrRecordNotFound
		}
	}
	return lockedUsers, nil
}

// LockActiveUsersForFinancialWriteTx exposes the shared financial actor lock
// to service-layer transactions that persist administrator evidence.
func LockActiveUsersForFinancialWriteTx(tx *gorm.DB, userIds ...int) (map[int]*User, error) {
	return lockActiveUsersForFinancialWriteTx(tx, userIds...)
}

// lockActiveUserForFinancialWriteTx serializes creation or mutation of durable
// financial records with permanent user deletion. The SQLite write fence is
// required because that dialect cannot express SELECT ... FOR UPDATE.
func lockActiveUserForFinancialWriteTx(tx *gorm.DB, userId int) (*User, error) {
	users, err := lockActiveUsersForFinancialWriteTx(tx, userId)
	if err != nil {
		return nil, err
	}
	return users[userId], nil
}
