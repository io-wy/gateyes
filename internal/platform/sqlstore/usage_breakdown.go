package sqlstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) GetUsageSummaryFiltered(ctx context.Context, filter repository.UsageFilter) (*repository.UsageStats, error) {
	query := `
SELECT COUNT(1),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 0 ELSE 1 END), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(cost), 0),
	COALESCE(AVG(latency_ms), 0)
FROM usage_records`
	where, args := usageFilterWhere(filter)
	if where != "" {
		query += " WHERE " + where
	}

	stats := &repository.UsageStats{}
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	if err := row.Scan(
		&stats.TotalRequests,
		&stats.SuccessRequests,
		&stats.FailedRequests,
		&stats.TotalTokens,
		&stats.TotalCostUSD,
		&stats.AvgLatencyMs,
	); err != nil {
		return nil, fmt.Errorf("get filtered usage summary: %w", err)
	}
	return stats, nil
}

func (s *Store) GetUsageBreakdown(ctx context.Context, filter repository.UsageFilter, dimension string) ([]repository.UsageBreakdownRow, error) {
	column, err := usageDimensionColumn(dimension)
	if err != nil {
		return nil, err
	}

	query := `
SELECT COALESCE(` + column + `, '') AS dimension,
	COUNT(1),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 0 ELSE 1 END), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(cost), 0),
	COALESCE(AVG(latency_ms), 0)
FROM usage_records`
	where, args := usageFilterWhere(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += `
GROUP BY ` + column + `
ORDER BY COUNT(1) DESC, dimension ASC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("get usage breakdown: %w", err)
	}
	defer rows.Close()

	var items []repository.UsageBreakdownRow
	for rows.Next() {
		var item repository.UsageBreakdownRow
		if err := rows.Scan(
			&item.Dimension,
			&item.TotalRequests,
			&item.SuccessRequests,
			&item.FailedRequests,
			&item.TotalTokens,
			&item.TotalCostUSD,
			&item.AvgLatencyMs,
		); err != nil {
			return nil, fmt.Errorf("scan usage breakdown: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage breakdown: %w", err)
	}
	return items, nil
}

func (s *Store) GetUsageTimeBuckets(ctx context.Context, filter repository.UsageFilter, period string, limit int) ([]repository.UsageTimeBucket, error) {
	if limit <= 0 {
		limit = 30
	}

	query := `
SELECT created_at, status, total_tokens, cost, latency_ms
FROM usage_records`
	where, args := usageFilterWhere(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += ` ORDER BY created_at ASC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("get usage time buckets: %w", err)
	}
	defer rows.Close()

	type bucketAgg struct {
		repository.UsageTimeBucket
		latencyCount int64
	}

	buckets := make(map[string]*bucketAgg)
	var order []string
	for rows.Next() {
		var createdAt time.Time
		var status string
		var totalTokens int64
		var cost float64
		var latencyMs int64
		if err := rows.Scan(&createdAt, &status, &totalTokens, &cost, &latencyMs); err != nil {
			return nil, fmt.Errorf("scan usage bucket row: %w", err)
		}
		key := usageBucketKey(createdAt, period)
		agg, ok := buckets[key]
		if !ok {
			agg = &bucketAgg{UsageTimeBucket: repository.UsageTimeBucket{Bucket: key}}
			buckets[key] = agg
			order = append(order, key)
		}
		agg.TotalRequests++
		if status == "success" {
			agg.SuccessRequests++
		} else {
			agg.FailedRequests++
		}
		agg.TotalTokens += totalTokens
		agg.TotalCostUSD += cost
		agg.AvgLatencyMs += float64(latencyMs)
		agg.latencyCount++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage bucket rows: %w", err)
	}

	sort.Strings(order)
	if len(order) > limit {
		order = order[len(order)-limit:]
	}
	result := make([]repository.UsageTimeBucket, 0, len(order))
	for _, key := range order {
		agg := buckets[key]
		if agg.latencyCount > 0 {
			agg.AvgLatencyMs = agg.AvgLatencyMs / float64(agg.latencyCount)
		}
		result = append(result, agg.UsageTimeBucket)
	}
	return result, nil
}

func usageFilterWhere(filter repository.UsageFilter) (string, []any) {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if filter.TenantID != "" {
		clauses = append(clauses, "tenant_id = ?")
		args = append(args, filter.TenantID)
	}
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.UserID != "" {
		clauses = append(clauses, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.APIKeyID != "" {
		clauses = append(clauses, "api_key_id = ?")
		args = append(args, filter.APIKeyID)
	}
	if filter.ProviderName != "" {
		clauses = append(clauses, "provider_name = ?")
		args = append(args, filter.ProviderName)
	}
	if filter.Model != "" {
		clauses = append(clauses, "model = ?")
		args = append(args, filter.Model)
	}
	if !filter.StartTime.IsZero() {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, filter.EndTime)
	}
	return stringsJoin(clauses, " AND "), args
}

func usageDimensionColumn(dimension string) (string, error) {
	switch dimension {
	case "provider":
		return "provider_name", nil
	case "model":
		return "model", nil
	case "project":
		return "project_id", nil
	case "user":
		return "user_id", nil
	case "key", "api_key":
		return "api_key_id", nil
	default:
		return "", fmt.Errorf("unsupported usage dimension: %s", dimension)
	}
}

func usageBucketKey(createdAt time.Time, period string) string {
	switch period {
	case "week":
		year, week := createdAt.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "month":
		return createdAt.Format("2006-01")
	default:
		return createdAt.Format("2006-01-02")
	}
}

func stringsJoin(values []string, sep string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for i := 1; i < len(values); i++ {
		result += sep + values[i]
	}
	return result
}
