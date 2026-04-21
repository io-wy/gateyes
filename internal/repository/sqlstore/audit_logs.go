package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateAuditLog(ctx context.Context, record repository.AuditLogRecord) error {
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO audit_logs (
	id, tenant_id, actor_user_id, actor_api_key_id, actor_role, action, resource_type, resource_id,
	request_id, ip_address, payload, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.TenantID,
		record.ActorUserID,
		record.ActorAPIKeyID,
		record.ActorRole,
		record.Action,
		record.ResourceType,
		record.ResourceID,
		record.RequestID,
		record.IPAddress,
		string(record.Payload),
		record.CreatedAt,
	); err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

func (s *Store) ListAuditLogs(ctx context.Context, tenantID string, filter repository.AuditLogFilter) ([]repository.AuditLogRecord, error) {
	query := `
SELECT id, tenant_id, actor_user_id, actor_api_key_id, actor_role, action, resource_type, resource_id,
	request_id, ip_address, payload, created_at
FROM audit_logs
WHERE 1 = 1`
	args := make([]any, 0, 8)
	if tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	if filter.Action != "" {
		query += ` AND action = ?`
		args = append(args, filter.Action)
	}
	if filter.ResourceType != "" {
		query += ` AND resource_type = ?`
		args = append(args, filter.ResourceType)
	}
	if filter.ResourceID != "" {
		query += ` AND resource_id = ?`
		args = append(args, filter.ResourceID)
	}
	if filter.ActorUserID != "" {
		query += ` AND actor_user_id = ?`
		args = append(args, filter.ActorUserID)
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

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	var items []repository.AuditLogRecord
	for rows.Next() {
		var item repository.AuditLogRecord
		var payload string
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.ActorUserID,
			&item.ActorAPIKeyID,
			&item.ActorRole,
			&item.Action,
			&item.ResourceType,
			&item.ResourceID,
			&item.RequestID,
			&item.IPAddress,
			&payload,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		item.Payload = []byte(payload)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit logs: %w", err)
	}
	return items, nil
}

func normalizeAuditAction(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
