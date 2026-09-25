// Package ports declares dependency checkers and the health service interface.
package ports

import (
	"context"

	"github.com/turahe/blog-api/internal/core/health/domain"
)

// Checker probes one named dependency.
type Checker interface {
	Name() string
	Check(context.Context) error
}

// Optional is implemented by checkers whose failure is reported but does not fail readiness,
// because the process keeps serving without that dependency.
type Optional interface {
	Optional() bool
}

// Service reports liveness and readiness.
type Service interface {
	Live() domain.Status
	Ready(context.Context) domain.Status
}
