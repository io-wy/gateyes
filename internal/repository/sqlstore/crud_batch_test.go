package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateAndCompleteBatchItems(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-batch",
		Slug: "tenant-batch",
		Name: "Tenant Batch",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	job, err := store.CreateBatchJob(ctx, repository.CreateBatchJobParams{
		TenantID:         tenant.ID,
		Endpoint:         "/v1/responses",
		Model:            "gpt-test",
		CompletionWindow: "24h",
		TotalItems:       2,
		RequestBody:      []byte(`{"requests":[{}]}`),
		Metadata:         []byte(`{"source":"test"}`),
	})
	if err != nil {
		t.Fatalf("CreateBatchJob() error: %v", err)
	}
	if job.Status != repository.BatchStatusPending || job.TotalItems != 2 {
		t.Fatalf("CreateBatchJob() = %+v, want pending total=2", job)
	}
	if job.CompletionWindow != "24h" || string(job.Metadata) != `{"source":"test"}` {
		t.Fatalf("job metadata fields = window:%q metadata:%s", job.CompletionWindow, job.Metadata)
	}

	item1, err := store.CreateBatchItem(ctx, repository.CreateBatchItemParams{
		JobID:       job.ID,
		TenantID:    tenant.ID,
		Index:       0,
		CustomID:    "a",
		RequestBody: []byte(`{"model":"gpt-test","input":"a"}`),
	})
	if err != nil {
		t.Fatalf("CreateBatchItem(1) error: %v", err)
	}
	item2, err := store.CreateBatchItem(ctx, repository.CreateBatchItemParams{
		JobID:       job.ID,
		TenantID:    tenant.ID,
		Index:       1,
		CustomID:    "b",
		RequestBody: []byte(`{"model":"gpt-test","input":"b"}`),
	})
	if err != nil {
		t.Fatalf("CreateBatchItem(2) error: %v", err)
	}

	if claimed, err := store.MarkBatchItemRunning(ctx, tenant.ID, item1.ID); err != nil {
		t.Fatalf("MarkBatchItemRunning() error: %v", err)
	} else if !claimed {
		t.Fatal("MarkBatchItemRunning() claimed = false, want true")
	}
	job, err = store.GetBatchJob(ctx, tenant.ID, job.ID)
	if err != nil {
		t.Fatalf("GetBatchJob(running) error: %v", err)
	}
	if job.Status != repository.BatchStatusRunning || job.InProgressAt == 0 {
		t.Fatalf("job after claim = %+v, want running with in_progress_at", job)
	}
	if err := store.CompleteBatchItem(ctx, tenant.ID, item1.ID, repository.BatchItemUpdate{
		ResponseBody:     []byte(`{"id":"resp-1"}`),
		ResponseID:       "resp-1",
		PromptTokens:     11,
		CompletionTokens: 7,
		TotalTokens:      18,
		CachedTokens:     3,
	}); err != nil {
		t.Fatalf("CompleteBatchItem() error: %v", err)
	}
	job, err = store.GetBatchJob(ctx, tenant.ID, job.ID)
	if err != nil {
		t.Fatalf("GetBatchJob() error: %v", err)
	}
	if job.Status != repository.BatchStatusRunning || job.CompletedItems != 1 || job.FailedItems != 0 {
		t.Fatalf("job after first completion = %+v, want running 1/0", job)
	}
	if job.PromptTokens != 11 || job.CompletionTokens != 7 || job.TotalTokens != 18 || job.CachedTokens != 3 {
		t.Fatalf("job token counters = %+v, want 11/7/18/3", job)
	}

	if claimed, err := store.MarkBatchItemRunning(ctx, tenant.ID, item2.ID); err != nil {
		t.Fatalf("MarkBatchItemRunning(2) error: %v", err)
	} else if !claimed {
		t.Fatal("MarkBatchItemRunning(2) claimed = false, want true")
	}
	if err := store.FailBatchItem(ctx, tenant.ID, item2.ID, repository.BatchItemUpdate{Error: "boom"}); err != nil {
		t.Fatalf("FailBatchItem() error: %v", err)
	}
	job, err = store.GetBatchJob(ctx, tenant.ID, job.ID)
	if err != nil {
		t.Fatalf("GetBatchJob(final) error: %v", err)
	}
	if job.Status != repository.BatchStatusFailed || job.CompletedItems != 1 || job.FailedItems != 1 {
		t.Fatalf("job final = %+v, want failed 1/1", job)
	}

	items, err := store.ListBatchItems(ctx, tenant.ID, job.ID)
	if err != nil {
		t.Fatalf("ListBatchItems() error: %v", err)
	}
	if len(items) != 2 || items[0].CustomID != "a" || items[1].CustomID != "b" {
		t.Fatalf("ListBatchItems() = %+v, want ordered custom ids", items)
	}
	if claimed, err := store.MarkBatchItemRunning(ctx, tenant.ID, item1.ID); err != nil {
		t.Fatalf("MarkBatchItemRunning(completed) error: %v", err)
	} else if claimed {
		t.Fatal("MarkBatchItemRunning(completed) claimed = true, want false")
	}
}

func TestCancelBatchJobCancelsPendingItems(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-batch-cancel",
		Slug: "tenant-batch-cancel",
		Name: "Tenant Batch Cancel",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	job, err := store.CreateBatchJob(ctx, repository.CreateBatchJobParams{
		TenantID:    tenant.ID,
		Endpoint:    "/v1/responses",
		Model:       "gpt-test",
		TotalItems:  1,
		RequestBody: []byte(`{"requests":[{}]}`),
	})
	if err != nil {
		t.Fatalf("CreateBatchJob() error: %v", err)
	}
	item, err := store.CreateBatchItem(ctx, repository.CreateBatchItemParams{
		JobID:       job.ID,
		TenantID:    tenant.ID,
		Index:       0,
		RequestBody: []byte(`{"model":"gpt-test","input":"a"}`),
	})
	if err != nil {
		t.Fatalf("CreateBatchItem() error: %v", err)
	}
	job, err = store.CancelBatchJob(ctx, tenant.ID, job.ID)
	if err != nil {
		t.Fatalf("CancelBatchJob() error: %v", err)
	}
	if job.Status != repository.BatchStatusCancelled || job.CancelledItems != 1 || job.CancelledAt == 0 {
		t.Fatalf("cancelled job = %+v, want cancelled count/timestamp", job)
	}
	if claimed, err := store.MarkBatchItemRunning(ctx, tenant.ID, item.ID); err != nil {
		t.Fatalf("MarkBatchItemRunning(cancelled) error: %v", err)
	} else if claimed {
		t.Fatal("MarkBatchItemRunning(cancelled) claimed = true, want false")
	}
	item, err = store.GetBatchItem(ctx, tenant.ID, item.ID)
	if err != nil {
		t.Fatalf("GetBatchItem(cancelled) error: %v", err)
	}
	if item.Status != repository.BatchItemStatusCancelled {
		t.Fatalf("item status = %q, want cancelled", item.Status)
	}
}
