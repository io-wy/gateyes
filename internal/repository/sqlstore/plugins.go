package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreatePlugin(ctx context.Context, params repository.CreatePluginParams) (*repository.PluginRecord, error) {
	if params.TenantID != "" {
		if _, err := s.loadTenant(ctx, params.TenantID); err != nil {
			return nil, err
		}
	}

	phasesBody, err := encodeJSON(params.Phases)
	if err != nil {
		return nil, err
	}
	configBody, err := encodeJSON(params.Config)
	if err != nil {
		return nil, err
	}
	if err := validateJSON("phases", phasesBody); err != nil {
		return nil, err
	}
	if err := validateJSON("config_body", configBody); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	record := repository.PluginRecord{
		ID:          uuid.NewString(),
		TenantID:    params.TenantID,
		Name:        strings.TrimSpace(params.Name),
		Type:        strings.ToLower(strings.TrimSpace(params.Type)),
		Description: strings.TrimSpace(params.Description),
		Author:      strings.TrimSpace(params.Author),
		Phases:      normalizePhases(params.Phases),
		FilePath:    params.FilePath,
		Address:     strings.TrimSpace(params.Address),
		TimeoutMs:   params.TimeoutMs,
		MemoryPages: params.MemoryPages,
		Enabled:     params.Enabled,
		Source:      strings.ToLower(strings.TrimSpace(params.Source)),
		Config:      params.Config,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if record.TimeoutMs <= 0 {
		record.TimeoutMs = 50
	}
	if record.MemoryPages <= 0 {
		record.MemoryPages = 1
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO plugins (
	id, tenant_id, name, type, description, author, phases, file_path, address,
	timeout_ms, memory_pages, enabled, source, config_body, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.TenantID,
		record.Name,
		record.Type,
		record.Description,
		record.Author,
		phasesBody,
		record.FilePath,
		record.Address,
		record.TimeoutMs,
		record.MemoryPages,
		boolToInt(record.Enabled),
		record.Source,
		configBody,
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create plugin: %w", err)
	}

	return s.GetPlugin(ctx, record.TenantID, record.ID)
}

func (s *Store) ListPlugins(ctx context.Context, tenantID string, filter repository.PluginFilter) ([]repository.PluginRecord, error) {
	query := `
SELECT id, tenant_id, name, type, description, author, phases, file_path, address,
	timeout_ms, memory_pages, enabled, source, config_body, created_at, updated_at
FROM plugins
WHERE tenant_id = ?`
	args := []any{tenantID}

	if filter.Type != "" {
		query += ` AND type = ?`
		args = append(args, strings.ToLower(filter.Type))
	}
	if filter.Enabled != nil {
		query += ` AND enabled = ?`
		args = append(args, boolToInt(*filter.Enabled))
	}
	if filter.Source != "" {
		query += ` AND source = ?`
		args = append(args, filter.Source)
	}
	query += ` ORDER BY created_at ASC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	defer rows.Close()

	var items []repository.PluginRecord
	for rows.Next() {
		record, err := scanPluginRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plugins: %w", err)
	}
	return items, nil
}

func (s *Store) GetPlugin(ctx context.Context, tenantID string, id string) (*repository.PluginRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT id, tenant_id, name, type, description, author, phases, file_path, address,
	timeout_ms, memory_pages, enabled, source, config_body, created_at, updated_at
FROM plugins
WHERE id = ? AND tenant_id = ?
LIMIT 1`), id, tenantID)
	record, err := scanPluginRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get plugin: %w", err)
	}
	return record, nil
}

func (s *Store) UpdatePlugin(ctx context.Context, tenantID string, id string, params repository.UpdatePluginParams) (*repository.PluginRecord, error) {
	record, err := s.GetPlugin(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 10)
	args := make([]any, 0, 11)
	if params.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*params.Name))
	}
	if params.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, strings.TrimSpace(*params.Description))
	}
	if params.Author != nil {
		sets = append(sets, "author = ?")
		args = append(args, strings.TrimSpace(*params.Author))
	}
	if params.Phases != nil {
		phasesBody, err := encodeJSON(normalizePhases(*params.Phases))
		if err != nil {
			return nil, err
		}
		if err := validateJSON("phases", phasesBody); err != nil {
			return nil, err
		}
		sets = append(sets, "phases = ?")
		args = append(args, phasesBody)
	}
	if params.FilePath != nil {
		sets = append(sets, "file_path = ?")
		args = append(args, *params.FilePath)
	}
	if params.Address != nil {
		sets = append(sets, "address = ?")
		args = append(args, strings.TrimSpace(*params.Address))
	}
	if params.TimeoutMs != nil {
		sets = append(sets, "timeout_ms = ?")
		args = append(args, *params.TimeoutMs)
	}
	if params.MemoryPages != nil {
		sets = append(sets, "memory_pages = ?")
		args = append(args, *params.MemoryPages)
	}
	if params.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*params.Enabled))
	}
	if params.Source != nil {
		sets = append(sets, "source = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(*params.Source)))
	}
	if params.Config != nil {
		configBody, err := encodeJSON(*params.Config)
		if err != nil {
			return nil, err
		}
		if err := validateJSON("config_body", configBody); err != nil {
			return nil, err
		}
		sets = append(sets, "config_body = ?")
		args = append(args, configBody)
	}
	if len(sets) == 0 {
		return record, nil
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), record.ID)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE plugins
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update plugin: %w", err)
	}
	return s.GetPlugin(ctx, record.TenantID, record.ID)
}

func (s *Store) DeletePlugin(ctx context.Context, tenantID string, id string) error {
	record, err := s.GetPlugin(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
DELETE FROM plugins
WHERE id = ? AND tenant_id = ?`), record.ID, record.TenantID); err != nil {
		return fmt.Errorf("delete plugin: %w", err)
	}
	return nil
}

func scanPluginRecord(scanner rowScanner) (*repository.PluginRecord, error) {
	record := &repository.PluginRecord{}
	var enabled int
	var phasesBody string
	var configBody string
	if err := scanner.Scan(
		&record.ID,
		&record.TenantID,
		&record.Name,
		&record.Type,
		&record.Description,
		&record.Author,
		&phasesBody,
		&record.FilePath,
		&record.Address,
		&record.TimeoutMs,
		&record.MemoryPages,
		&enabled,
		&record.Source,
		&configBody,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.Enabled = enabled == 1
	record.Phases = []string{}
	if phasesBody != "" {
		if err := json.Unmarshal([]byte(phasesBody), &record.Phases); err != nil {
			return nil, fmt.Errorf("decode plugin phases: %w", err)
		}
	}
	record.Config = map[string]any{}
	if configBody != "" {
		if err := json.Unmarshal([]byte(configBody), &record.Config); err != nil {
			return nil, fmt.Errorf("decode plugin config: %w", err)
		}
	}
	return record, nil
}

func normalizePhases(phases []string) []string {
	if len(phases) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(phases))
	result := make([]string, 0, len(phases))
	for _, phase := range phases {
		value := strings.ToLower(strings.TrimSpace(phase))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
