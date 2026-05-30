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

	requestBody := string(record.RequestBody)
	responseBody := string(record.ResponseBody)
	routeTraceBody := string(record.RouteTraceBody)
	if err := validateJSON("request_body", requestBody); err != nil {
		return err
	}
	if err := validateJSON("response_body", responseBody); err != nil {
		return err
	}
	if err := validateJSON("route_trace_body", routeTraceBody); err != nil {
		return err
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
		requestBody,
		responseBody,
		routeTraceBody,
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

	var query string
	var args []any

	respBody := string(record.ResponseBody)
	if err := validateJSON("response_body", respBody); err != nil {
		return err
	}

	if record.RouteTraceBody != nil {
		routeBody := string(record.RouteTraceBody)
		if err := validateJSON("route_trace_body", routeBody); err != nil {
			return err
		}
		query = `
UPDATE responses
SET provider_name = ?, model = ?, status = ?, response_body = ?, route_trace_body = ?, updated_at = ?
WHERE id = ?
  AND tenant_id = ?`
		args = []any{
			record.ProviderName,
			record.Model,
			record.Status,
			respBody,
			routeBody,
			record.UpdatedAt,
			record.ID,
			record.TenantID,
		}
	} else {
		query = `
UPDATE responses
SET provider_name = ?, model = ?, status = ?, response_body = ?, updated_at = ?
WHERE id = ?
  AND tenant_id = ?`
		args = []any{
			record.ProviderName,
			record.Model,
			record.Status,
			respBody,
			record.UpdatedAt,
			record.ID,
			record.TenantID,
		}
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(query), args...); err != nil {
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

func buildResponseFilterQuery(tenantID string, filter repository.ResponseFilter) (string, []any) {
	query := `
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
	return query, args
}

func (s *Store) CountResponses(ctx context.Context, tenantID string, filter repository.ResponseFilter) (int, error) {
	baseQuery, args := buildResponseFilterQuery(tenantID, filter)
	query := "SELECT COUNT(*) " + baseQuery
	var count int
	if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count responses: %w", err)
	}
	return count, nil
}

func (s *Store) ListResponses(ctx context.Context, tenantID string, filter repository.ResponseFilter) ([]repository.ResponseRecord, error) {
	baseQuery, args := buildResponseFilterQuery(tenantID, filter)
	query := `
SELECT id, tenant_id, project_id, user_id, api_key_id, provider_name, model, status, request_body, response_body, route_trace_body, created_at, updated_at`
	query += baseQuery + ` ORDER BY created_at DESC`
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

func (s *Store) DeleteResponsesOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`DELETE FROM responses WHERE created_at < ?`), before)
	if err != nil {
		return 0, fmt.Errorf("delete old responses: %w", err)
	}
	return result.RowsAffected()
}