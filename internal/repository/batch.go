package repository

import "time"

const (
	BatchStatusPending   = "pending"
	BatchStatusRunning   = "running"
	BatchStatusCompleted = "completed"
	BatchStatusFailed    = "failed"

	BatchItemStatusPending   = "pending"
	BatchItemStatusRunning   = "running"
	BatchItemStatusCompleted = "completed"
	BatchItemStatusFailed    = "failed"
)

type BatchJobRecord struct {
	ID             string
	TenantID       string
	ProjectID      string
	UserID         string
	APIKeyID       string
	Status         string
	Endpoint       string
	Model          string
	TotalItems     int
	CompletedItems int
	FailedItems    int
	RequestBody    []byte
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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

type CreateBatchJobParams struct {
	TenantID    string
	ProjectID   string
	UserID      string
	APIKeyID    string
	Endpoint    string
	Model       string
	TotalItems  int
	RequestBody []byte
}

type CreateBatchItemParams struct {
	JobID       string
	TenantID    string
	Index       int
	CustomID    string
	RequestBody []byte
}

type BatchItemUpdate struct {
	Status       string
	ResponseBody []byte
	ResponseID   string
	Error        string
}
