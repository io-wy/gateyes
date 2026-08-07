package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func TestSemanticCacheCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	record, err := store.CreateSemanticCacheEntry(ctx, repository.CreateSemanticCacheParams{
		TenantID:            "tenant-a",
		Surface:             "responses",
		Model:               "gpt-test",
		EmbeddingModel:      "mock-embedding-model",
		PromptHash:          "hash-1",
		PromptCanonical:     []byte(`{"input":"hello"}`),
		PromptText:          "hello",
		Embedding:           []float64{0.1, 0.2, 0.3},
		ResponseBody:        []byte(`{"id":"r1"}`),
		ProviderName:        "openai-primary",
		UsageBody:           []byte(`{"prompt_tokens":3}`),
		SimilarityThreshold: 0.92,
		ExpiresAt:           time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSemanticCacheEntry: %v", err)
	}
	if record.ID == "" {
		t.Fatal("CreateSemanticCacheEntry returned empty ID")
	}

	got, err := store.GetSemanticCacheEntry(ctx, "tenant-a", record.ID)
	if err != nil {
		t.Fatalf("GetSemanticCacheEntry: %v", err)
	}
	if got.PromptHash != "hash-1" || len(got.Embedding) != 3 {
		t.Fatalf("GetSemanticCacheEntry = %+v", got)
	}

	ts := time.Now().UTC()
	hitCount := int64(2)
	disabled := true
	updated, err := store.UpdateSemanticCacheEntry(ctx, "tenant-a", record.ID, repository.UpdateSemanticCacheParams{
		LastHitAt: &ts,
		HitCount:  &hitCount,
		Disabled:  &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateSemanticCacheEntry: %v", err)
	}
	if updated.HitCount != 2 || !updated.Disabled || updated.LastHitAt == nil {
		t.Fatalf("updated = %+v", updated)
	}

	entries, err := store.ListSemanticCacheEntries(ctx, repository.SemanticCacheFilter{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("ListSemanticCacheEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListSemanticCacheEntries len = %d, want 1", len(entries))
	}

	n, err := store.DisableSemanticCacheEntriesByScope(ctx, "tenant-a", "", "", "", "responses", "gpt-test")
	if err != nil {
		t.Fatalf("DisableSemanticCacheEntriesByScope: %v", err)
	}
	if n != 1 {
		t.Fatalf("DisableSemanticCacheEntriesByScope = %d, want 1", n)
	}

	past := time.Now().UTC().Add(-time.Minute)
	if _, err := store.UpdateSemanticCacheEntry(ctx, "tenant-a", record.ID, repository.UpdateSemanticCacheParams{ExpiresAt: &past}); err != nil {
		t.Fatalf("UpdateSemanticCacheEntry(expire): %v", err)
	}
	deleted, err := store.DeleteSemanticCacheEntriesExpiredBefore(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("DeleteSemanticCacheEntriesExpiredBefore: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expected expired delete to affect rows")
	}
}

func TestSemanticCacheCandidates(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(time.Hour)

	for _, item := range []struct {
		hash      string
		embedding []float64
	}{
		{hash: "near", embedding: []float64{1, 0, 0}},
		{hash: "far", embedding: []float64{0, 1, 0}},
	} {
		if _, err := store.CreateSemanticCacheEntry(ctx, repository.CreateSemanticCacheParams{
			TenantID:            "tenant-candidate",
			ProjectID:           "project-a",
			Surface:             "responses",
			Model:               "gpt-test",
			EmbeddingModel:      "mock-embedding-model",
			PromptHash:          item.hash,
			PromptCanonical:     []byte(`{}`),
			PromptText:          item.hash,
			Embedding:           item.embedding,
			ResponseBody:        []byte(`{"id":"` + item.hash + `"}`),
			ProviderName:        "openai-primary",
			SimilarityThreshold: 0.92,
			ExpiresAt:           expires,
		}); err != nil {
			t.Fatalf("CreateSemanticCacheEntry(%s): %v", item.hash, err)
		}
	}

	candidates, err := store.FindSemanticCacheCandidates(ctx, repository.SemanticCacheFilter{
		TenantID:       "tenant-candidate",
		ProjectID:      "project-a",
		Surface:        "responses",
		Model:          "gpt-test",
		EmbeddingModel: "mock-embedding-model",
		Limit:          2,
	}, []float64{0.99, 0.01, 0})
	if err != nil {
		t.Fatalf("FindSemanticCacheCandidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates len = %d, want 2", len(candidates))
	}
	if candidates[0].PromptHash != "near" || candidates[0].Similarity <= candidates[1].Similarity {
		t.Fatalf("candidates = %+v, want near first", candidates)
	}
}
