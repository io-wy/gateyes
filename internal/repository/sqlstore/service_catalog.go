package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateService(ctx context.Context, params repository.CreateServiceParams) (*repository.ServiceRecord, error) {
	tenant, err := s.loadTenant(ctx, params.TenantID)
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID != "" {
		if _, err := s.loadProject(ctx, tenant.ID, projectID); err != nil {
			return nil, err
		}
	}

	configBody, err := encodeJSON(normalizeServiceConfig(params.Config))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := repository.ServiceRecord{
		ID:              uuid.NewString(),
		TenantID:        tenant.ID,
		ProjectID:       projectID,
		Name:            strings.TrimSpace(params.Name),
		RequestPrefix:   normalizeRequestPrefix(params.RequestPrefix),
		Description:     strings.TrimSpace(params.Description),
		DefaultProvider: strings.TrimSpace(params.DefaultProvider),
		DefaultModel:    strings.TrimSpace(params.DefaultModel),
		PublishStatus:   "draft",
		Enabled:         params.Enabled,
		Config:          normalizeServiceConfig(params.Config),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO services (
	id, tenant_id, project_id, name, request_prefix, description, default_provider, default_model,
	publish_status, published_version_id, staged_version_id, enabled, config_body, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, ?)`),
		record.ID,
		record.TenantID,
		record.ProjectID,
		record.Name,
		record.RequestPrefix,
		record.Description,
		record.DefaultProvider,
		record.DefaultModel,
		record.PublishStatus,
		boolToInt(record.Enabled),
		configBody,
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}

	return s.GetService(ctx, tenant.ID, record.ID)
}

func (s *Store) ListServices(ctx context.Context, tenantID string, filter repository.ServiceFilter) ([]repository.ServiceRecord, error) {
	query := `
SELECT sv.id, sv.tenant_id, COALESCE(sv.project_id, ''), COALESCE(p.slug, ''), sv.name, sv.request_prefix,
	sv.description, sv.default_provider, sv.default_model, sv.publish_status, sv.published_version_id,
	sv.staged_version_id, sv.enabled, sv.config_body, sv.created_at, sv.updated_at
FROM services sv
LEFT JOIN projects p ON p.id = sv.project_id
WHERE 1 = 1`

	args := make([]any, 0, 4)
	if tenantID != "" {
		query += ` AND sv.tenant_id = ?`
		args = append(args, tenantID)
	}
	if filter.ProjectID != "" {
		query += ` AND sv.project_id = ?`
		args = append(args, filter.ProjectID)
	}
	if filter.PublishStatus != "" {
		query += ` AND sv.publish_status = ?`
		args = append(args, filter.PublishStatus)
	}
	if filter.Enabled != nil {
		query += ` AND sv.enabled = ?`
		args = append(args, boolToInt(*filter.Enabled))
	}
	query += ` ORDER BY sv.created_at ASC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()

	var items []repository.ServiceRecord
	for rows.Next() {
		record, err := scanServiceRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate services: %w", err)
	}
	return items, nil
}

func (s *Store) GetService(ctx context.Context, tenantID string, idOrPrefix string) (*repository.ServiceRecord, error) {
	return s.loadService(ctx, tenantID, idOrPrefix)
}

func (s *Store) GetServiceByPrefix(ctx context.Context, tenantID string, prefix string) (*repository.ServiceRecord, error) {
	return s.loadServiceByPrefix(ctx, tenantID, prefix)
}

func (s *Store) UpdateService(ctx context.Context, tenantID string, idOrPrefix string, params repository.UpdateServiceParams) (*repository.ServiceRecord, error) {
	record, err := s.loadService(ctx, tenantID, idOrPrefix)
	if err != nil {
		return nil, err
	}
	if params.ProjectID != nil && *params.ProjectID != "" {
		if _, err := s.loadProject(ctx, record.TenantID, *params.ProjectID); err != nil {
			return nil, err
		}
	}

	sets := make([]string, 0, 8)
	args := make([]any, 0, 9)
	if params.ProjectID != nil {
		sets = append(sets, "project_id = ?")
		args = append(args, strings.TrimSpace(*params.ProjectID))
	}
	if params.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*params.Name))
	}
	if params.RequestPrefix != nil {
		sets = append(sets, "request_prefix = ?")
		args = append(args, normalizeRequestPrefix(*params.RequestPrefix))
	}
	if params.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, strings.TrimSpace(*params.Description))
	}
	if params.DefaultProvider != nil {
		sets = append(sets, "default_provider = ?")
		args = append(args, strings.TrimSpace(*params.DefaultProvider))
	}
	if params.DefaultModel != nil {
		sets = append(sets, "default_model = ?")
		args = append(args, strings.TrimSpace(*params.DefaultModel))
	}
	if params.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*params.Enabled))
	}
	if params.Config != nil {
		configBody, err := encodeJSON(normalizeServiceConfig(*params.Config))
		if err != nil {
			return nil, err
		}
		sets = append(sets, "config_body = ?")
		args = append(args, configBody)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), record.ID)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE services
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update service: %w", err)
	}
	return s.GetService(ctx, record.TenantID, record.ID)
}

func (s *Store) CreateServiceVersion(ctx context.Context, tenantID string, params repository.CreateServiceVersionParams) (*repository.ServiceVersionRecord, error) {
	service, err := s.loadService(ctx, tenantID, params.ServiceID)
	if err != nil {
		return nil, err
	}

	snapshot := params.Snapshot
	if snapshot.RequestPrefix == "" {
		snapshot = serviceSnapshotFromRecord(*service)
	}
	body, err := encodeJSON(snapshot)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create service version: %w", err)
	}

	var nextVersion int
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`
SELECT COALESCE(MAX(version), 0) + 1
FROM service_versions
WHERE service_id = ?`), service.ID).Scan(&nextVersion); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("next service version: %w", err)
	}

	now := time.Now().UTC()
	record := repository.ServiceVersionRecord{
		ID:        uuid.NewString(),
		ServiceID: service.ID,
		TenantID:  service.TenantID,
		Version:   nextVersion,
		Status:    "draft",
		Snapshot:  snapshot,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO service_versions (id, service_id, tenant_id, version, status, snapshot_body, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.ServiceID,
		record.TenantID,
		record.Version,
		record.Status,
		body,
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert service version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit service version: %w", err)
	}
	return s.GetServiceVersion(ctx, service.TenantID, service.ID, record.ID)
}

func (s *Store) ListServiceVersions(ctx context.Context, tenantID string, serviceID string) ([]repository.ServiceVersionRecord, error) {
	service, err := s.loadService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT id, service_id, tenant_id, version, status, snapshot_body, created_at, updated_at
FROM service_versions
WHERE service_id = ?
ORDER BY version DESC`), service.ID)
	if err != nil {
		return nil, fmt.Errorf("list service versions: %w", err)
	}
	defer rows.Close()

	var items []repository.ServiceVersionRecord
	for rows.Next() {
		record, err := scanServiceVersionRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service versions: %w", err)
	}
	return items, nil
}

func (s *Store) GetServiceVersion(ctx context.Context, tenantID string, serviceID string, versionOrID string) (*repository.ServiceVersionRecord, error) {
	service, err := s.loadService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}
	return s.loadServiceVersion(ctx, service.TenantID, service.ID, versionOrID)
}

func (s *Store) PublishServiceVersion(ctx context.Context, tenantID string, serviceID string, params repository.PublishServiceVersionParams) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	service, version, err := s.transitionServiceVersion(ctx, tenantID, serviceID, params.VersionID, strings.ToLower(strings.TrimSpace(params.Mode)))
	if err != nil {
		return nil, nil, err
	}
	return service, version, nil
}

func (s *Store) PromoteStagedServiceVersion(ctx context.Context, tenantID string, serviceID string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	service, err := s.loadService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, nil, err
	}
	if service.StagedVersionID == "" {
		return nil, nil, repository.ErrNotFound
	}
	return s.transitionServiceVersion(ctx, tenantID, service.ID, service.StagedVersionID, "published")
}

func (s *Store) RollbackServiceVersion(ctx context.Context, tenantID string, serviceID string, params repository.RollbackServiceVersionParams) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return s.transitionServiceVersion(ctx, tenantID, serviceID, params.VersionID, "published")
}

func (s *Store) CreateServiceSubscription(ctx context.Context, tenantID string, params repository.CreateServiceSubscriptionParams) (*repository.ServiceSubscriptionRecord, error) {
	service, err := s.loadService(ctx, tenantID, params.ServiceID)
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID != "" {
		if _, err := s.loadProject(ctx, service.TenantID, projectID); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	record := repository.ServiceSubscriptionRecord{
		ID:                    uuid.NewString(),
		TenantID:              service.TenantID,
		ServiceID:             service.ID,
		ProjectID:             projectID,
		ConsumerName:          strings.TrimSpace(params.ConsumerName),
		ConsumerEmail:         strings.TrimSpace(params.ConsumerEmail),
		ConsumerUserID:        strings.TrimSpace(params.ConsumerUserID),
		Status:                "pending",
		RequestedBudgetUSD:    params.RequestedBudgetUSD,
		RequestedRateLimitQPS: params.RequestedRateLimitQPS,
		AllowedSurfaces:       params.AllowedSurfaces,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO service_subscriptions (
	id, tenant_id, service_id, project_id, consumer_name, consumer_email, consumer_user_id, status,
	requested_budget_usd, requested_rate_limit_qps, allowed_surfaces, approved_api_key_id, approved_user_id,
	review_note, approved_at, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '', NULL, ?, ?)`),
		record.ID,
		record.TenantID,
		record.ServiceID,
		record.ProjectID,
		record.ConsumerName,
		record.ConsumerEmail,
		record.ConsumerUserID,
		record.Status,
		record.RequestedBudgetUSD,
		record.RequestedRateLimitQPS,
		encodeStringSlice(record.AllowedSurfaces),
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create service subscription: %w", err)
	}
	return s.GetServiceSubscription(ctx, service.TenantID, record.ID)
}

func (s *Store) ListServiceSubscriptions(ctx context.Context, tenantID string, filter repository.ServiceSubscriptionFilter) ([]repository.ServiceSubscriptionRecord, error) {
	query := `
SELECT ss.id, ss.tenant_id, ss.service_id, COALESCE(ss.project_id, ''), COALESCE(p.slug, ''), ss.consumer_name,
	ss.consumer_email, ss.consumer_user_id, ss.status, ss.requested_budget_usd, ss.requested_rate_limit_qps,
	ss.allowed_surfaces, ss.approved_api_key_id, ss.approved_user_id, ss.review_note, ss.approved_at, ss.created_at, ss.updated_at
FROM service_subscriptions ss
LEFT JOIN projects p ON p.id = ss.project_id
WHERE 1 = 1`
	args := make([]any, 0, 4)
	if tenantID != "" {
		query += ` AND ss.tenant_id = ?`
		args = append(args, tenantID)
	}
	if filter.ServiceID != "" {
		query += ` AND ss.service_id = ?`
		args = append(args, filter.ServiceID)
	}
	if filter.ProjectID != "" {
		query += ` AND ss.project_id = ?`
		args = append(args, filter.ProjectID)
	}
	if filter.Status != "" {
		query += ` AND ss.status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY ss.created_at DESC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list service subscriptions: %w", err)
	}
	defer rows.Close()

	var items []repository.ServiceSubscriptionRecord
	for rows.Next() {
		record, err := scanServiceSubscriptionRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service subscriptions: %w", err)
	}
	return items, nil
}

func (s *Store) GetServiceSubscription(ctx context.Context, tenantID string, id string) (*repository.ServiceSubscriptionRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT ss.id, ss.tenant_id, ss.service_id, COALESCE(ss.project_id, ''), COALESCE(p.slug, ''), ss.consumer_name,
	ss.consumer_email, ss.consumer_user_id, ss.status, ss.requested_budget_usd, ss.requested_rate_limit_qps,
	ss.allowed_surfaces, ss.approved_api_key_id, ss.approved_user_id, ss.review_note, ss.approved_at, ss.created_at, ss.updated_at
FROM service_subscriptions ss
LEFT JOIN projects p ON p.id = ss.project_id
WHERE ss.id = ?
  AND (? = '' OR ss.tenant_id = ?)
LIMIT 1`), id, tenantID, tenantID)
	record, err := scanServiceSubscriptionRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get service subscription: %w", err)
	}
	return record, nil
}

func (s *Store) UpdateServiceSubscription(ctx context.Context, tenantID string, id string, params repository.UpdateServiceSubscriptionParams) (*repository.ServiceSubscriptionRecord, error) {
	record, err := s.GetServiceSubscription(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)
	if params.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *params.Status)
		if *params.Status == "approved" {
			sets = append(sets, "approved_at = ?")
			args = append(args, time.Now().UTC())
		}
	}
	if params.ReviewNote != nil {
		sets = append(sets, "review_note = ?")
		args = append(args, *params.ReviewNote)
	}
	if params.ApprovedAPIKeyID != nil {
		sets = append(sets, "approved_api_key_id = ?")
		args = append(args, *params.ApprovedAPIKeyID)
	}
	if params.ApprovedUserID != nil {
		sets = append(sets, "approved_user_id = ?")
		args = append(args, *params.ApprovedUserID)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), record.ID)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE service_subscriptions
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update service subscription: %w", err)
	}
	return s.GetServiceSubscription(ctx, record.TenantID, record.ID)
}

func (s *Store) transitionServiceVersion(ctx context.Context, tenantID string, serviceID string, versionID string, mode string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	service, err := s.loadService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, nil, err
	}
	version, err := s.loadServiceVersion(ctx, service.TenantID, service.ID, versionID)
	if err != nil {
		return nil, nil, err
	}
	if mode == "" || mode == "active" {
		mode = "published"
	}
	if mode == "stage" {
		mode = "staged"
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin transition service version: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE service_versions
SET status = CASE
	WHEN id = ? THEN ?
	WHEN service_id = ? AND status IN ('published', 'staged') THEN 'archived'
	ELSE status
END,
updated_at = ?
WHERE service_id = ?`), version.ID, mode, service.ID, time.Now().UTC(), service.ID); err != nil {
		tx.Rollback()
		return nil, nil, fmt.Errorf("update service version statuses: %w", err)
	}

	publishedID := service.PublishedVersionID
	stagedID := ""
	publishStatus := ""
	switch mode {
	case "staged":
		stagedID = version.ID
		publishStatus = "staged"
	case "published":
		publishedID = version.ID
		stagedID = ""
		publishStatus = "published"
	default:
		tx.Rollback()
		return nil, nil, fmt.Errorf("unsupported publish mode: %s", mode)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE services
SET published_version_id = ?, staged_version_id = ?, publish_status = ?, updated_at = ?
WHERE id = ?`), publishedID, stagedID, publishStatus, time.Now().UTC(), service.ID); err != nil {
		tx.Rollback()
		return nil, nil, fmt.Errorf("update service publish state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit service version transition: %w", err)
	}

	updatedService, err := s.GetService(ctx, service.TenantID, service.ID)
	if err != nil {
		return nil, nil, err
	}
	updatedVersion, err := s.GetServiceVersion(ctx, service.TenantID, service.ID, version.ID)
	if err != nil {
		return nil, nil, err
	}
	return updatedService, updatedVersion, nil
}

func (s *Store) loadService(ctx context.Context, tenantID string, idOrPrefix string) (*repository.ServiceRecord, error) {
	query := `
SELECT sv.id, sv.tenant_id, COALESCE(sv.project_id, ''), COALESCE(p.slug, ''), sv.name, sv.request_prefix,
	sv.description, sv.default_provider, sv.default_model, sv.publish_status, sv.published_version_id,
	sv.staged_version_id, sv.enabled, sv.config_body, sv.created_at, sv.updated_at
FROM services sv
LEFT JOIN projects p ON p.id = sv.project_id
WHERE (sv.id = ? OR sv.request_prefix = ?)`
	args := []any{idOrPrefix, normalizeRequestPrefix(idOrPrefix)}
	if tenantID != "" {
		query += ` AND sv.tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	record, err := scanServiceRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load service: %w", err)
	}
	return record, nil
}

func (s *Store) loadServiceByPrefix(ctx context.Context, tenantID string, prefix string) (*repository.ServiceRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT sv.id, sv.tenant_id, COALESCE(sv.project_id, ''), COALESCE(p.slug, ''), sv.name, sv.request_prefix,
	sv.description, sv.default_provider, sv.default_model, sv.publish_status, sv.published_version_id,
	sv.staged_version_id, sv.enabled, sv.config_body, sv.created_at, sv.updated_at
FROM services sv
LEFT JOIN projects p ON p.id = sv.project_id
WHERE sv.tenant_id = ?
  AND sv.request_prefix = ?
LIMIT 1`), tenantID, normalizeRequestPrefix(prefix))
	record, err := scanServiceRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load service by prefix: %w", err)
	}
	return record, nil
}

func (s *Store) loadServiceVersion(ctx context.Context, tenantID string, serviceID string, versionOrID string) (*repository.ServiceVersionRecord, error) {
	query := `
SELECT id, service_id, tenant_id, version, status, snapshot_body, created_at, updated_at
FROM service_versions
WHERE service_id = ?`
	args := []any{serviceID}
	if tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	if versionNumber, err := strconv.Atoi(strings.TrimSpace(versionOrID)); err == nil {
		query += ` AND version = ?`
		args = append(args, versionNumber)
	} else {
		query += ` AND id = ?`
		args = append(args, versionOrID)
	}
	query += ` LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	record, err := scanServiceVersionRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load service version: %w", err)
	}
	return record, nil
}

func scanServiceRecord(scanner rowScanner) (*repository.ServiceRecord, error) {
	record := &repository.ServiceRecord{}
	var enabled int
	var configBody string
	if err := scanner.Scan(
		&record.ID,
		&record.TenantID,
		&record.ProjectID,
		&record.ProjectSlug,
		&record.Name,
		&record.RequestPrefix,
		&record.Description,
		&record.DefaultProvider,
		&record.DefaultModel,
		&record.PublishStatus,
		&record.PublishedVersionID,
		&record.StagedVersionID,
		&enabled,
		&configBody,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.Enabled = enabled == 1
	record.Config = repository.ServiceConfig{}
	if configBody != "" {
		if err := json.Unmarshal([]byte(configBody), &record.Config); err != nil {
			return nil, fmt.Errorf("decode service config: %w", err)
		}
	}
	record.Config = normalizeServiceConfig(record.Config)
	return record, nil
}

func scanServiceVersionRecord(scanner rowScanner) (*repository.ServiceVersionRecord, error) {
	record := &repository.ServiceVersionRecord{}
	var snapshotBody string
	if err := scanner.Scan(
		&record.ID,
		&record.ServiceID,
		&record.TenantID,
		&record.Version,
		&record.Status,
		&snapshotBody,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if snapshotBody != "" {
		if err := json.Unmarshal([]byte(snapshotBody), &record.Snapshot); err != nil {
			return nil, fmt.Errorf("decode service snapshot: %w", err)
		}
		record.Snapshot.Config = normalizeServiceConfig(record.Snapshot.Config)
	}
	return record, nil
}

func scanServiceSubscriptionRecord(scanner rowScanner) (*repository.ServiceSubscriptionRecord, error) {
	record := &repository.ServiceSubscriptionRecord{}
	var allowedSurfacesRaw string
	var approvedAt sql.NullTime
	if err := scanner.Scan(
		&record.ID,
		&record.TenantID,
		&record.ServiceID,
		&record.ProjectID,
		&record.ProjectSlug,
		&record.ConsumerName,
		&record.ConsumerEmail,
		&record.ConsumerUserID,
		&record.Status,
		&record.RequestedBudgetUSD,
		&record.RequestedRateLimitQPS,
		&allowedSurfacesRaw,
		&record.ApprovedAPIKeyID,
		&record.ApprovedUserID,
		&record.ReviewNote,
		&approvedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.AllowedSurfaces = decodeStringSlice(allowedSurfacesRaw)
	if approvedAt.Valid {
		value := approvedAt.Time
		record.ApprovedAt = &value
	}
	return record, nil
}

func serviceSnapshotFromRecord(record repository.ServiceRecord) repository.ServiceSnapshot {
	return repository.ServiceSnapshot{
		Name:            record.Name,
		RequestPrefix:   record.RequestPrefix,
		Description:     record.Description,
		DefaultProvider: record.DefaultProvider,
		DefaultModel:    record.DefaultModel,
		Enabled:         record.Enabled,
		Config:          normalizeServiceConfig(record.Config),
	}
}

func normalizeServiceConfig(config repository.ServiceConfig) repository.ServiceConfig {
	config.Surfaces = normalizeSurfaces(config.Surfaces)
	if config.Metadata == nil {
		config.Metadata = map[string]any{}
	}
	return config
}

func normalizeSurfaces(surfaces []string) []string {
	if len(surfaces) == 0 {
		return []string{"responses"}
	}
	seen := make(map[string]struct{}, len(surfaces))
	result := make([]string, 0, len(surfaces))
	for _, surface := range surfaces {
		value := strings.ToLower(strings.TrimSpace(surface))
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

func normalizeRequestPrefix(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "/"))
	return strings.ToLower(value)
}

func encodeJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
