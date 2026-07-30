package service

import (
	"context"
	"strings"

	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	"github.com/turahe/blog-api/internal/core/category/ports"
)

type CategoryService struct {
	repo ports.Repository
}

func New(repo ports.Repository) *CategoryService {
	return &CategoryService{repo: repo}
}

func (s *CategoryService) List(ctx context.Context) ([]categorydomain.Category, error) {
	return s.repo.List(ctx)
}

func (s *CategoryService) GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	return s.repo.GetBySlug(ctx, slug)
}
