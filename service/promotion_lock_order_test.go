package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type promotionRowLockTrace struct {
	mu     sync.Mutex
	tables []string
}

func (trace *promotionRowLockTrace) reset() {
	trace.mu.Lock()
	trace.tables = nil
	trace.mu.Unlock()
}

func (trace *promotionRowLockTrace) record(tx *gorm.DB) {
	if _, locked := tx.Statement.Clauses["FOR"]; !locked {
		return
	}
	table := tx.Statement.Table
	if tx.Statement.Schema != nil {
		table = tx.Statement.Schema.Table
	}
	trace.mu.Lock()
	trace.tables = append(trace.tables, table)
	trace.mu.Unlock()
	// The test database is SQLite. Preserve the production lock intent in the
	// trace, then remove the unsupported clause before SQLite executes the query.
	delete(tx.Statement.Clauses, "FOR")
}

func (trace *promotionRowLockTrace) firstOrder(tables ...string) []string {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	wanted := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		wanted[table] = struct{}{}
	}
	seen := make(map[string]struct{}, len(tables))
	order := make([]string, 0, len(tables))
	for _, table := range trace.tables {
		if _, ok := wanted[table]; !ok {
			continue
		}
		if _, ok := seen[table]; ok {
			continue
		}
		seen[table] = struct{}{}
		order = append(order, table)
	}
	return order
}

func TestPromotionOutflowRowLockOrder(t *testing.T) {
	trace := &promotionRowLockTrace{}
	const callbackName = "test:promotion-row-lock-order"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, trace.record))
	previousDatabaseType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	t.Cleanup(func() {
		common.SetMainDatabaseType(previousDatabaseType)
		model.DB.Callback().Query().Remove(callbackName)
	})

	t.Run("transfer locks ledgers before user", func(t *testing.T) {
		truncate(t)
		seedUser(t, 8101, 0)
		seedPromotionCommissionLedger(t, 8101, 1_000, 5_000)
		trace.reset()

		_, err := TransferAllSettledPromotionCommissionsToQuota(8101, promotionCommissionBalanceExpectation(t, 8101))
		require.NoError(t, err)
		assert.Equal(t, []string{"promotion_commission_ledgers", "users"},
			trace.firstOrder("promotion_commission_ledgers", "users"))
	})

	t.Run("withdrawal review locks withdrawal then ledgers then user", func(t *testing.T) {
		truncate(t)
		seedUser(t, 91, 0)
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 91).Update("role", common.RoleAdminUser).Error)
		seedUser(t, 8102, 0)
		seedPromotionCommissionLedger(t, 8102, 1_000, 5_000)
		request := withPromotionCommissionBalanceExpectation(t, 8102, PromotionWithdrawalRequest{
			PayoutMethod:  "bank",
			PayoutAccount: "review-lock-order",
		})
		trace.reset()
		withdrawal, err := CreatePromotionWithdrawal(8102, request)
		require.NoError(t, err)
		assert.Equal(t, []string{"promotion_commission_ledgers", "users"},
			trace.firstOrder("promotion_commission_ledgers", "users"))
		trace.reset()

		_, err = AdminApprovePromotionWithdrawal(withdrawal.Id, 91, PromotionWithdrawalReviewRequest{ReviewNote: "approved"})
		require.NoError(t, err)
		assert.Equal(t, []string{"promotion_withdrawals", "promotion_commission_ledgers", "users"},
			trace.firstOrder("promotion_withdrawals", "promotion_commission_ledgers", "users"))
	})
}
