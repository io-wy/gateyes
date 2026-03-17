package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateUsageRecord(ctx context.Context, record repository.UsageRecord) error {
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO usage_records (
	id, tenant_id, user_id, api_key_id, provider_name, model,
	prompt_tokens, completion_tokens, total_tokens, cost, latency_ms,
	status, error_type, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.TenantID,
		record.UserID,
		record.APIKeyID,
		record.ProviderName,
		record.Model,
		record.PromptTokens,
		record.CompletionTokens,
		record.TotalTokens,
		record.Cost,
		record.LatencyMs,
		record.Status,
		record.ErrorType,
		record.CreatedAt,
	); err != nil {
		return fmt.Errorf("create usage record: %w", err)
	}

	return nil
}

func (s *Store) GetUsageSummary(ctx context.Context, tenantID string) (*repository.UsageStats, error) {
	query := `
SELECT COUNT(1),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 0 ELSE 1 END), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(AVG(latency_ms), 0)
FROM usage_records`
	args := make([]any, 0, 1)
	if tenantID != "" {
		query += `
WHERE tenant_id = ?`
		args = append(args, tenantID)
	}

	stats := &repository.UsageStats{}
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	if err := row.Scan(
		&stats.TotalRequests,
		&stats.SuccessRequests,
		&stats.FailedRequests,
		&stats.TotalTokens,
		&stats.AvgLatencyMs,
	); err != nil {
		return nil, fmt.Errorf("get usage summary: %w", err)
	}
	return stats, nil
}

func (s *Store) GetProviderUsageSummary(ctx context.Context, tenantID string) (map[string]repository.ProviderUsageStats, error) {
	query := `
SELECT provider_name,
	COUNT(1),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 0 ELSE 1 END), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(AVG(latency_ms), 0)
FROM usage_records`
	args := make([]any, 0, 1)
	if tenantID != "" {
		query += `
WHERE tenant_id = ?`
		args = append(args, tenantID)
	}
	query += `
GROUP BY provider_name`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("get provider usage summary: %w", err)
	}
	defer rows.Close()

	result := make(map[string]repository.ProviderUsageStats)
	for rows.Next() {
		var stat repository.ProviderUsageStats
		if err := rows.Scan(
			&stat.ProviderName,
			&stat.TotalRequests,
			&stat.SuccessRequests,
			&stat.FailedRequests,
			&stat.TotalTokens,
			&stat.AvgLatencyMs,
		); err != nil {
			return nil, fmt.Errorf("scan provider usage summary: %w", err)
		}
		result[stat.ProviderName] = stat
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider usage summary: %w", err)
	}

	return result, nil
}
