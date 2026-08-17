package model

// BackfillNullUserRefundHolds normalizes rows created before refund_hold had a
// durable boolean value. The GORM predicate and assignment are portable across
// SQLite, MySQL, and PostgreSQL. Unscoped includes soft-deleted users so a
// later restore cannot revive a NULL legacy value.
func BackfillNullUserRefundHolds() error {
	migrator := DB.Migrator()
	if !migrator.HasTable(&User{}) || !migrator.HasColumn(&User{}, "refund_hold") {
		return nil
	}
	return DB.Unscoped().Model(&User{}).
		Where("refund_hold IS NULL").
		UpdateColumn("refund_hold", false).Error
}
