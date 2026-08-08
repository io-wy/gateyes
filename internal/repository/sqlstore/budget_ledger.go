package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/pkg/eventbus"
	"github.com/gateyes/gateway/internal/repository"
)

const (
	budgetLedgerFlushLockTTL = 10 * time.Second
	budgetLedgerInitLockTTL  = 5 * time.Second
)

const consumeBudgetLedgerScript = `
local amount = tonumber(ARGV[1]) or 0
local tokens = tonumber(ARGV[2]) or 0
local has_quota = ARGV[3] == "1"
local first_budget = 1

if has_quota then
  local quota = tonumber(redis.call("HGET", KEYS[1], "quota")) or 0
  local used = tonumber(redis.call("HGET", KEYS[1], "used")) or 0
  if tokens > 0 and quota > 0 and used + tokens > quota then
    return {0, "quota"}
  end
  first_budget = 2
end

for i = first_budget, #KEYS do
  local budget = tonumber(redis.call("HGET", KEYS[i], "budget")) or 0
  local spent = tonumber(redis.call("HGET", KEYS[i], "spent")) or 0
  local reserved = tonumber(redis.call("HGET", KEYS[i], "reserved")) or 0
  local policy = redis.call("HGET", KEYS[i], "policy") or "hard_reject"
  if amount > 0 and budget > 0 and policy == "hard_reject" and spent + reserved + amount > budget then
    return {0, "budget"}
  end
end

if has_quota and tokens > 0 then
  redis.call("HINCRBYFLOAT", KEYS[1], "used", tokens)
  redis.call("HINCRBYFLOAT", KEYS[1], "delta_used", tokens)
end

if amount > 0 then
  for i = first_budget, #KEYS do
    redis.call("HINCRBYFLOAT", KEYS[i], "spent", amount)
    redis.call("HINCRBYFLOAT", KEYS[i], "delta_spent", amount)
  end
end

return {1, ""}
`

const reserveBudgetLedgerScript = `
local amount = tonumber(ARGV[1]) or 0
if amount <= 0 then
  return {1, ""}
end

for i = 1, #KEYS do
  local budget = tonumber(redis.call("HGET", KEYS[i], "budget")) or 0
  local spent = tonumber(redis.call("HGET", KEYS[i], "spent")) or 0
  local reserved = tonumber(redis.call("HGET", KEYS[i], "reserved")) or 0
  local policy = redis.call("HGET", KEYS[i], "policy") or "hard_reject"
  if budget > 0 and policy == "hard_reject" and spent + reserved + amount > budget then
    return {0, "budget"}
  end
end

for i = 1, #KEYS do
  redis.call("HINCRBYFLOAT", KEYS[i], "reserved", amount)
  redis.call("HINCRBYFLOAT", KEYS[i], "delta_reserved", amount)
end

return {1, ""}
`

const commitBudgetLedgerScript = `
local amount = tonumber(ARGV[1]) or 0
if amount <= 0 then
  return {1, ""}
end
for i = 1, #KEYS do
  redis.call("HINCRBYFLOAT", KEYS[i], "reserved", -amount)
  redis.call("HINCRBYFLOAT", KEYS[i], "spent", amount)
  redis.call("HINCRBYFLOAT", KEYS[i], "delta_reserved", -amount)
  redis.call("HINCRBYFLOAT", KEYS[i], "delta_spent", amount)
end
return {1, ""}
`

const releaseBudgetLedgerScript = `
local amount = tonumber(ARGV[1]) or 0
if amount <= 0 then
  return {1, ""}
end
for i = 1, #KEYS do
  redis.call("HINCRBYFLOAT", KEYS[i], "reserved", -amount)
  redis.call("HINCRBYFLOAT", KEYS[i], "delta_reserved", -amount)
end
return {1, ""}
`

type budgetScope struct {
	ID    string
	Scope string
	Table string
}

type budgetLedgerEvent struct {
	Scopes []budgetLedgerEventScope `json:"scopes,omitempty"`
	UserID string                   `json:"user_id,omitempty"`
}

type budgetLedgerEventScope struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

type budgetLedgerState struct {
	Budget   float64
	Spent    float64
	Reserved float64
	Policy   string
}

func (s *Store) consumeBudgetsRedis(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID, userID string, cost float64, tokens int) (bool, bool, error) {
	if s.rdb == nil {
		return false, false, nil
	}
	scopes := activeBudgetScopes(apiKeyID, projectID, tenantID, virtualKeyID)
	if cost > 0 {
		if err := s.ensureBudgetLedgerScopes(ctx, scopes); err != nil {
			return false, false, nil
		}
	}
	hasQuota := tokens > 0 && userID != ""
	if hasQuota {
		if err := s.ensureQuotaLedger(ctx, userID); err != nil {
			return false, false, nil
		}
	}

	keys := make([]string, 0, len(scopes)+1)
	if hasQuota {
		keys = append(keys, quotaLedgerKey(userID))
	}
	for _, sc := range scopes {
		keys = append(keys, budgetLedgerKey(sc.Scope, sc.ID))
	}
	if len(keys) == 0 {
		return true, true, nil
	}

	result, err := s.rdb.Eval(ctx, consumeBudgetLedgerScript, keys, cost, tokens, boolArg(hasQuota)).Result()
	if err != nil {
		return false, false, nil
	}
	allowed, reason := parseLedgerResult(result)
	if !allowed {
		if reason == "quota" {
			return false, true, repository.ErrQuotaExceeded
		}
		return false, true, nil
	}
	if err := s.publishBudgetLedgerFlush(ctx, scopes, userID); err != nil {
		return false, true, err
	}
	return true, true, nil
}

func (s *Store) reserveBudgetsRedis(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) (bool, bool, error) {
	if s.rdb == nil {
		return false, false, nil
	}
	scopes := activeBudgetScopes(apiKeyID, projectID, tenantID, virtualKeyID)
	if len(scopes) == 0 {
		return true, true, nil
	}
	if err := s.ensureBudgetLedgerScopes(ctx, scopes); err != nil {
		return false, false, nil
	}
	keys := budgetLedgerKeys(scopes)
	result, err := s.rdb.Eval(ctx, reserveBudgetLedgerScript, keys, amount).Result()
	if err != nil {
		return false, false, nil
	}
	allowed, _ := parseLedgerResult(result)
	if !allowed {
		return false, true, nil
	}
	if err := s.publishBudgetLedgerFlush(ctx, scopes, ""); err != nil {
		return false, true, err
	}
	return true, true, nil
}

func (s *Store) commitBudgetsRedis(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) (bool, error) {
	return s.adjustReservedBudgetsRedis(ctx, apiKeyID, projectID, tenantID, virtualKeyID, amount, commitBudgetLedgerScript)
}

func (s *Store) releaseBudgetsRedis(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) (bool, error) {
	return s.adjustReservedBudgetsRedis(ctx, apiKeyID, projectID, tenantID, virtualKeyID, amount, releaseBudgetLedgerScript)
}

func (s *Store) adjustReservedBudgetsRedis(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64, script string) (bool, error) {
	if s.rdb == nil {
		return false, nil
	}
	scopes := activeBudgetScopes(apiKeyID, projectID, tenantID, virtualKeyID)
	if len(scopes) == 0 {
		return true, nil
	}
	if err := s.ensureBudgetLedgerScopes(ctx, scopes); err != nil {
		return false, nil
	}
	if _, err := s.rdb.Eval(ctx, script, budgetLedgerKeys(scopes), amount).Result(); err != nil {
		return false, nil
	}
	if err := s.publishBudgetLedgerFlush(ctx, scopes, ""); err != nil {
		return true, err
	}
	return true, nil
}

func activeBudgetScopes(apiKeyID, projectID, tenantID, virtualKeyID string) []budgetScope {
	scopes := budgetScopes(apiKeyID, projectID, tenantID, virtualKeyID)
	active := make([]budgetScope, 0, len(scopes))
	for _, sc := range scopes {
		if sc.ID != "" {
			active = append(active, sc)
		}
	}
	return active
}

func budgetLedgerKeys(scopes []budgetScope) []string {
	keys := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		keys = append(keys, budgetLedgerKey(sc.Scope, sc.ID))
	}
	return keys
}

func (s *Store) ensureBudgetLedgerScopes(ctx context.Context, scopes []budgetScope) error {
	for _, sc := range scopes {
		if err := s.ensureBudgetLedgerScope(ctx, sc); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureBudgetLedgerScope(ctx context.Context, sc budgetScope) error {
	key := budgetLedgerKey(sc.Scope, sc.ID)
	exists, err := s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	lockKey := budgetLedgerInitLockKey(sc.Scope, sc.ID)
	locked, err := s.rdb.SetNX(ctx, lockKey, "1", budgetLedgerInitLockTTL).Result()
	if err != nil {
		return err
	}
	if !locked {
		return waitRedisKey(ctx, s.rdb, key)
	}
	defer s.rdb.Del(context.Background(), lockKey)

	exists, err = s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	state, err := s.loadBudgetLedgerScope(ctx, sc)
	if err != nil {
		return err
	}
	return s.rdb.HSet(ctx, key, map[string]any{
		"budget":         state.Budget,
		"spent":          state.Spent,
		"reserved":       state.Reserved,
		"policy":         state.Policy,
		"delta_spent":    0,
		"delta_reserved": 0,
	}).Err()
}

func waitRedisKey(ctx context.Context, rdb *redis.Client, key string) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(250 * time.Millisecond)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("wait redis key initialized: %s", key)
		case <-ticker.C:
			exists, err := rdb.Exists(ctx, key).Result()
			if err != nil {
				return err
			}
			if exists > 0 {
				return nil
			}
		}
	}
}

func (s *Store) loadBudgetLedgerScope(ctx context.Context, sc budgetScope) (budgetLedgerState, error) {
	if _, err := budgetTableForScope(sc.Scope); err != nil {
		return budgetLedgerState{}, err
	}
	var state budgetLedgerState
	err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(fmt.Sprintf(`
SELECT budget_usd, spent_usd, reserved_usd, budget_policy
FROM %s
WHERE id = ?`, sc.Table)), sc.ID).Scan(&state.Budget, &state.Spent, &state.Reserved, &state.Policy)
	if errors.Is(err, sql.ErrNoRows) {
		return budgetLedgerState{}, repository.ErrNotFound
	}
	if err != nil {
		return budgetLedgerState{}, fmt.Errorf("load %s budget ledger: %w", sc.Scope, err)
	}
	if state.Policy == "" {
		state.Policy = repository.BudgetPolicyHardReject
	}
	return state, nil
}

func (s *Store) ensureQuotaLedger(ctx context.Context, userID string) error {
	key := quotaLedgerKey(userID)
	exists, err := s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	lockKey := quotaLedgerInitLockKey(userID)
	locked, err := s.rdb.SetNX(ctx, lockKey, "1", budgetLedgerInitLockTTL).Result()
	if err != nil {
		return err
	}
	if !locked {
		return waitRedisKey(ctx, s.rdb, key)
	}
	defer s.rdb.Del(context.Background(), lockKey)

	exists, err = s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	var quota, used int
	err = s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT quota, used
FROM users
WHERE id = ?`), userID).Scan(&quota, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load user quota ledger: %w", err)
	}
	return s.rdb.HSet(ctx, key, map[string]any{
		"quota":      quota,
		"used":       used,
		"delta_used": 0,
	}).Err()
}

func (s *Store) publishBudgetLedgerFlush(ctx context.Context, scopes []budgetScope, userID string) error {
	event := budgetLedgerEvent{UserID: userID}
	for _, sc := range scopes {
		event.Scopes = append(event.Scopes, budgetLedgerEventScope{Scope: sc.Scope, ID: sc.ID})
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal budget ledger event: %w", err)
	}
	if s.eventBus != nil && s.eventBus.PublishEvent(ctx, eventbus.Event{
		Key:     budgetLedgerEventKey(event),
		Type:    eventbus.EventTypeBudgetLedger,
		Payload: payload,
	}) {
		return nil
	}
	return s.flushBudgetLedgerDeltas(ctx, event)
}

func budgetLedgerEventKey(event budgetLedgerEvent) string {
	if event.UserID != "" {
		return event.UserID
	}
	if len(event.Scopes) > 0 {
		return event.Scopes[len(event.Scopes)-1].ID
	}
	return eventbus.EventTypeBudgetLedger
}

func (s *Store) handleBudgetLedgerEvent(ctx context.Context, payload []byte) error {
	var event budgetLedgerEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode budget ledger event: %w", err)
	}
	return s.flushBudgetLedgerDeltas(ctx, event)
}

func (s *Store) flushBudgetLedgerDeltas(ctx context.Context, event budgetLedgerEvent) error {
	if s.rdb == nil {
		return nil
	}
	if event.UserID != "" {
		if err := s.flushQuotaLedgerDelta(ctx, event.UserID); err != nil {
			return err
		}
	}
	for _, sc := range event.Scopes {
		if err := s.flushBudgetScopeDelta(ctx, sc.Scope, sc.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) FlushBudgetLedgerDeltas(ctx context.Context) error {
	if s.rdb == nil {
		return nil
	}
	if err := s.scanBudgetLedgerDeltas(ctx); err != nil {
		return err
	}
	return s.scanQuotaLedgerDeltas(ctx)
}

func (s *Store) scanBudgetLedgerDeltas(ctx context.Context) error {
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, "budget:ledger:*:*", 100).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			scope, id, ok := parseBudgetLedgerKey(key)
			if !ok {
				continue
			}
			if err := s.flushBudgetScopeDelta(ctx, scope, id); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

func (s *Store) scanQuotaLedgerDeltas(ctx context.Context) error {
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, "quota:ledger:user:*", 100).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			userID := strings.TrimPrefix(key, "quota:ledger:user:")
			if userID == "" || strings.Contains(userID, ":") {
				continue
			}
			if err := s.flushQuotaLedgerDelta(ctx, userID); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

func parseBudgetLedgerKey(key string) (string, string, bool) {
	rest := strings.TrimPrefix(key, "budget:ledger:")
	if rest == key {
		return "", "", false
	}
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if _, err := budgetTableForScope(parts[0]); err != nil {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *Store) flushBudgetScopeDelta(ctx context.Context, scope, id string) error {
	table, err := budgetTableForScope(scope)
	if err != nil {
		return err
	}
	lockKey := budgetLedgerFlushLockKey(scope, id)
	locked, err := s.rdb.SetNX(ctx, lockKey, "1", budgetLedgerFlushLockTTL).Result()
	if err != nil || !locked {
		return err
	}
	defer s.rdb.Del(context.Background(), lockKey)

	key := budgetLedgerKey(scope, id)
	spentDelta, err := redisHashFloat(ctx, s.rdb, key, "delta_spent")
	if err != nil {
		return err
	}
	reservedDelta, err := redisHashFloat(ctx, s.rdb, key, "delta_reserved")
	if err != nil {
		return err
	}
	if spentDelta == 0 && reservedDelta == 0 {
		return nil
	}

	now := time.Now().UTC()
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE %s
SET spent_usd = spent_usd + ?,
    reserved_usd = CASE WHEN reserved_usd + ? < 0 THEN 0 ELSE reserved_usd + ? END,
    updated_at = ?
WHERE id = ?`, table)), spentDelta, reservedDelta, reservedDelta, now, id); err != nil {
		return fmt.Errorf("flush %s budget ledger: %w", scope, err)
	}
	if spentDelta != 0 {
		_ = s.rdb.HIncrByFloat(ctx, key, "delta_spent", -spentDelta).Err()
	}
	if reservedDelta != 0 {
		_ = s.rdb.HIncrByFloat(ctx, key, "delta_reserved", -reservedDelta).Err()
	}
	s.budgetCacheDelete(ctx, scope, id)
	return nil
}

func (s *Store) flushQuotaLedgerDelta(ctx context.Context, userID string) error {
	lockKey := quotaLedgerFlushLockKey(userID)
	locked, err := s.rdb.SetNX(ctx, lockKey, "1", budgetLedgerFlushLockTTL).Result()
	if err != nil || !locked {
		return err
	}
	defer s.rdb.Del(context.Background(), lockKey)

	key := quotaLedgerKey(userID)
	usedDelta, err := redisHashFloat(ctx, s.rdb, key, "delta_used")
	if err != nil {
		return err
	}
	if usedDelta == 0 {
		return nil
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE users
SET used = used + ?, updated_at = ?
WHERE id = ?`), int(usedDelta), time.Now().UTC(), userID); err != nil {
		return fmt.Errorf("flush user quota ledger: %w", err)
	}
	_ = s.rdb.HIncrByFloat(ctx, key, "delta_used", -usedDelta).Err()
	return nil
}

func (s *Store) invalidateBudgetLedgerScope(ctx context.Context, scope, id string) {
	if s.rdb == nil || id == "" {
		return
	}
	_ = s.flushBudgetScopeDelta(ctx, scope, id)
	_ = s.rdb.Del(ctx, budgetLedgerKey(scope, id)).Err()
}

func (s *Store) invalidateQuotaLedger(ctx context.Context, userID string) {
	if s.rdb == nil || userID == "" {
		return
	}
	_ = s.flushQuotaLedgerDelta(ctx, userID)
	_ = s.rdb.Del(ctx, quotaLedgerKey(userID)).Err()
}

func (s *Store) invalidateAPIKeyLedgersForUser(ctx context.Context, userID string) {
	if s.rdb == nil || userID == "" {
		return
	}
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`SELECT id FROM api_keys WHERE user_id = ?`), userID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var apiKeyID string
		if err := rows.Scan(&apiKeyID); err == nil {
			s.invalidateBudgetLedgerScope(ctx, "api_key", apiKeyID)
		}
	}
}

func budgetTableForScope(scope string) (string, error) {
	switch scope {
	case "virtual_key":
		return "virtual_keys", nil
	case "api_key":
		return "api_keys", nil
	case "project":
		return "projects", nil
	case "tenant":
		return "tenants", nil
	default:
		return "", fmt.Errorf("unsupported budget scope: %s", scope)
	}
}

func redisHashFloat(ctx context.Context, rdb *redis.Client, key, field string) (float64, error) {
	value, err := rdb.HGet(ctx, key, field).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse redis hash float %s/%s: %w", key, field, err)
	}
	return parsed, nil
}

func parseLedgerResult(result any) (bool, string) {
	items, ok := result.([]any)
	if !ok || len(items) == 0 {
		return false, ""
	}
	allowed := fmt.Sprint(items[0]) == "1"
	reason := ""
	if len(items) > 1 {
		reason = fmt.Sprint(items[1])
	}
	return allowed, reason
}

func boolArg(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
