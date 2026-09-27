package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakeUserRepo struct {
	users       map[uuid.UUID]userdomain.User
	listErr     error
	page, limit int
}

func (r *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (userdomain.User, error) {
	user, ok := r.users[id]
	if !ok {
		return userdomain.User{}, userdomain.ErrNotFound
	}

	return user, nil
}

func (r *fakeUserRepo) List(_ context.Context, page, perPage int) ([]userdomain.User, int64, error) {
	r.page, r.limit = page, perPage
	if r.listErr != nil {
		return nil, 0, r.listErr
	}

	out := make([]userdomain.User, 0, len(r.users))
	for _, user := range r.users {
		out = append(out, user)
	}

	return out, int64(len(out)), nil
}

func TestUserServiceGetByID(t *testing.T) {
	t.Parallel()

	user := userdomain.User{UUID: uuid.New(), Username: "ada"}
	svc := New(&fakeUserRepo{users: map[uuid.UUID]userdomain.User{user.UUID: user}})

	got, err := svc.GetByID(t.Context(), user.UUID)
	require.NoError(t, err)
	assert.Equal(t, user, got)

	_, err = svc.GetByID(t.Context(), uuid.New())
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

func TestUserServiceListNormalisesPaging(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		page, perPage     int
		wantPage, wantPer int
	}{
		{name: "defaults", page: 0, perPage: 0, wantPage: 1, wantPer: 20},
		{name: "negative", page: -3, perPage: -1, wantPage: 1, wantPer: 20},
		{name: "too many per page", page: 2, perPage: 101, wantPage: 2, wantPer: 20},
		{name: "in range", page: 3, perPage: 100, wantPage: 3, wantPer: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeUserRepo{users: map[uuid.UUID]userdomain.User{uuid.New(): {Username: "ada"}}}

			users, total, err := New(repo).List(t.Context(), tt.page, tt.perPage)
			require.NoError(t, err)
			assert.Len(t, users, 1)
			assert.Equal(t, int64(1), total)
			assert.Equal(t, [2]int{tt.wantPage, tt.wantPer}, [2]int{repo.page, repo.limit})
		})
	}
}

func TestUserServiceListPassesThroughFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	_, _, err := New(&fakeUserRepo{listErr: boom}).List(t.Context(), 1, 20)
	require.ErrorIs(t, err, boom)
}
