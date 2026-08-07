package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateSemanticCacheEntry(ctx context.Context, params repository.CreateSemanticCacheParams) (*repository.SemanticCacheRecord, error) {
	now := time.Now().UTC()
	if params.ExpiresAt.IsZero() {
		params.ExpiresAt = now.Add(3600 * time.Second)
	}
	record := repository.SemanticCacheRecord{
		ID:                  uuid.NewString(),
		TenantID:            params.TenantID,
		ProjectID:           params.ProjectID,
		ServiceID:           params.ServiceID,
		APIKeyID:            params.APIKeyID,
		Surface:             params.Surface,
		Model:               params.Model,
		EmbeddingModel:      params.EmbeddingModel,
		PromptHash:          params.PromptHash,
		PromptCanonical:     cloneBytes(params.PromptCanonical),
		PromptText:          params.PromptText,
		Embedding:           cloneFloat64Slice(params.Embedding),
		ResponseBody:        cloneBytes(params.ResponseBody),
		StreamBody:          cloneBytes(params.StreamBody),
		ProviderName:        params.ProviderName,
		UsageBody:           cloneBytes(params.UsageBody),
		SimilarityThreshold: params.SimilarityThreshold,
		ExpiresAt:           params.ExpiresAt,
		CreatedAt:           now,
		Disabled:            params.Disabled,
	}
	if err := s.semanticCacheInsert(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) ListSemanticCacheEntries(ctx context.Context, filter repository.SemanticCacheFilter) ([]repository.SemanticCacheRecord, error) {
	query := `
SELECT id, tenant_id, project_id, service_id, api_key_id, surface, model, embedding_model, prompt_hash,
       prompt_canonical, prompt_text, embedding, response_body, stream_body, provider_name, usage_body,
       similarity_threshold, expires_at, created_at, last_hit_at, hit_count, disabled
FROM semantic_cache_entries`
	where, args := semanticCacheWhere(filter)
	if where != "" {
		query += "\nWHERE " + where
	}
	query += "\nORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list semantic cache entries: %w", err)
	}
	defer rows.Close()

	var out []repository.SemanticCacheRecord
	for rows.Next() {
		record, err := scanSemanticCacheRecord(rows, s.db.Driver())
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate semantic cache entries: %w", err)
	}
	return out, nil
}

func (s *Store) FindSemanticCacheCandidates(ctx context.Context, filter repository.SemanticCacheFilter, embedding []float64) ([]repository.SemanticCacheCandidate, error) {
	if s.db.Driver() != "postgres" {
		entries, err := s.ListSemanticCacheEntries(ctx, filter)
		if err != nil {
			return nil, err
		}
		candidates := make([]repository.SemanticCacheCandidate, 0, len(entries))
		for _, entry := range entries {
			if entry.Disabled || !entry.ExpiresAt.IsZero() && time.Now().UTC().After(entry.ExpiresAt) {
				continue
			}
			sim := cosineSimilarity(embedding, entry.Embedding)
			candidates = append(candidates, repository.SemanticCacheCandidate{SemanticCacheRecord: entry, Similarity: sim})
		}
		sortSemanticCandidates(candidates)
		if filter.Limit > 0 && len(candidates) > filter.Limit {
			candidates = candidates[:filter.Limit]
		}
		return candidates, nil
	}

	query := `
SELECT id, tenant_id, project_id, service_id, api_key_id, surface, model, embedding_model, prompt_hash,
       prompt_canonical, prompt_text, embedding, response_body, stream_body, provider_name, usage_body,
       similarity_threshold, expires_at, created_at, last_hit_at, hit_count, disabled,
       1 - (embedding <=> ?::vector) AS similarity
FROM semantic_cache_entries`
	where, args := semanticCacheWhere(filter)
	vec := encodePGVector(embedding)
	queryArgs := []any{vec}
	if where != "" {
		query += "\nWHERE " + where
		queryArgs = append(queryArgs, args...)
		query += " AND disabled = 0 AND expires_at > NOW()"
	} else {
		query += "\nWHERE disabled = 0 AND expires_at > NOW()"
	}
	query += "\nORDER BY embedding <=> ?::vector ASC"
	queryArgs = append(queryArgs, vec)
	if filter.Limit > 0 {
		query += "\nLIMIT ?"
		queryArgs = append(queryArgs, filter.Limit)
	}
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("find semantic cache candidates: %w", err)
	}
	defer rows.Close()

	var out []repository.SemanticCacheCandidate
	for rows.Next() {
		record, sim, err := scanSemanticCacheCandidate(rows, s.db.Driver())
		if err != nil {
			return nil, err
		}
		out = append(out, repository.SemanticCacheCandidate{SemanticCacheRecord: *record, Similarity: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate semantic cache candidates: %w", err)
	}
	return out, nil
}

func (s *Store) GetSemanticCacheEntry(ctx context.Context, tenantID, id string) (*repository.SemanticCacheRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT id, tenant_id, project_id, service_id, api_key_id, surface, model, embedding_model, prompt_hash,
       prompt_canonical, prompt_text, embedding, response_body, stream_body, provider_name, usage_body,
       similarity_threshold, expires_at, created_at, last_hit_at, hit_count, disabled
FROM semantic_cache_entries
WHERE id = ? AND tenant_id = ?
LIMIT 1`), id, tenantID)
	record, err := scanSemanticCacheRecord(row, s.db.Driver())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get semantic cache entry: %w", err)
	}
	return record, nil
}

func (s *Store) UpdateSemanticCacheEntry(ctx context.Context, tenantID, id string, params repository.UpdateSemanticCacheParams) (*repository.SemanticCacheRecord, error) {
	current, err := s.GetSemanticCacheEntry(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	next := *current
	if params.LastHitAt != nil {
		next.LastHitAt = params.LastHitAt
	}
	if params.HitCount != nil {
		next.HitCount = *params.HitCount
	}
	if params.Disabled != nil {
		next.Disabled = *params.Disabled
	}
	if params.ExpiresAt != nil {
		next.ExpiresAt = *params.ExpiresAt
	}
	if err := s.semanticCacheUpdate(ctx, next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *Store) DeleteSemanticCacheEntry(ctx context.Context, tenantID, id string) error {
	_, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
DELETE FROM semantic_cache_entries
WHERE id = ? AND tenant_id = ?`), id, tenantID)
	if err != nil {
		return fmt.Errorf("delete semantic cache entry: %w", err)
	}
	return nil
}

func (s *Store) DisableSemanticCacheEntriesByScope(ctx context.Context, tenantID, projectID, serviceID, apiKeyID, surface, model string) (int64, error) {
	query := `UPDATE semantic_cache_entries SET disabled = 1 WHERE tenant_id = ?`
	args := []any{tenantID}
	if projectID != "" {
		query += ` AND project_id = ?`
		args = append(args, projectID)
	}
	if serviceID != "" {
		query += ` AND service_id = ?`
		args = append(args, serviceID)
	}
	if apiKeyID != "" {
		query += ` AND api_key_id = ?`
		args = append(args, apiKeyID)
	}
	if surface != "" {
		query += ` AND surface = ?`
		args = append(args, surface)
	}
	if model != "" {
		query += ` AND model = ?`
		args = append(args, model)
	}
	res, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return 0, fmt.Errorf("disable semantic cache entries: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) DeleteSemanticCacheEntriesExpiredBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
DELETE FROM semantic_cache_entries
WHERE expires_at < ?`), before)
	if err != nil {
		return 0, fmt.Errorf("delete expired semantic cache entries: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) semanticCacheInsert(ctx context.Context, record repository.SemanticCacheRecord) error {
	embedding, err := encodeEmbeddingValue(s.db.Driver(), record.Embedding)
	if err != nil {
		return err
	}
	promptCanonical := string(record.PromptCanonical)
	responseBody := string(record.ResponseBody)
	streamBody := record.StreamBody
	usageBody := string(record.UsageBody)
	lastHitAt := any(nil)
	if record.LastHitAt != nil {
		lastHitAt = *record.LastHitAt
	}
	disabled := 0
	if record.Disabled {
		disabled = 1
	}
	_, err = s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO semantic_cache_entries (
	id, tenant_id, project_id, service_id, api_key_id, surface, model, embedding_model,
	prompt_hash, prompt_canonical, prompt_text, embedding, response_body, stream_body,
	provider_name, usage_body, similarity_threshold, expires_at, created_at, last_hit_at,
	hit_count, disabled
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID, record.TenantID, record.ProjectID, record.ServiceID, record.APIKeyID,
		record.Surface, record.Model, record.EmbeddingModel, record.PromptHash, promptCanonical,
		record.PromptText, embedding, responseBody, streamBody, record.ProviderName, usageBody,
		record.SimilarityThreshold, record.ExpiresAt, record.CreatedAt, lastHitAt, record.HitCount, disabled,
	)
	if err != nil {
		return fmt.Errorf("insert semantic cache entry: %w", err)
	}
	return nil
}

func (s *Store) semanticCacheUpdate(ctx context.Context, record repository.SemanticCacheRecord) error {
	embedding, err := encodeEmbeddingValue(s.db.Driver(), record.Embedding)
	if err != nil {
		return err
	}
	promptCanonical := string(record.PromptCanonical)
	responseBody := string(record.ResponseBody)
	streamBody := record.StreamBody
	usageBody := string(record.UsageBody)
	lastHitAt := any(nil)
	if record.LastHitAt != nil {
		lastHitAt = *record.LastHitAt
	}
	disabled := 0
	if record.Disabled {
		disabled = 1
	}
	_, err = s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE semantic_cache_entries
SET project_id = ?, service_id = ?, api_key_id = ?, surface = ?, model = ?, embedding_model = ?,
    prompt_hash = ?, prompt_canonical = ?, prompt_text = ?, embedding = ?, response_body = ?, stream_body = ?,
    provider_name = ?, usage_body = ?, similarity_threshold = ?, expires_at = ?, last_hit_at = ?, hit_count = ?,
    disabled = ?
WHERE id = ? AND tenant_id = ?`),
		record.ProjectID, record.ServiceID, record.APIKeyID, record.Surface, record.Model, record.EmbeddingModel,
		record.PromptHash, promptCanonical, record.PromptText, embedding, responseBody, streamBody,
		record.ProviderName, usageBody, record.SimilarityThreshold, record.ExpiresAt, lastHitAt, record.HitCount,
		disabled, record.ID, record.TenantID,
	)
	if err != nil {
		return fmt.Errorf("update semantic cache entry: %w", err)
	}
	return nil
}

func semanticCacheWhere(filter repository.SemanticCacheFilter) (string, []any) {
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
	if filter.ServiceID != "" {
		clauses = append(clauses, "service_id = ?")
		args = append(args, filter.ServiceID)
	}
	if filter.APIKeyID != "" {
		clauses = append(clauses, "api_key_id = ?")
		args = append(args, filter.APIKeyID)
	}
	if filter.Surface != "" {
		clauses = append(clauses, "surface = ?")
		args = append(args, filter.Surface)
	}
	if filter.Model != "" {
		clauses = append(clauses, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.EmbeddingModel != "" {
		clauses = append(clauses, "embedding_model = ?")
		args = append(args, filter.EmbeddingModel)
	}
	if filter.Disabled != nil {
		if *filter.Disabled {
			clauses = append(clauses, "disabled = 1")
		} else {
			clauses = append(clauses, "disabled = 0")
		}
	}
	return strings.Join(clauses, " AND "), args
}

func scanSemanticCacheRecord(scanner interface {
	Scan(dest ...any) error
}, driver string) (*repository.SemanticCacheRecord, error) {
	var record repository.SemanticCacheRecord
	var promptCanonical string
	var responseBody string
	var streamBody []byte
	var usageBody string
	var lastHitAt sql.NullTime
	var disabled int
	var embeddingRaw string
	if driver == "postgres" {
		if err := scanner.Scan(
			&record.ID, &record.TenantID, &record.ProjectID, &record.ServiceID, &record.APIKeyID, &record.Surface, &record.Model, &record.EmbeddingModel,
			&record.PromptHash, &promptCanonical, &record.PromptText, &embeddingRaw, &responseBody, &streamBody,
			&record.ProviderName, &usageBody, &record.SimilarityThreshold, &record.ExpiresAt, &record.CreatedAt, &lastHitAt,
			&record.HitCount, &disabled,
		); err != nil {
			return nil, err
		}
		record.Embedding = decodeEmbeddingValue(embeddingRaw)
	} else {
		if err := scanner.Scan(
			&record.ID, &record.TenantID, &record.ProjectID, &record.ServiceID, &record.APIKeyID, &record.Surface, &record.Model, &record.EmbeddingModel,
			&record.PromptHash, &promptCanonical, &record.PromptText, &embeddingRaw, &responseBody, &streamBody,
			&record.ProviderName, &usageBody, &record.SimilarityThreshold, &record.ExpiresAt, &record.CreatedAt, &lastHitAt,
			&record.HitCount, &disabled,
		); err != nil {
			return nil, err
		}
		record.Embedding = decodeEmbeddingValue(embeddingRaw)
	}
	record.PromptCanonical = []byte(promptCanonical)
	record.ResponseBody = []byte(responseBody)
	record.StreamBody = cloneBytes(streamBody)
	record.UsageBody = []byte(usageBody)
	if lastHitAt.Valid {
		ts := lastHitAt.Time.UTC()
		record.LastHitAt = &ts
	}
	record.Disabled = disabled == 1
	return &record, nil
}

func scanSemanticCacheCandidate(scanner interface {
	Scan(dest ...any) error
}, driver string) (*repository.SemanticCacheRecord, float64, error) {
	var record repository.SemanticCacheRecord
	var promptCanonical string
	var responseBody string
	var streamBody []byte
	var usageBody string
	var lastHitAt sql.NullTime
	var disabled int
	var embeddingRaw string
	var similarity float64
	if err := scanner.Scan(
		&record.ID, &record.TenantID, &record.ProjectID, &record.ServiceID, &record.APIKeyID, &record.Surface, &record.Model, &record.EmbeddingModel,
		&record.PromptHash, &promptCanonical, &record.PromptText, &embeddingRaw, &responseBody, &streamBody,
		&record.ProviderName, &usageBody, &record.SimilarityThreshold, &record.ExpiresAt, &record.CreatedAt, &lastHitAt,
		&record.HitCount, &disabled, &similarity,
	); err != nil {
		return nil, 0, err
	}
	record.Embedding = decodeEmbeddingValue(embeddingRaw)
	record.PromptCanonical = []byte(promptCanonical)
	record.ResponseBody = []byte(responseBody)
	record.StreamBody = cloneBytes(streamBody)
	record.UsageBody = []byte(usageBody)
	if lastHitAt.Valid {
		ts := lastHitAt.Time.UTC()
		record.LastHitAt = &ts
	}
	record.Disabled = disabled == 1
	return &record, similarity, nil
}

func encodeEmbeddingValue(driver string, values []float64) (string, error) {
	if driver == "postgres" {
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, fmt.Sprintf("%f", value))
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeEmbeddingValue(raw string) []float64 {
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var values []float64
		if err := json.Unmarshal([]byte(raw), &values); err == nil {
			return values
		}
	}
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(part, "%f", &v); err == nil {
			values = append(values, v)
		}
	}
	return values
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneFloat64Slice(in []float64) []float64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]float64, len(in))
	copy(out, in)
	return out
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < limit; i++ {
		dot += a[i] * b[i]
	}
	for _, v := range a {
		na += v * v
	}
	for _, v := range b {
		nb += v * v
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func sortSemanticCandidates(items []repository.SemanticCacheCandidate) {
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j-1].Similarity < items[j].Similarity {
			items[j-1], items[j] = items[j], items[j-1]
			j--
		}
	}
}

func encodePGVector(values []float64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%f", value))
	}
	return "[" + strings.Join(parts, ",") + "]"
}
