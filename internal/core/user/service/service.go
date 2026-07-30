package service

import (
	"context"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/core/user/ports"
)

type UserService struct {
	repo ports.Repository
}

func New(repo ports.Repository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (userdomain.User, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *UserService) List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return s.repo.List(ctx, page, perPage)
}
