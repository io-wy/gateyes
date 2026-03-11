package budget

import (
	"log/slog"
	"net/http"

	"gateyes/internal/dto"
	"gateyes/internal/service/usage"
)

func adjustTokenCounters(
	req *http.Request,
	backend Backend,
	statusCode int,
	estimate usage.TokenUsage,
	actual usage.TokenUsage,
	tokenCounters []dto.BudgetCounter,
	rollbackLog string,
	adjustLog string,
) error {
	if len(tokenCounters) == 0 || estimate.TotalTokens <= 0 {
		return nil
	}

	if actual.TotalTokens <= 0 && statusCode >= http.StatusBadRequest {
		if err := backend.Adjust(req.Context(), toAdjustments(tokenCounters, -estimate.TotalTokens)); err != nil {
			slog.Warn(rollbackLog, "error", err)
			return err
		}
		return nil
	}
	if actual.TotalTokens <= 0 {
		return nil
	}

	delta := actual.TotalTokens - estimate.TotalTokens
	if delta == 0 {
		return nil
	}
	if err := backend.Adjust(req.Context(), toAdjustments(tokenCounters, delta)); err != nil {
		slog.Warn(adjustLog, "error", err)
		return err
	}
	return nil
}

func toAdjustments(counters []dto.BudgetCounter, delta int64) []dto.BudgetAdjustment {
	adjustments := make([]dto.BudgetAdjustment, 0, len(counters))
	for _, counter := range counters {
		adjustments = append(adjustments, dto.BudgetAdjustment{
			Key:   counter.Key,
			Delta: delta,
			TTL:   counter.TTL,
		})
	}
	return adjustments
}
