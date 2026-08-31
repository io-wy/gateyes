package ports

import (
	"context"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

// UsagePort is the reporting and accounting surface exposed to applications.
type UsagePort interface {
	repository.UsageStore
	GetBudgetStatus(context.Context, string, string, string) ([]repository.BudgetStatus, error)
	CreateUsageRecord(context.Context, repository.UsageRecord) error
	GetUsageSummary(context.Context, string) (*repository.UsageStats, error)
	GetUserUsageDetail(context.Context, string, string, time.Time, time.Time) ([]repository.UsageRecord, error)
}

type UsageUseCase = UsagePort
