package administration

import (
	"context"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/ports"
)

// RuntimeConfigAdapter keeps the config reloader behind a small application
// port for admin handlers.
type RuntimeConfigAdapter struct{ reloader *config.Reloader }

func NewRuntimeConfig(reloader *config.Reloader) *RuntimeConfigAdapter {
	return &RuntimeConfigAdapter{reloader: reloader}
}

func (a *RuntimeConfigAdapter) Reload(ctx context.Context) error {
	if a == nil || a.reloader == nil {
		return nil
	}
	return a.reloader.Reload(ctx)
}

var _ ports.RuntimeConfigPort = (*RuntimeConfigAdapter)(nil)
