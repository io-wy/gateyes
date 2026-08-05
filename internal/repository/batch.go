package repository

import "time"

const (
	BatchStatusPending    = "pending"
	BatchStatusRunning    = "running"
	BatchStatusCompleted  = "completed"
	BatchStatusFailed     = "failed"
	BatchStatusCancelling = "cancelling"
	BatchStatusCancelled  = "cancelled"

	BatchItemStatusPending   = "pending"
	BatchItemStatusRunning   = "running"
	BatchItemStatusCompleted = "completed"
	BatchItemStatusFailed    = "failed"
	BatchItemStatusCancelled = "cancelled"
)

type BatchJobRecord struct {
	ID               string
	TenantID         string
	ProjectID        string
	UserID           string
	APIKeyID         string
	Status           string
	Endpoint         string
	Model            string
	CompletionWindow string
	TotalItems       int
	CompletedItems   int
	FailedItems      int
	CancelledItems   int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	RequestBody      []byte
	Metadata         []byte
	Error            string
	InProgressAt     int64
	CompletedAt      int64
	FailedAt         int64
	CancelledAt      int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type BatchItemRecord struct {
	ID           string
	JobID        string
	TenantID     string
	Index        int
	CustomID     string
	Status       string
	RequestBody  []byte
	ResponseBody []byte
	Error        string
	ResponseID   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RecoverableBatchItemRecord struct {
	ItemID      string
	JobID       string
	TenantID    string
	ProjectID   string
	UserID      string
	APIKeyID    string
	Endpoint    string
	RequestBody []byte
	UpdatedAt   time.Time
}

type CreateBatchJobParams struct {
	TenantID         string
	ProjectID        string
	UserID           string
	APIKeyID         string
	Endpoint         string
	Model            string
	CompletionWindow string
	TotalItems       int
	RequestBody      []byte
	Metadata         []byte
}

type CreateBatchItemParams struct {
	JobID       string
	TenantID    string
	Index       int
	CustomID    string
	RequestBody []byte
}

type BatchItemUpdate struct {
	Status           string
	ResponseBody     []byte
	ResponseID       string
	Error            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
}
