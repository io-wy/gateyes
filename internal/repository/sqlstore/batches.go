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
	if params.CompletionWindow == "" {
		params.CompletionWindow = "24h"
	}
	if len(params.Metadata) == 0 {
		params.Metadata = []byte(`{}`)
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	status := repository.BatchStatusPending
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO batch_jobs (
	id, tenant_id, project_id, user_id, api_key_id, status, endpoint, model,
	completion_window, total_items, completed_items, failed_items, cancelled_items,
	prompt_tokens, completion_tokens, total_tokens, cached_tokens, request_body,
	metadata, error, in_progress_at, completed_at, failed_at, cancelled_at,
	created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, ?, ?, '', 0, 0, 0, 0, ?, ?)`),
		id,
		params.TenantID,
		params.ProjectID,
		params.UserID,
		params.APIKeyID,
		status,
		params.Endpoint,
		params.Model,
		params.CompletionWindow,
		params.TotalItems,
		string(params.RequestBody),
		string(params.Metadata),
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
	completion_window, total_items, completed_items, failed_items, cancelled_items,
	prompt_tokens, completion_tokens, total_tokens, cached_tokens, request_body,
	metadata, error, in_progress_at, completed_at, failed_at, cancelled_at,
	created_at, updated_at
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
	completion_window, total_items, completed_items, failed_items, cancelled_items,
	prompt_tokens, completion_tokens, total_tokens, cached_tokens, request_body,
	metadata, error, in_progress_at, completed_at, failed_at, cancelled_at,
	created_at, updated_at
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

func (s *Store) ListRecoverableBatchItems(ctx context.Context, cutoff time.Time, limit int) ([]repository.RecoverableBatchItemRecord, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT
	batch_items.id,
	batch_items.job_id,
	batch_items.tenant_id,
	batch_jobs.project_id,
	batch_jobs.user_id,
	batch_jobs.api_key_id,
	batch_jobs.endpoint,
	batch_items.request_body,
	batch_items.updated_at
FROM batch_items
JOIN batch_jobs
	ON batch_jobs.tenant_id = batch_items.tenant_id
	AND batch_jobs.id = batch_items.job_id
WHERE batch_items.status = ?
	AND batch_items.updated_at < ?
	AND batch_jobs.status NOT IN (?, ?, ?, ?)
ORDER BY batch_items.updated_at ASC
LIMIT ?`),
		repository.BatchItemStatusRunning,
		cutoff,
		repository.BatchStatusCompleted,
		repository.BatchStatusFailed,
		repository.BatchStatusCancelling,
		repository.BatchStatusCancelled,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recoverable batch items: %w", err)
	}
	defer rows.Close()

	var result []repository.RecoverableBatchItemRecord
	for rows.Next() {
		var item repository.RecoverableBatchItemRecord
		if err := rows.Scan(
			&item.ItemID,
			&item.JobID,
			&item.TenantID,
			&item.ProjectID,
			&item.UserID,
			&item.APIKeyID,
			&item.Endpoint,
			&item.RequestBody,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recoverable batch item: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recoverable batch items: %w", err)
	}
	return result, nil
}

func (s *Store) MarkBatchItemRunning(ctx context.Context, tenantID, itemID string) (bool, error) {
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin batch item claim: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	nowUnix := now.Unix()
	res, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_items
SET status = ?, updated_at = ?
WHERE tenant_id = ? AND id = ? AND status IN (?, ?)
	AND EXISTS (
		SELECT 1 FROM batch_jobs
		WHERE batch_jobs.tenant_id = batch_items.tenant_id
			AND batch_jobs.id = batch_items.job_id
			AND batch_jobs.status NOT IN (?, ?, ?, ?)
	)`),
		repository.BatchItemStatusRunning,
		now,
		tenantID,
		itemID,
		repository.BatchItemStatusPending,
		repository.BatchItemStatusRunning,
		repository.BatchStatusCompleted,
		repository.BatchStatusFailed,
		repository.BatchStatusCancelling,
		repository.BatchStatusCancelled,
	)
	if err != nil {
		return false, fmt.Errorf("mark batch item running: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_jobs
SET status = CASE WHEN status = ? THEN ? ELSE status END,
	in_progress_at = CASE WHEN in_progress_at = 0 THEN ? ELSE in_progress_at END,
	updated_at = ?
WHERE tenant_id = ?
	AND id = (SELECT job_id FROM batch_items WHERE tenant_id = ? AND id = ?)`),
		repository.BatchStatusPending,
		repository.BatchStatusRunning,
		nowUnix,
		now,
		tenantID,
		tenantID,
		itemID,
	); err != nil {
		return false, fmt.Errorf("mark batch job running: %w", err)
	}
	return true, tx.Commit()
}

func (s *Store) CompleteBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error {
	return s.updateBatchItemTerminal(ctx, tenantID, itemID, repository.BatchItemStatusCompleted, update)
}

func (s *Store) FailBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error {
	return s.updateBatchItemTerminal(ctx, tenantID, itemID, repository.BatchItemStatusFailed, update)
}

func (s *Store) CancelBatchItem(ctx context.Context, tenantID, itemID string) error {
	return s.updateBatchItemTerminal(ctx, tenantID, itemID, repository.BatchItemStatusCancelled, repository.BatchItemUpdate{})
}

func (s *Store) CancelBatchJob(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error) {
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin batch cancel: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`SELECT status FROM batch_jobs WHERE tenant_id = ? AND id = ?`), tenantID, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("load batch job for cancel: %w", err)
	}
	switch status {
	case repository.BatchStatusCompleted, repository.BatchStatusFailed:
		return nil, fmt.Errorf("batch status %s cannot be cancelled", status)
	case repository.BatchStatusCancelled, repository.BatchStatusCancelling:
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetBatchJob(ctx, tenantID, id)
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_items
SET status = ?, updated_at = ?
WHERE tenant_id = ? AND job_id = ? AND status = ?`),
		repository.BatchItemStatusCancelled,
		now,
		tenantID,
		id,
		repository.BatchItemStatusPending,
	); err != nil {
		return nil, fmt.Errorf("cancel pending batch items: %w", err)
	}

	var running int
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`
SELECT COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
FROM batch_items
WHERE tenant_id = ? AND job_id = ?`),
		repository.BatchItemStatusRunning,
		tenantID,
		id,
	).Scan(&running); err != nil {
		return nil, fmt.Errorf("count running batch items: %w", err)
	}

	nextStatus := repository.BatchStatusCancelled
	if running > 0 {
		nextStatus = repository.BatchStatusCancelling
	}
	if err := s.refreshBatchJobStatusTx(ctx, tx, tenantID, id, nextStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetBatchJob(ctx, tenantID, id)
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
WHERE tenant_id = ? AND id = ? AND status != ? AND status != ? AND status != ?`),
		status,
		string(update.ResponseBody),
		update.Error,
		update.ResponseID,
		now,
		tenantID,
		itemID,
		repository.BatchItemStatusCompleted,
		repository.BatchItemStatusFailed,
		repository.BatchItemStatusCancelled,
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
	if status == repository.BatchItemStatusCompleted {
		if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_jobs
SET prompt_tokens = prompt_tokens + ?,
	completion_tokens = completion_tokens + ?,
	total_tokens = total_tokens + ?,
	cached_tokens = cached_tokens + ?
WHERE tenant_id = ? AND id = ?`),
			update.PromptTokens,
			update.CompletionTokens,
			update.TotalTokens,
			update.CachedTokens,
			tenantID,
			jobID,
		); err != nil {
			return fmt.Errorf("update batch token counters: %w", err)
		}
	}
	if err := s.refreshBatchJobStatusTx(ctx, tx, tenantID, jobID, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) refreshBatchJobStatusTx(ctx context.Context, tx *sql.Tx, tenantID, jobID, preferredStatus string) error {
	var total, completed, failed, cancelled, running int
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`
SELECT
	COUNT(1),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
FROM batch_items
WHERE tenant_id = ? AND job_id = ?`),
		repository.BatchItemStatusCompleted,
		repository.BatchItemStatusFailed,
		repository.BatchItemStatusCancelled,
		repository.BatchItemStatusRunning,
		tenantID,
		jobID,
	).Scan(&total, &completed, &failed, &cancelled, &running); err != nil {
		return fmt.Errorf("summarize batch items: %w", err)
	}

	var currentStatus string
	if err := tx.QueryRowContext(ctx, s.db.Rebind(`SELECT status FROM batch_jobs WHERE tenant_id = ? AND id = ?`), tenantID, jobID).Scan(&currentStatus); err != nil {
		return fmt.Errorf("load batch job status: %w", err)
	}
	status := repository.BatchStatusPending
	terminal := completed + failed + cancelled
	switch {
	case preferredStatus != "":
		status = preferredStatus
	case currentStatus == repository.BatchStatusCancelled:
		status = repository.BatchStatusCancelled
	case currentStatus == repository.BatchStatusCancelling && terminal >= total:
		status = repository.BatchStatusCancelled
	case currentStatus == repository.BatchStatusCancelling:
		status = repository.BatchStatusCancelling
	case cancelled > 0 && terminal >= total:
		status = repository.BatchStatusCancelled
	case terminal >= total && failed > 0:
		status = repository.BatchStatusFailed
	case terminal >= total:
		status = repository.BatchStatusCompleted
	case running > 0 || completed > 0 || failed > 0 || cancelled > 0:
		status = repository.BatchStatusRunning
	}
	now := time.Now().UTC()
	nowUnix := now.Unix()

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE batch_jobs
SET status = ?,
	completed_items = ?,
	failed_items = ?,
	cancelled_items = ?,
	completed_at = CASE WHEN ? = ? AND completed_at = 0 THEN ? ELSE completed_at END,
	failed_at = CASE WHEN ? = ? AND failed_at = 0 THEN ? ELSE failed_at END,
	cancelled_at = CASE WHEN ? = ? AND cancelled_at = 0 THEN ? ELSE cancelled_at END,
	updated_at = ?
WHERE tenant_id = ? AND id = ?`),
		status,
		completed,
		failed,
		cancelled,
		status,
		repository.BatchStatusCompleted,
		nowUnix,
		status,
		repository.BatchStatusFailed,
		nowUnix,
		status,
		repository.BatchStatusCancelled,
		nowUnix,
		now,
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
	var requestBody, metadata string
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.ProjectID,
		&record.UserID,
		&record.APIKeyID,
		&record.Status,
		&record.Endpoint,
		&record.Model,
		&record.CompletionWindow,
		&record.TotalItems,
		&record.CompletedItems,
		&record.FailedItems,
		&record.CancelledItems,
		&record.PromptTokens,
		&record.CompletionTokens,
		&record.TotalTokens,
		&record.CachedTokens,
		&requestBody,
		&metadata,
		&record.Error,
		&record.InProgressAt,
		&record.CompletedAt,
		&record.FailedAt,
		&record.CancelledAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan batch job: %w", err)
	}
	record.RequestBody = []byte(requestBody)
	record.Metadata = []byte(metadata)
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
