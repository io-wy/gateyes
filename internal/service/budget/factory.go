package budget

import (
	"log/slog"
	"strings"
)

func NewBackend(
	backend, addr, password string,
	db int,
	prefix, suffix string,
	redisStrict bool,
) (Backend, error) {
	normalizedBackend := strings.ToLower(strings.TrimSpace(backend))
	if normalizedBackend == "redis" {
		service, err := NewRedisBackend(addr, password, db, budgetPrefix(prefix, suffix))
		if err != nil {
			if redisStrict {
				return nil, err
			}
			slog.Warn("failed to init redis budget service, fallback to memory", "error", err)
			return NewMemoryBackend(), nil
		}
		return service, nil
	}
	return NewMemoryBackend(), nil
}

func budgetPrefix(prefix, suffix string) string {
	base := strings.TrimSpace(prefix)
	if base == "" {
		base = "gateyes"
	}
	sfx := strings.TrimSpace(suffix)
	if sfx == "" {
		return base
	}
	return base + ":" + sfx
}
