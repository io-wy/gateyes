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

func (s *Store) CreateBatchJob(ctx context.Context, params repository.CreateBatchJobParams) (*repository.BatchJobRecord, error) {
	if params.TenantID == "" {
		return nil, fmt.Errorf("tenant id is required")
	}
	if params.TotalItems <= 0 {
		return nil, fmt.Errorf("total items must be positive")
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	status := repository.BatchStatusPending
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO batch_jobs (
	id, tenant_id, project_id, user_id, api_key_id, status, endpoint, model,
	total_items, completed_items, failed_items, request_body, error, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?, '', ?, ?)`),
		id,
		params.TenantID,
		params.ProjectID,
		params.UserID,
		params.APIKeyID,
		status,
		params.Endpoint,
		params.Model,
		params.TotalItems,
		string(params.RequestBody),
		now,
		now,
	); err != nil {
		return nil, fmt.Errorf("create batch job: %w", err)
	}
	return s.GetBatchJob(ctx, params.TenantID, id)
}

func (s *Store) CreateBatchItem(ctx context.Context, params repository.CreateBatchItemParams) (*repository.BatchItemRecord, error) {
	if params.JobID == "" || params.TenantID == "" {
		return nil, fmt.Errorf("job id and tenant id are required")
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO batch_items (
	id, job_id, tenant_id, item_index, custom_id, status, request_body,
	response_body, error, response_id, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, '', '', '', ?, ?)`),
		id,
		params.JobID,
		params.TenantID,
		params.Index,
		params.CustomID,
		repository.BatchItemStatusPending,
		string(params.RequestBody),
		now,
		now,
	); err != nil {
		return nil, fmt.Errorf("create batch item: %w", err)
	}
	return s.GetBatchItem(ctx, params.TenantID, id)
}

func (s *Store) GetBatchJob(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT id, tenant_id, project_id, user_id, api_key_id, status, endpoint, model,
	total_items, completed_items, failed_items, request_body, error, created_at, updated_at
FROM batch_jobs
WHERE tenant_id = ? AND id = ?`), tenantID, id)
	return scanBatchJob(row)
}

func (s *Store) ListBatchJobs(ctx context.Context, tenantID string, limit, offset int) ([]repository.BatchJobRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT id, tenant_id, project_id, user_id, api_key_id, status, endpoint, model,
	total_items, completed_items, failed_items, request_body, error, created_at, updated_at
FROM batch_jobs
WHERE tenant_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?`), tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list batch jobs: %w", err)
	}
	defer rows.Close()

	var result []repository.BatchJobRecord
	for rows.Next() {
		item, err := scanBatchJobRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch jobs: %w", err)
	}
	return result, nil
}

func (s *Store) GetBatchItem(ctx context.Context, tenantID, id string) (*repository.BatchItemRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT id, job_id, tenant_id, item_index, custom_id, status, request_body,
	response_body, error, response_id, created_at, updated_at
FROM batch_items
WHERE tenant_id = ? AND id = ?`), tenantID, id)
	return scanBatchItem(row)
}

func (s *Store) ListBatchItems(ctx context.Context, tenantID, jobID string) ([]repository.BatchItemRecord, error) {
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT id, job_id, tenant_id, item_index, custom_id, status, request_body,
	response_body, error, response_id, created_at, updated_at
FROM batch_items
WHERE tenant_id = ? AND job_id = ?
ORDER BY item_index ASC`), tenantID, jobID)
	if err != nil {
		return nil, fmt.Errorf("list batch items: %w", err)
	}
	defer rows.Close()

	var result []repository.BatchItemRecord
	for rows.Next() {
		item, err := scanBatchItemRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch items: %w", err)
	}
	return result, nil
}

func (s *Store) MarkBatchItemRunning(ctx context.Context, tenantID, itemID string) (bool, error) {
	now := time.Now().UTC()
	res, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_items
SET status = ?, updated_at = ?
WHERE tenant_id = ? AND id = ? AND status = ?`),
		repository.BatchItemStatusRunning,
		now,
		tenantID,
		itemID,
		repository.BatchItemStatusPending,
	)
	if err != nil {
		return false, fmt.Errorf("mark batch item running: %w", err)
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func (s *Store) CompleteBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error {
	return s.updateBatchItemTerminal(ctx, tenantID, itemID, repository.BatchItemStatusCompleted, update)
}

func (s *Store) FailBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error {
	return s.updateBatchItemTerminal(ctx, tenantID, itemID, repository.BatchItemStatusFailed, update)
}

func (s *Store) updateBatchItemTerminal(ctx context.Context, tenantID, itemID, status string, update repository.BatchItemUpdate) error {
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin batch item update: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_items
SET status = ?, response_body = ?, error = ?, response_id = ?, updated_at = ?
WHERE tenant_id = ? AND id = ? AND status != ? AND status != ?`),
		status,
		string(update.ResponseBody),
		update.Error,
		update.ResponseID,
		now,
		tenantID,
		itemID,
		repository.BatchItemStatusCompleted,
		repository.BatchItemStatusFailed,
	)
	if err != nil {
		return fmt.Errorf("update batch item: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// Terminal updates are idempotent so Kafka redelivery after a successful
		// commit race does not keep the message uncommitted forever.
		return nil
	}

	var jobID string
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`SELECT job_id FROM batch_items WHERE tenant_id = ? AND id = ?`), tenantID, itemID).Scan(&jobID); err != nil {
		return fmt.Errorf("load batch item job: %w", err)
	}
	if err := s.refreshBatchJobStatusTx(ctx, tx, tenantID, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) refreshBatchJobStatusTx(ctx context.Context, tx *sql.Tx, tenantID, jobID string) error {
	var total, completed, failed, running int
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`
SELECT
	COUNT(1),
	SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
	SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
	SUM(CASE WHEN status = ? THEN 1 ELSE 0 END)
FROM batch_items
WHERE tenant_id = ? AND job_id = ?`),
		repository.BatchItemStatusCompleted,
		repository.BatchItemStatusFailed,
		repository.BatchItemStatusRunning,
		tenantID,
		jobID,
	).Scan(&total, &completed, &failed, &running); err != nil {
		return fmt.Errorf("summarize batch items: %w", err)
	}

	status := repository.BatchStatusPending
	switch {
	case completed+failed >= total && failed > 0:
		status = repository.BatchStatusFailed
	case completed+failed >= total:
		status = repository.BatchStatusCompleted
	case running > 0 || completed > 0 || failed > 0:
		status = repository.BatchStatusRunning
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_jobs
SET status = ?, completed_items = ?, failed_items = ?, updated_at = ?
WHERE tenant_id = ? AND id = ?`),
		status,
		completed,
		failed,
		time.Now().UTC(),
		tenantID,
		jobID,
	); err != nil {
		return fmt.Errorf("update batch job summary: %w", err)
	}
	return nil
}

type batchJobScanner interface {
	Scan(dest ...any) error
}

func scanBatchJob(row batchJobScanner) (*repository.BatchJobRecord, error) {
	record, err := scanBatchJobRows(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return record, nil
}

func scanBatchJobRows(row batchJobScanner) (*repository.BatchJobRecord, error) {
	var record repository.BatchJobRecord
	var requestBody string
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.ProjectID,
		&record.UserID,
		&record.APIKeyID,
		&record.Status,
		&record.Endpoint,
		&record.Model,
		&record.TotalItems,
		&record.CompletedItems,
		&record.FailedItems,
		&requestBody,
		&record.Error,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan batch job: %w", err)
	}
	record.RequestBody = []byte(requestBody)
	return &record, nil
}

type batchItemScanner interface {
	Scan(dest ...any) error
}

func scanBatchItem(row batchItemScanner) (*repository.BatchItemRecord, error) {
	record, err := scanBatchItemRows(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return record, nil
}

func scanBatchItemRows(row batchItemScanner) (*repository.BatchItemRecord, error) {
	var record repository.BatchItemRecord
	var requestBody, responseBody string
	if err := row.Scan(
		&record.ID,
		&record.JobID,
		&record.TenantID,
		&record.Index,
		&record.CustomID,
		&record.Status,
		&requestBody,
		&responseBody,
		&record.Error,
		&record.ResponseID,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan batch item: %w", err)
	}
	record.RequestBody = []byte(requestBody)
	record.ResponseBody = []byte(responseBody)
	return &record, nil
}
