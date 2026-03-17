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
	id, tenant_id, user_id, api_key_id, provider_name, model, status,
	request_body, response_body, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.TenantID,
		record.UserID,
		record.APIKeyID,
		record.ProviderName,
		record.Model,
		record.Status,
		string(record.RequestBody),
		string(record.ResponseBody),
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

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE responses
SET provider_name = ?, model = ?, status = ?, response_body = ?, updated_at = ?
WHERE id = ?
  AND tenant_id = ?`),
		record.ProviderName,
		record.Model,
		record.Status,
		string(record.ResponseBody),
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
SELECT id, tenant_id, user_id, api_key_id, provider_name, model, status, request_body, response_body, created_at, updated_at
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
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.UserID,
		&record.APIKeyID,
		&record.ProviderName,
		&record.Model,
		&record.Status,
		&requestBody,
		&responseBody,
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
	return &record, nil
}
