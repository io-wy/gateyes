package batch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestNormalizeItemRequestUsesBatchModelAndDisablesStream(t *testing.T) {
	body, err := normalizeItemRequest("/v1/responses", "gpt-test", json.RawMessage(`{"input":"hello","stream":true}`))
	if err != nil {
		t.Fatalf("normalizeItemRequest() error: %v", err)
	}
	var req provider.ResponseRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if req.Model != "gpt-test" {
		t.Fatalf("model = %q, want inherited gpt-test", req.Model)
	}
	if req.Stream {
		t.Fatal("stream = true, want false for batch item")
	}
}

func TestSurfaceFromEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{endpoint: "/v1/chat/completions", want: "chat"},
		{endpoint: "/v1/messages", want: "messages"},
		{endpoint: "/v1/responses", want: "responses"},
	}
	for _, tc := range tests {
		if got := surfaceFromEndpoint(tc.endpoint); got != tc.want {
			t.Fatalf("surfaceFromEndpoint(%s) = %q, want %q", tc.endpoint, got, tc.want)
		}
	}
}

func TestCreateValidatesAllItemsBeforeCreatingJob(t *testing.T) {
	store := &fakeStore{}
	svc := New(Dependencies{Store: store})
	_, err := svc.Create(context.Background(), &repository.AuthIdentity{TenantID: "tenant-1"}, CreateRequest{
		Endpoint: "/v1/responses",
		Model:    "gpt-test",
		Requests: []RequestItem{
			{Body: json.RawMessage(`{"input":"ok"}`)},
			{Body: json.RawMessage(`{bad`)},
		},
	}, nil)
	if err == nil {
		t.Fatal("Create() error = nil, want invalid item error")
	}
	if store.createJobCalls != 0 || store.createItemCalls != 0 {
		t.Fatalf("store calls = job:%d item:%d, want no writes before validation succeeds", store.createJobCalls, store.createItemCalls)
	}
}

func TestCreateStoresCompletionWindowAndMetadata(t *testing.T) {
	store := &fakeStore{}
	svc := New(Dependencies{Store: store})
	_, err := svc.Create(context.Background(), &repository.AuthIdentity{TenantID: "tenant-1"}, CreateRequest{
		Endpoint:         "/v1/responses",
		Model:            "gpt-test",
		CompletionWindow: "24h",
		Metadata:         json.RawMessage(`{"source":"test"}`),
		Requests: []RequestItem{
			{Body: json.RawMessage(`{"input":"ok"}`)},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if store.lastCreateJob.CompletionWindow != "24h" {
		t.Fatalf("CompletionWindow = %q, want 24h", store.lastCreateJob.CompletionWindow)
	}
	if string(store.lastCreateJob.Metadata) != `{"source":"test"}` {
		t.Fatalf("Metadata = %s, want source metadata", store.lastCreateJob.Metadata)
	}
}

func TestHandleBatchItemEventSkipsWhenItemNotClaimed(t *testing.T) {
	store := &fakeStore{claimResult: false}
	svc := New(Dependencies{Store: store})
	payload, err := json.Marshal(ItemEvent{
		ItemID:      "item-1",
		TenantID:    "tenant-1",
		Endpoint:    "/v1/responses",
		RequestBody: json.RawMessage(`{"model":"gpt-test","input":"hello"}`),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if err := svc.handleBatchItemEvent(context.Background(), payload); err != nil {
		t.Fatalf("handleBatchItemEvent() error: %v", err)
	}
	if store.markCalls != 1 {
		t.Fatalf("MarkBatchItemRunning calls = %d, want 1", store.markCalls)
	}
}

func TestHandleBatchItemEventCancelsClaimedItemWhenJobCancelled(t *testing.T) {
	store := &fakeStore{claimResult: true, jobStatus: repository.BatchStatusCancelled}
	svc := New(Dependencies{Store: store})
	payload, err := json.Marshal(ItemEvent{
		JobID:       "job-1",
		ItemID:      "item-1",
		TenantID:    "tenant-1",
		Endpoint:    "/v1/responses",
		RequestBody: json.RawMessage(`{"model":"gpt-test","input":"hello"}`),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if err := svc.handleBatchItemEvent(context.Background(), payload); err != nil {
		t.Fatalf("handleBatchItemEvent() error: %v", err)
	}
	if store.cancelItemCalls != 1 {
		t.Fatalf("CancelBatchItem calls = %d, want 1", store.cancelItemCalls)
	}
}

type fakeStore struct {
	createJobCalls  int
	createItemCalls int
	markCalls       int
	cancelItemCalls int
	claimResult     bool
	lastCreateJob   repository.CreateBatchJobParams
	jobStatus       string
}

func (f *fakeStore) CreateBatchJob(ctx context.Context, params repository.CreateBatchJobParams) (*repository.BatchJobRecord, error) {
	f.createJobCalls++
	f.lastCreateJob = params
	return &repository.BatchJobRecord{ID: "job-1", TenantID: params.TenantID, TotalItems: params.TotalItems, CompletionWindow: params.CompletionWindow, Metadata: params.Metadata}, nil
}

func (f *fakeStore) CreateBatchItem(ctx context.Context, params repository.CreateBatchItemParams) (*repository.BatchItemRecord, error) {
	f.createItemCalls++
	return &repository.BatchItemRecord{ID: "item-1", JobID: params.JobID, TenantID: params.TenantID}, nil
}

func (f *fakeStore) GetBatchJob(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error) {
	status := f.jobStatus
	if status == "" {
		status = repository.BatchStatusRunning
	}
	return &repository.BatchJobRecord{ID: id, TenantID: tenantID, Status: status}, nil
}

func (f *fakeStore) ListBatchJobs(ctx context.Context, tenantID string, limit, offset int) ([]repository.BatchJobRecord, error) {
	return nil, nil
}

func (f *fakeStore) GetBatchItem(ctx context.Context, tenantID, id string) (*repository.BatchItemRecord, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeStore) ListBatchItems(ctx context.Context, tenantID, jobID string) ([]repository.BatchItemRecord, error) {
	return nil, nil
}

func (f *fakeStore) MarkBatchItemRunning(ctx context.Context, tenantID, itemID string) (bool, error) {
	f.markCalls++
	return f.claimResult, nil
}

func (f *fakeStore) CompleteBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error {
	return nil
}

func (f *fakeStore) FailBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error {
	return nil
}

func (f *fakeStore) CancelBatchItem(ctx context.Context, tenantID, itemID string) error {
	f.cancelItemCalls++
	return nil
}

func (f *fakeStore) CancelBatchJob(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error) {
	return &repository.BatchJobRecord{ID: id, TenantID: tenantID, Status: repository.BatchStatusCancelled}, nil
}
