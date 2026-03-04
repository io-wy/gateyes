package scheduler

import (
	"context"
	"errors"
	"time"
)

var ErrNoAvailableChannel = errors.New("no available channel")

type Request struct {
	Model     string
	TokenID   string
	SessionID string
}

type Decision struct {
	ChannelID     string
	Provider      string
	UpstreamModel string

	// WaitFor > 0 can be used by handlers to return 202 wait plan.
	WaitFor time.Duration
}

type Selector interface {
	Select(ctx context.Context, req Request) (Decision, error)
}
