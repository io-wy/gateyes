package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) ListProviderRegistry(ctx context.Context) ([]repository.ProviderRegistryRecord, error) {
	rows, err := s.db.Conn.QueryContext(ctx, `
SELECT name, type, vendor, base_url, endpoint, model,
       enabled, drain, health_status, routing_weight,
       supports_chat, supports_responses, supports_messages, supports_stream,
       supports_tools, supports_images, supports_structured_output, supports_long_context,
       created_at, updated_at
FROM provider_registry
ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list provider registry: %w", err)
	}
	defer rows.Close()

	var items []repository.ProviderRegistryRecord
	for rows.Next() {
		record, err := scanProviderRegistryRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider registry: %w", err)
	}
	return items, nil
}

func (s *Store) GetProviderRegistry(ctx context.Context, name string) (*repository.ProviderRegistryRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT name, type, vendor, base_url, endpoint, model,
       enabled, drain, health_status, routing_weight,
       supports_chat, supports_responses, supports_messages, supports_stream,
       supports_tools, supports_images, supports_structured_output, supports_long_context,
       created_at, updated_at
FROM provider_registry
WHERE name = ?
LIMIT 1`), name)

	record, err := scanProviderRegistryRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get provider registry: %w", err)
	}
	return record, nil
}

func (s *Store) UpsertProviderRegistry(ctx context.Context, record repository.ProviderRegistryRecord) error {
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}

	existing, err := s.GetProviderRegistry(ctx, record.Name)
	switch err {
	case nil:
		record.CreatedAt = existing.CreatedAt
		_, err = s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE provider_registry
SET type = ?, vendor = ?, base_url = ?, endpoint = ?, model = ?,
    enabled = ?, drain = ?, health_status = ?, routing_weight = ?,
    supports_chat = ?, supports_responses = ?, supports_messages = ?, supports_stream = ?,
    supports_tools = ?, supports_images = ?, supports_structured_output = ?, supports_long_context = ?,
    updated_at = ?
WHERE name = ?`),
			record.Type,
			record.Vendor,
			record.BaseURL,
			record.Endpoint,
			record.Model,
			boolToIntFlag(record.Enabled),
			boolToIntFlag(record.Drain),
			record.HealthStatus,
			record.RoutingWeight,
			boolToIntFlag(record.SupportsChat),
			boolToIntFlag(record.SupportsResponses),
			boolToIntFlag(record.SupportsMessages),
			boolToIntFlag(record.SupportsStream),
			boolToIntFlag(record.SupportsTools),
			boolToIntFlag(record.SupportsImages),
			boolToIntFlag(record.SupportsStructuredOutput),
			boolToIntFlag(record.SupportsLongContext),
			record.UpdatedAt,
			record.Name,
		)
		if err != nil {
			return fmt.Errorf("update provider registry: %w", err)
		}
		return nil
	case repository.ErrNotFound:
		_, err = s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO provider_registry (
	name, type, vendor, base_url, endpoint, model,
	enabled, drain, health_status, routing_weight,
	supports_chat, supports_responses, supports_messages, supports_stream,
	supports_tools, supports_images, supports_structured_output, supports_long_context,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			record.Name,
			record.Type,
			record.Vendor,
			record.BaseURL,
			record.Endpoint,
			record.Model,
			boolToIntFlag(record.Enabled),
			boolToIntFlag(record.Drain),
			record.HealthStatus,
			record.RoutingWeight,
			boolToIntFlag(record.SupportsChat),
			boolToIntFlag(record.SupportsResponses),
			boolToIntFlag(record.SupportsMessages),
			boolToIntFlag(record.SupportsStream),
			boolToIntFlag(record.SupportsTools),
			boolToIntFlag(record.SupportsImages),
			boolToIntFlag(record.SupportsStructuredOutput),
			boolToIntFlag(record.SupportsLongContext),
			record.CreatedAt,
			record.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert provider registry: %w", err)
		}
		return nil
	default:
		return err
	}
}

func (s *Store) UpdateProviderRegistry(ctx context.Context, name string, params repository.UpdateProviderRegistryParams) (*repository.ProviderRegistryRecord, error) {
	current, err := s.GetProviderRegistry(ctx, name)
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 12)
	args := make([]any, 0, 13)
	if params.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToIntFlag(*params.Enabled))
	}
	if params.Drain != nil {
		sets = append(sets, "drain = ?")
		args = append(args, boolToIntFlag(*params.Drain))
	}
	if params.HealthStatus != nil {
		sets = append(sets, "health_status = ?")
		args = append(args, strings.TrimSpace(*params.HealthStatus))
	}
	if params.RoutingWeight != nil {
		sets = append(sets, "routing_weight = ?")
		args = append(args, *params.RoutingWeight)
	}
	if params.SupportsChat != nil {
		sets = append(sets, "supports_chat = ?")
		args = append(args, boolToIntFlag(*params.SupportsChat))
	}
	if params.SupportsResponses != nil {
		sets = append(sets, "supports_responses = ?")
		args = append(args, boolToIntFlag(*params.SupportsResponses))
	}
	if params.SupportsMessages != nil {
		sets = append(sets, "supports_messages = ?")
		args = append(args, boolToIntFlag(*params.SupportsMessages))
	}
	if params.SupportsStream != nil {
		sets = append(sets, "supports_stream = ?")
		args = append(args, boolToIntFlag(*params.SupportsStream))
	}
	if params.SupportsTools != nil {
		sets = append(sets, "supports_tools = ?")
		args = append(args, boolToIntFlag(*params.SupportsTools))
	}
	if params.SupportsImages != nil {
		sets = append(sets, "supports_images = ?")
		args = append(args, boolToIntFlag(*params.SupportsImages))
	}
	if params.SupportsStructuredOutput != nil {
		sets = append(sets, "supports_structured_output = ?")
		args = append(args, boolToIntFlag(*params.SupportsStructuredOutput))
	}
	if params.SupportsLongContext != nil {
		sets = append(sets, "supports_long_context = ?")
		args = append(args, boolToIntFlag(*params.SupportsLongContext))
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), current.Name)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE provider_registry
SET %s
WHERE name = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update provider registry: %w", err)
	}
	return s.GetProviderRegistry(ctx, name)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProviderRegistryRecord(scanner rowScanner) (*repository.ProviderRegistryRecord, error) {
	var (
		record              repository.ProviderRegistryRecord
		enabled             int
		drain               int
		supportsChat        int
		supportsResponses   int
		supportsMessages    int
		supportsStream      int
		supportsTools       int
		supportsImages      int
		supportsStructured  int
		supportsLongContext int
	)
	if err := scanner.Scan(
		&record.Name,
		&record.Type,
		&record.Vendor,
		&record.BaseURL,
		&record.Endpoint,
		&record.Model,
		&enabled,
		&drain,
		&record.HealthStatus,
		&record.RoutingWeight,
		&supportsChat,
		&supportsResponses,
		&supportsMessages,
		&supportsStream,
		&supportsTools,
		&supportsImages,
		&supportsStructured,
		&supportsLongContext,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.Enabled = enabled == 1
	record.Drain = drain == 1
	record.SupportsChat = supportsChat == 1
	record.SupportsResponses = supportsResponses == 1
	record.SupportsMessages = supportsMessages == 1
	record.SupportsStream = supportsStream == 1
	record.SupportsTools = supportsTools == 1
	record.SupportsImages = supportsImages == 1
	record.SupportsStructuredOutput = supportsStructured == 1
	record.SupportsLongContext = supportsLongContext == 1
	return &record, nil
}

func boolToIntFlag(value bool) int {
	if value {
		return 1
	}
	return 0
}
