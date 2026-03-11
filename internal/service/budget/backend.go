package budget

import (
	"context"
	"time"

	"gateyes/internal/dto"
)

type Backend interface {
	Consume(ctx context.Context, counters []dto.BudgetCounter) (dto.BudgetConsumeResult, error)
	Adjust(ctx context.Context, adjustments []dto.BudgetAdjustment) error
}

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return time.Minute
	}
	return ttl
}
