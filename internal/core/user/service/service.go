// Package service implements read access to user accounts.
package service

import (
	"context"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/core/user/ports"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

// UserService implements ports.Service.
type UserService struct {
	repo ports.Repository
}

// New returns a UserService.
func New(repo ports.Repository) *UserService {
	return &UserService{repo: repo}
}

// GetByID returns the user with the given UUID.
func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (userdomain.User, error) {
	return s.repo.FindByID(ctx, id)
}

// List returns a page of users.
func (s *UserService) List(ctx context.Context, filter userdomain.ListFilter) (userdomain.ListResult, error) {
	pr := &filter.PageRequest
	if pr.Limit < 1 || pr.Limit > pagination.DefaultMaxPerPage {
		pr.Limit = pagination.DefaultPerPage
	}
	if pr.Page < 1 {
		pr.Page = 1
	}
	if pr.Mode == pagination.ModeOffset {
		pr.Offset = (pr.Page - 1) * pr.Limit
		pr.Forward = true
	}

	return s.repo.List(ctx, filter)
}
