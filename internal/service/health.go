package service

import (
	"context"
	"time"
)

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	BuildType string `json:"build_type"`
}

type HealthService struct {
	buildInfo BuildInfo
	now       func() time.Time
}

func NewHealthService(buildInfo BuildInfo) *HealthService {
	return &HealthService{
		buildInfo: buildInfo,
		now:       time.Now,
	}
}

type HealthStatus struct {
	Status string    `json:"status"`
	Time   string    `json:"time"`
	Build  BuildInfo `json:"build"`
}

func (s *HealthService) Status(_ context.Context) HealthStatus {
	return HealthStatus{
		Status: "ok",
		Time:   s.now().UTC().Format(time.RFC3339),
		Build:  s.buildInfo,
	}
}
