package ports

import (
	"context"

	"github.com/turahe/blog-api/internal/core/health/domain"
)

type Checker interface {
	Name() string
	Check(context.Context) error
}

type Service interface {
	Live() domain.Status
	Ready(context.Context) domain.Status
}
