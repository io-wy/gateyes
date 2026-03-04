package scheduler

import (
	"context"
	"strings"
)

type StaticSelector struct {
	DefaultChannelID string
	DefaultProvider  string
	DefaultModel     string
}

func NewStaticSelector(defaultChannelID, defaultProvider, defaultModel string) *StaticSelector {
	return &StaticSelector{
		DefaultChannelID: defaultChannelID,
		DefaultProvider:  defaultProvider,
		DefaultModel:     defaultModel,
	}
}

func (s *StaticSelector) Select(_ context.Context, req Request) (Decision, error) {
	if strings.TrimSpace(s.DefaultChannelID) == "" {
		return Decision{}, ErrNoAvailableChannel
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = s.DefaultModel
	}

	// TODO(io): replace static strategy with sticky -> priority/weight -> wait-plan.
	return Decision{
		ChannelID:     s.DefaultChannelID,
		Provider:      s.DefaultProvider,
		UpstreamModel: model,
	}, nil
}
