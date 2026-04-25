package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateResponse(ctx context.Context, record repository.ResponseRecord) error {
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO responses (
	id, tenant_id, project_id, user_id, api_key_id, provider_name, model, status,
	request_body, response_body, route_trace_body, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.TenantID,
		record.ProjectID,
		record.UserID,
		record.APIKeyID,
		record.ProviderName,
		record.Model,
		record.Status,
		string(record.RequestBody),
		string(record.ResponseBody),
		string(record.RouteTraceBody),
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create response: %w", err)
	}

	return nil
}

func (s *Store) UpdateResponse(ctx context.Context, record repository.ResponseRecord) error {
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	var routeTraceValue any
	if record.RouteTraceBody != nil {
		routeTraceValue = string(record.RouteTraceBody)
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE responses
SET provider_name = ?, model = ?, status = ?, response_body = ?, route_trace_body = CASE WHEN ? IS NULL THEN route_trace_body ELSE ? END, updated_at = ?
WHERE id = ?
  AND tenant_id = ?`),
		record.ProviderName,
		record.Model,
		record.Status,
		string(record.ResponseBody),
		routeTraceValue,
		routeTraceValue,
		record.UpdatedAt,
		record.ID,
		record.TenantID,
	); err != nil {
		return fmt.Errorf("update response: %w", err)
	}

	return nil
}

func (s *Store) GetResponse(ctx context.Context, tenantID string, id string) (*repository.ResponseRecord, error) {
	query := `
SELECT id, tenant_id, project_id, user_id, api_key_id, provider_name, model, status, request_body, response_body, route_trace_body, created_at, updated_at
FROM responses
WHERE id = ?`
	args := []any{id}
	if tenantID != "" {
		query += `
  AND tenant_id = ?`
		args = append(args, tenantID)
	}
	query += `
LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	var record repository.ResponseRecord
	var requestBody string
	var responseBody string
	var routeTraceBody string
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.ProjectID,
		&record.UserID,
		&record.APIKeyID,
		&record.ProviderName,
		&record.Model,
		&record.Status,
		&requestBody,
		&responseBody,
		&routeTraceBody,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get response: %w", err)
	}
	record.RequestBody = []byte(requestBody)
	record.ResponseBody = []byte(responseBody)
	record.RouteTraceBody = []byte(routeTraceBody)
	return &record, nil
}

func (s *Store) ListResponses(ctx context.Context, tenantID string, filter repository.ResponseFilter) ([]repository.ResponseRecord, error) {
	query := `
SELECT id, tenant_id, project_id, user_id, api_key_id, provider_name, model, status, request_body, response_body, route_trace_body, created_at, updated_at
FROM responses
WHERE 1 = 1`
	args := make([]any, 0, 8)
	if tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	if filter.ProviderName != "" {
		query += ` AND provider_name = ?`
		args = append(args, filter.ProviderName)
	}
	if filter.Model != "" {
		query += ` AND model = ?`
		args = append(args, filter.Model)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.ProjectID != "" {
		query += ` AND project_id = ?`
		args = append(args, filter.ProjectID)
	}
	if filter.Query != "" {
		query += ` AND (request_body LIKE ? OR response_body LIKE ?)`
		pattern := "%" + filter.Query + "%"
		args = append(args, pattern, pattern)
	}
	if filter.APIKeyID != "" {
		query += ` AND api_key_id = ?`
		args = append(args, filter.APIKeyID)
	}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if !filter.StartTime.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += ` AND created_at <= ?`
		args = append(args, filter.EndTime)
	}
	query += ` ORDER BY created_at DESC`
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += ` LIMIT ?`
	args = append(args, limit)
	if filter.Offset > 0 {
		query += ` OFFSET ?`
		args = append(args, filter.Offset)
	}

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list responses: %w", err)
	}
	defer rows.Close()

	var items []repository.ResponseRecord
	for rows.Next() {
		var record repository.ResponseRecord
		var requestBody string
		var responseBody string
		var routeTraceBody string
		if err := rows.Scan(
			&record.ID,
			&record.TenantID,
			&record.ProjectID,
			&record.UserID,
			&record.APIKeyID,
			&record.ProviderName,
			&record.Model,
			&record.Status,
			&requestBody,
			&responseBody,
			&routeTraceBody,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan response: %w", err)
		}
		record.RequestBody = []byte(requestBody)
		record.ResponseBody = []byte(responseBody)
		record.RouteTraceBody = []byte(routeTraceBody)
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate responses: %w", err)
	}
	return items, nil
}
