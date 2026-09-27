package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// fakeUserRepo is the userports.Repository behind a real UserService.
type fakeUserRepo struct {
	user  userdomain.User
	users []userdomain.User
	total int64
	err   error

	foundID       uuid.UUID
	page, perPage int
}

func (f *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (userdomain.User, error) {
	f.foundID = id
	return f.user, f.err
}

func (f *fakeUserRepo) List(_ context.Context, page, perPage int) ([]userdomain.User, int64, error) {
	f.page, f.perPage = page, perPage
	return f.users, f.total, f.err
}

func sampleUser() userdomain.User {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	return userdomain.User{
		UUID: uuid.New(), Username: "ada", Email: "ada@example.com", FullName: "Ada",
		Status: userdomain.StatusActive, CreatedAt: at, UpdatedAt: at,
	}
}

func TestMeGetHandler(t *testing.T) {
	t.Parallel()

	user := sampleUser()
	tests := []struct {
		name    string
		anon    bool
		err     error
		status  int
		code    string
		message string
	}{
		{name: "anonymous", anon: true, status: nethttp.StatusUnauthorized, code: "unauthorized", message: "Authentication required"},
		{name: "deleted account", err: userdomain.ErrNotFound, status: nethttp.StatusUnauthorized, code: "unauthorized", message: "User not found"},
		{name: "repository failure", err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to load user"},
		{name: "returns the caller", status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeUserRepo{user: user, err: tc.err}
			req := profileRequest{method: nethttp.MethodGet, target: "/api/v1/me"}
			if !tc.anon {
				req.user = &user.UUID
			}

			w, body := runProfile(t, meGetHandler(userservice.New(repo)), req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, user.UUID, repo.foundID)
				require.Equal(t, user.UUID.String(), dataOf(body)["id"])
				require.Equal(t, "ada@example.com", dataOf(body)["email"])
			}
		})
	}
}

func TestAdminUsersListHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		query                 string
		err                   error
		status                int
		wantPage, wantPerPage int
	}{
		{name: "defaults", query: "", status: nethttp.StatusOK, wantPage: 1, wantPerPage: 20},
		{name: "explicit page", query: "?page=3&perPage=5", status: nethttp.StatusOK, wantPage: 3, wantPerPage: 5},
		{name: "out of range values are clamped", query: "?page=0&perPage=500", status: nethttp.StatusOK, wantPage: 1, wantPerPage: 20},
		{name: "repository failure", query: "", err: errors.New("db down"), status: nethttp.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeUserRepo{users: []userdomain.User{sampleUser(), sampleUser()}, total: 42, err: tc.err}
			w, body := runProfile(t, adminUsersListHandler(userservice.New(repo)), profileRequest{
				method: nethttp.MethodGet, target: "/api/v1/admin/users" + tc.query,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())

			if tc.err != nil {
				require.Equal(t, "internal_error", errorCode(body))
				require.Equal(t, "Failed to list users", errorMessage(body))

				return
			}

			require.Equal(t, tc.wantPage, repo.page)
			require.Equal(t, tc.wantPerPage, repo.perPage)

			data, ok := body["data"].([]any)
			require.True(t, ok)
			require.Len(t, data, 2)

			meta, ok := body["meta"].(map[string]any)
			require.True(t, ok)
			require.InDelta(t, 42, meta["total"], 0)
			require.InDelta(t, tc.wantPage, meta["currentPage"], 0)
			require.InDelta(t, tc.wantPerPage, meta["perPage"], 0)
		})
	}
}
