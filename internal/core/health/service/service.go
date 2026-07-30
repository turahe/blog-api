package service

import (
	"context"
	"time"

	"github.com/turahe/blog-api/internal/core/health/domain"
	"github.com/turahe/blog-api/internal/core/health/ports"
)

type Health struct {
	version  string
	checkers []ports.Checker
}

func New(version string, checkers ...ports.Checker) *Health {
	return &Health{version: version, checkers: checkers}
}

func (h *Health) Live() domain.Status {
	return domain.Status{Status: "ok", Version: h.version, Time: time.Now().UTC()}
}

func (h *Health) Ready(ctx context.Context) domain.Status {
	status := domain.Status{
		Status:       "ok",
		Version:      h.version,
		Time:         time.Now().UTC(),
		Dependencies: make([]domain.Dependency, 0, len(h.checkers)),
	}
	for _, checker := range h.checkers {
		dependency := domain.Dependency{Name: checker.Name(), Healthy: true}
		if err := checker.Check(ctx); err != nil {
			dependency.Healthy = false
			dependency.Message = "unavailable"
			status.Status = "degraded"
		}
		status.Dependencies = append(status.Dependencies, dependency)
	}
	return status
}
