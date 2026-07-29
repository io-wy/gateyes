package batch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gateyes/gateway/internal/pkg/eventbus"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

type Store interface {
	CreateBatchJob(ctx context.Context, params repository.CreateBatchJobParams) (*repository.BatchJobRecord, error)
	CreateBatchItem(ctx context.Context, params repository.CreateBatchItemParams) (*repository.BatchItemRecord, error)
	GetBatchJob(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error)
	ListBatchJobs(ctx context.Context, tenantID string, limit, offset int) ([]repository.BatchJobRecord, error)
	GetBatchItem(ctx context.Context, tenantID, id string) (*repository.BatchItemRecord, error)
	ListBatchItems(ctx context.Context, tenantID, jobID string) ([]repository.BatchItemRecord, error)
	MarkBatchItemRunning(ctx context.Context, tenantID, itemID string) (bool, error)
	CompleteBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error
	FailBatchItem(ctx context.Context, tenantID, itemID string, update repository.BatchItemUpdate) error
	CancelBatchItem(ctx context.Context, tenantID, itemID string) error
	CancelBatchJob(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error)
}

type Service struct {
	store     Store
	responses *responseSvc.Service
	eventBus  *eventbus.Bus
}

type Dependencies struct {
	Store     Store
	Responses *responseSvc.Service
	EventBus  *eventbus.Bus
}

type CreateRequest struct {
	Endpoint         string          `json:"endpoint"`
	Model            string          `json:"model"`
	CompletionWindow string          `json:"completion_window"`
	Metadata         json.RawMessage `json:"metadata"`
	Requests         []RequestItem   `json:"requests"`
}

type RequestItem struct {
	CustomID string          `json:"custom_id"`
	Body     json.RawMessage `json:"body"`
}

type ItemEvent struct {
	JobID       string                  `json:"job_id"`
	ItemID      string                  `json:"item_id"`
	TenantID    string                  `json:"tenant_id"`
	Endpoint    string                  `json:"endpoint"`
	Identity    repository.AuthIdentity `json:"identity"`
	RequestBody json.RawMessage         `json:"request_body"`
}

func New(deps Dependencies) *Service {
	s := &Service{
		store:     deps.Store,
		responses: deps.Responses,
		eventBus:  deps.EventBus,
	}
	if s.eventBus != nil {
		s.eventBus.RegisterEventHandler(eventbus.EventTypeBatchItem, s.handleBatchItemEvent)
	}
	return s
}

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, req CreateRequest, rawBody []byte) (*repository.BatchJobRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("batch service not configured")
	}
	if identity == nil || identity.TenantID == "" {
		return nil, fmt.Errorf("identity is required")
	}
	if len(req.Requests) == 0 {
		return nil, fmt.Errorf("requests must not be empty")
	}
	if req.Endpoint == "" {
		req.Endpoint = "/v1/responses"
	}
	if req.Endpoint != "/v1/responses" && req.Endpoint != "/v1/chat/completions" && req.Endpoint != "/v1/messages" {
		return nil, fmt.Errorf("unsupported batch endpoint: %s", req.Endpoint)
	}
	if req.CompletionWindow == "" {
		req.CompletionWindow = "24h"
	}
	if req.CompletionWindow != "24h" {
		return nil, fmt.Errorf("unsupported completion_window: %s", req.CompletionWindow)
	}
	if len(req.Metadata) == 0 {
		req.Metadata = json.RawMessage(`{}`)
	} else if !json.Valid(req.Metadata) {
		return nil, fmt.Errorf("metadata must be valid JSON")
	}
	if len(rawBody) == 0 {
		rawBody, _ = json.Marshal(req)
	}
	normalizedBodies := make([][]byte, len(req.Requests))
	for i, itemReq := range req.Requests {
		body, err := normalizeItemRequest(req.Endpoint, req.Model, itemReq.Body)
		if err != nil {
			return nil, fmt.Errorf("request %d: %w", i, err)
		}
		normalizedBodies[i] = body
	}

	job, err := s.store.CreateBatchJob(ctx, repository.CreateBatchJobParams{
		TenantID:         identity.TenantID,
		ProjectID:        identity.ProjectID,
		UserID:           identity.UserID,
		APIKeyID:         identity.APIKeyID,
		Endpoint:         req.Endpoint,
		Model:            req.Model,
		CompletionWindow: req.CompletionWindow,
		TotalItems:       len(req.Requests),
		RequestBody:      rawBody,
		Metadata:         req.Metadata,
	})
	if err != nil {
		return nil, err
	}

	for i, itemReq := range req.Requests {
		item, err := s.store.CreateBatchItem(ctx, repository.CreateBatchItemParams{
			JobID:       job.ID,
			TenantID:    identity.TenantID,
			Index:       i,
			CustomID:    itemReq.CustomID,
			RequestBody: normalizedBodies[i],
		})
		if err != nil {
			return nil, err
		}
		s.publishItem(ctx, *identity, job.ID, item.ID, identity.TenantID, req.Endpoint, normalizedBodies[i])
	}
	return s.store.GetBatchJob(ctx, identity.TenantID, job.ID)
}

func (s *Service) Get(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error) {
	return s.store.GetBatchJob(ctx, tenantID, id)
}

func (s *Service) List(ctx context.Context, tenantID string, limit, offset int) ([]repository.BatchJobRecord, error) {
	return s.store.ListBatchJobs(ctx, tenantID, limit, offset)
}

func (s *Service) Items(ctx context.Context, tenantID, jobID string) ([]repository.BatchItemRecord, error) {
	return s.store.ListBatchItems(ctx, tenantID, jobID)
}

func (s *Service) Cancel(ctx context.Context, tenantID, id string) (*repository.BatchJobRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("batch service not configured")
	}
	return s.store.CancelBatchJob(ctx, tenantID, id)
}

func (s *Service) publishItem(ctx context.Context, identity repository.AuthIdentity, jobID, itemID, tenantID, endpoint string, body []byte) {
	eventBody, _ := json.Marshal(ItemEvent{
		JobID:       jobID,
		ItemID:      itemID,
		TenantID:    tenantID,
		Endpoint:    endpoint,
		Identity:    identity,
		RequestBody: body,
	})
	if s.eventBus != nil {
		if s.eventBus.PublishEvent(ctx, eventbus.Event{
			Key:     jobID,
			Type:    eventbus.EventTypeBatchItem,
			Payload: eventBody,
		}) {
			return
		}
	}
	go func() {
		_ = s.handleBatchItemEvent(context.Background(), eventBody)
	}()
}

func (s *Service) handleBatchItemEvent(ctx context.Context, payload []byte) error {
	var event ItemEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshal batch item event: %w", err)
	}
	if event.TenantID == "" || event.ItemID == "" {
		return fmt.Errorf("batch item event missing tenant_id or item_id")
	}
	claimed, err := s.store.MarkBatchItemRunning(ctx, event.TenantID, event.ItemID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	job, err := s.store.GetBatchJob(ctx, event.TenantID, event.JobID)
	if err != nil {
		return err
	}
	if job.Status == repository.BatchStatusCancelling || job.Status == repository.BatchStatusCancelled {
		return s.store.CancelBatchItem(ctx, event.TenantID, event.ItemID)
	}
	var req provider.ResponseRequest
	if err := json.Unmarshal(event.RequestBody, &req); err != nil {
		failErr := s.store.FailBatchItem(ctx, event.TenantID, event.ItemID, repository.BatchItemUpdate{Error: err.Error()})
		if failErr != nil {
			return failErr
		}
		return nil
	}
	req.Stream = false
	req.Surface = surfaceFromEndpoint(event.Endpoint)
	result, err := s.responses.Create(ctx, &event.Identity, &req, "")
	if err != nil {
		failErr := s.store.FailBatchItem(ctx, event.TenantID, event.ItemID, repository.BatchItemUpdate{Error: err.Error()})
		if failErr != nil {
			return failErr
		}
		return nil
	}
	body, _ := json.Marshal(result.Response)
	return s.store.CompleteBatchItem(ctx, event.TenantID, event.ItemID, repository.BatchItemUpdate{
		ResponseBody:     body,
		ResponseID:       result.Response.ID,
		PromptTokens:     result.Response.Usage.PromptTokens,
		CompletionTokens: result.Response.Usage.CompletionTokens,
		TotalTokens:      result.Response.Usage.TotalTokens,
		CachedTokens:     result.Response.Usage.CachedTokens,
	})
}

func normalizeItemRequest(endpoint, fallbackModel string, raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("body is required")
	}
	var req provider.ResponseRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid body: %w", err)
	}
	if req.Model == "" {
		req.Model = fallbackModel
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	req.Stream = false
	return json.Marshal(req)
}

func surfaceFromEndpoint(endpoint string) string {
	switch endpoint {
	case "/v1/chat/completions":
		return "chat"
	case "/v1/messages":
		return "messages"
	default:
		return "responses"
	}
}
