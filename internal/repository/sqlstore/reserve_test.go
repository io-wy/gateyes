package sqlstore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/pkg/eventbus"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/testutil"
)

// reserveTestTenant creates an active tenant with the given budget and returns its ID.
func reserveTestTenant(t *testing.T, store *Store, id string, budget float64) string {
	t.Helper()
	tenant, err := store.EnsureTenant(context.Background(), repository.EnsureTenantParams{
		ID:        id,
		Slug:      id,
		Name:      id,
		Status:    repository.StatusActive,
		BudgetUSD: budget,
	})
	if err != nil {
		t.Fatalf("ensure tenant %s: %v", id, err)
	}
	return tenant.ID
}

// readReservedSpent reads the reserved_usd and spent_usd columns directly, since
// CheckXxxBudget does not surface reserved_usd.
func readReservedSpent(t *testing.T, store *Store, table, id string) (reserved, spent float64) {
	t.Helper()
	if err := store.db.Conn.QueryRowContext(context.Background(), store.db.Rebind(
		"SELECT reserved_usd, spent_usd FROM "+table+" WHERE id = ?"), id).Scan(&reserved, &spent); err != nil {
		t.Fatalf("read %s reserved/spent for %s: %v", table, id, err)
	}
	return reserved, spent
}

func readUserUsed(t *testing.T, store *Store, userID string) int {
	t.Helper()
	var used int
	if err := store.db.Conn.QueryRowContext(context.Background(), store.db.Rebind(
		"SELECT used FROM users WHERE id = ?"), userID).Scan(&used); err != nil {
		t.Fatalf("read user used for %s: %v", userID, err)
	}
	return used
}

func TestReserveBudgetsSucceedsWithinBudget(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-reserve-ok", 100)

	ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 30)
	if err != nil {
		t.Fatalf("ReserveBudgets: %v", err)
	}
	if !ok {
		t.Fatal("ReserveBudgets(30) within budget 100 = false, want true")
	}
	reserved, spent := readReservedSpent(t, store, "tenants", tenantID)
	if reserved != 30 || spent != 0 {
		t.Fatalf("after reserve: reserved=%v spent=%v, want reserved=30 spent=0", reserved, spent)
	}
}

// TestReserveBudgetsRollsBackWhenOneScopeInsufficient is the core guarantee:
// when reserving across multiple scopes and any one scope lacks budget, the whole
// reservation fails AND the scopes that already succeeded are rolled back — no
// partial reservation may leak (that would let concurrent requests over-commit).
func TestReserveBudgetsRollsBackWhenOneScopeInsufficient(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-reserve-rollback", 100)
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenantID,
		Slug:      "proj-reserve-rollback",
		Name:      "Reserve Rollback Project",
		Status:    repository.StatusActive,
		BudgetUSD: 10, // tighter than the tenant
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// project (budget 10) cannot fit 50, tenant (budget 100) can.
	ok, err := store.ReserveBudgets(ctx, "", project.ID, tenantID, "", 50)
	if err != nil {
		t.Fatalf("ReserveBudgets: %v", err)
	}
	if ok {
		t.Fatal("ReserveBudgets(50) with project budget 10 = true, want false")
	}

	// Both scopes must be untouched — including the tenant scope which on its own
	// had room. A leaked tenant reservation is exactly the over-commit bug.
	if reserved, _ := readReservedSpent(t, store, "projects", project.ID); reserved != 0 {
		t.Fatalf("project reserved_usd = %v after failed reserve, want 0 (rolled back)", reserved)
	}
	if reserved, _ := readReservedSpent(t, store, "tenants", tenantID); reserved != 0 {
		t.Fatalf("tenant reserved_usd = %v after failed reserve, want 0 (rolled back)", reserved)
	}
}

// TestReserveBudgetsAccumulatesUntilExhausted verifies reservations stack and the
// available headroom shrinks — the mechanism that prevents concurrent over-commit.
func TestReserveBudgetsAccumulatesUntilExhausted(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-reserve-accum", 100)

	if ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 30); err != nil || !ok {
		t.Fatalf("first reserve(30) = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 50); err != nil || !ok {
		t.Fatalf("second reserve(50) = (%v,%v), want (true,nil)", ok, err)
	}
	if reserved, _ := readReservedSpent(t, store, "tenants", tenantID); reserved != 80 {
		t.Fatalf("reserved after 30+50 = %v, want 80", reserved)
	}
	// 80 already reserved, 30 more would exceed budget 100 -> rejected, no change.
	if ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 30); err != nil || ok {
		t.Fatalf("third reserve(30) over budget = (%v,%v), want (false,nil)", ok, err)
	}
	if reserved, _ := readReservedSpent(t, store, "tenants", tenantID); reserved != 80 {
		t.Fatalf("reserved after rejected third reserve = %v, want unchanged 80", reserved)
	}
}

func TestReserveBudgetsUnlimitedAndNonPositiveAmount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// budget_usd <= 0 means unlimited: any amount reserves successfully.
	unlimited := reserveTestTenant(t, store, "tenant-reserve-unlimited", 0)
	if ok, err := store.ReserveBudgets(ctx, "", "", unlimited, "", 99999); err != nil || !ok {
		t.Fatalf("reserve against unlimited budget = (%v,%v), want (true,nil)", ok, err)
	}

	// amount <= 0 is a no-op that always succeeds and changes nothing.
	capped := reserveTestTenant(t, store, "tenant-reserve-zero", 100)
	if ok, err := store.ReserveBudgets(ctx, "", "", capped, "", 0); err != nil || !ok {
		t.Fatalf("reserve(0) = (%v,%v), want (true,nil)", ok, err)
	}
	if reserved, _ := readReservedSpent(t, store, "tenants", capped); reserved != 0 {
		t.Fatalf("reserved after reserve(0) = %v, want 0", reserved)
	}
}

func TestConsumeBudgetsRedisLedgerDefersPGFlush(t *testing.T) {
	store := newTestStore(t)
	rdb := testutil.NewRedisClient(t)
	store.SetRedis(rdb)
	bus := eventbus.New(eventbus.Options{Buffer: 8, Workers: 1})
	store.SetEventBus(bus)
	t.Cleanup(func() { _ = bus.Close() })

	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-ledger-defer", 100)
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenantID,
		Key:        "ledger-defer-key",
		SecretHash: repository.HashSecret("secret"),
		Name:       "ledger defer",
		Email:      "ledger-defer@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "ledger-defer-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	ok, err := store.ConsumeBudgets(ctx, identity.APIKeyID, "", tenantID, "", identity.UserID, 12, 7)
	if err != nil || !ok {
		t.Fatalf("ConsumeBudgets(redis) = (%v,%v), want (true,nil)", ok, err)
	}
	if _, spent := readReservedSpent(t, store, "tenants", tenantID); spent != 0 {
		t.Fatalf("tenant spent before eventbus start = %v, want 0", spent)
	}
	if used := readUserUsed(t, store, identity.UserID); used != 0 {
		t.Fatalf("user used before eventbus start = %v, want 0", used)
	}
	ledgerSpent, err := rdb.HGet(ctx, budgetLedgerKey("tenant", tenantID), "spent").Float64()
	if err != nil || ledgerSpent != 12 {
		t.Fatalf("redis ledger spent = (%v,%v), want (12,nil)", ledgerSpent, err)
	}
	quotaUsed, err := rdb.HGet(ctx, quotaLedgerKey(identity.UserID), "used").Float64()
	if err != nil || quotaUsed != 7 {
		t.Fatalf("redis quota used = (%v,%v), want (7,nil)", quotaUsed, err)
	}

	bus.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, spent := readReservedSpent(t, store, "tenants", tenantID)
		used := readUserUsed(t, store, identity.UserID)
		if spent == 12 && used == 7 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ledger deltas not flushed: spent=%v used=%v", spent, used)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConsumeBudgetsRedisLedgerRejectsConcurrentOverBudget(t *testing.T) {
	store := newTestStore(t)
	rdb := testutil.NewRedisClient(t)
	store.SetRedis(rdb)
	store.SetEventBus(eventbus.New(eventbus.Options{Buffer: 64, Workers: 1}))

	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-ledger-concurrent", 100)
	if err := store.ensureBudgetLedgerScopes(ctx, activeBudgetScopes("", "", tenantID, "")); err != nil {
		t.Fatalf("ensure ledger: %v", err)
	}

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := store.ConsumeBudgets(ctx, "", "", tenantID, "", "", 3, 0)
			if err != nil {
				t.Errorf("ConsumeBudgets(redis) error: %v", err)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if allowed.Load() != 33 {
		t.Fatalf("allowed requests = %d, want 33", allowed.Load())
	}
	ledgerSpent, err := rdb.HGet(ctx, budgetLedgerKey("tenant", tenantID), "spent").Float64()
	if err != nil || ledgerSpent != 99 {
		t.Fatalf("redis ledger spent = (%v,%v), want (99,nil)", ledgerSpent, err)
	}
	if _, spent := readReservedSpent(t, store, "tenants", tenantID); spent != 0 {
		t.Fatalf("tenant PG spent before eventbus start = %v, want 0", spent)
	}
}

func TestReserveCommitReleaseBudgetsRedisLedger(t *testing.T) {
	store := newTestStore(t)
	rdb := testutil.NewRedisClient(t)
	store.SetRedis(rdb)
	bus := eventbus.New(eventbus.Options{Buffer: 16, Workers: 1})
	store.SetEventBus(bus)
	t.Cleanup(func() { _ = bus.Close() })

	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-ledger-reserve", 100)
	ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 40)
	if err != nil || !ok {
		t.Fatalf("ReserveBudgets(redis) = (%v,%v), want (true,nil)", ok, err)
	}
	redisReserved, err := rdb.HGet(ctx, budgetLedgerKey("tenant", tenantID), "reserved").Float64()
	if err != nil || redisReserved != 40 {
		t.Fatalf("redis reserved after reserve = (%v,%v), want (40,nil)", redisReserved, err)
	}

	if err := store.ReleaseBudgets(ctx, "", "", tenantID, "", 10); err != nil {
		t.Fatalf("ReleaseBudgets(redis): %v", err)
	}
	redisReserved, err = rdb.HGet(ctx, budgetLedgerKey("tenant", tenantID), "reserved").Float64()
	if err != nil || redisReserved != 30 {
		t.Fatalf("redis reserved after release = (%v,%v), want (30,nil)", redisReserved, err)
	}

	if err := store.CommitBudgets(ctx, "", "", tenantID, "", 30); err != nil {
		t.Fatalf("CommitBudgets(redis): %v", err)
	}
	redisReserved, err = rdb.HGet(ctx, budgetLedgerKey("tenant", tenantID), "reserved").Float64()
	if err != nil || redisReserved != 0 {
		t.Fatalf("redis reserved after commit = (%v,%v), want (0,nil)", redisReserved, err)
	}
	redisSpent, err := rdb.HGet(ctx, budgetLedgerKey("tenant", tenantID), "spent").Float64()
	if err != nil || redisSpent != 30 {
		t.Fatalf("redis spent after commit = (%v,%v), want (30,nil)", redisSpent, err)
	}
	if reserved, spent := readReservedSpent(t, store, "tenants", tenantID); reserved != 0 || spent != 0 {
		t.Fatalf("PG before eventbus start = reserved:%v spent:%v, want zero", reserved, spent)
	}

	bus.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		reserved, spent := readReservedSpent(t, store, "tenants", tenantID)
		if reserved == 0 && spent == 30 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reserve ledger deltas not flushed: reserved=%v spent=%v", reserved, spent)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCommitBudgetsMovesReservedToSpent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-commit", 100)

	if ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 40); err != nil || !ok {
		t.Fatalf("reserve(40) = (%v,%v), want (true,nil)", ok, err)
	}
	if err := store.CommitBudgets(ctx, "", "", tenantID, "", 40); err != nil {
		t.Fatalf("CommitBudgets: %v", err)
	}
	// Reserved is released, spent grows by the committed amount.
	reserved, spent := readReservedSpent(t, store, "tenants", tenantID)
	if reserved != 0 || spent != 40 {
		t.Fatalf("after commit: reserved=%v spent=%v, want reserved=0 spent=40", reserved, spent)
	}
}

func TestReleaseBudgetsFreesReservationWithoutSpending(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenantID := reserveTestTenant(t, store, "tenant-release", 100)

	if ok, err := store.ReserveBudgets(ctx, "", "", tenantID, "", 40); err != nil || !ok {
		t.Fatalf("reserve(40) = (%v,%v), want (true,nil)", ok, err)
	}
	if err := store.ReleaseBudgets(ctx, "", "", tenantID, "", 40); err != nil {
		t.Fatalf("ReleaseBudgets: %v", err)
	}
	// Reservation freed, nothing spent.
	reserved, spent := readReservedSpent(t, store, "tenants", tenantID)
	if reserved != 0 || spent != 0 {
		t.Fatalf("after release: reserved=%v spent=%v, want reserved=0 spent=0", reserved, spent)
	}
}
