package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
)

type fakeAuthService struct {
	parseAccessFn func(token string) (authdomain.AccessClaims, error)
}

func (f fakeAuthService) Login(context.Context, string, string, string, string, bool) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}

func (f fakeAuthService) Refresh(context.Context, string, string, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}

func (f fakeAuthService) Logout(context.Context, uuid.UUID, string) error { return nil }

func (f fakeAuthService) ParseAccessToken(token string) (authdomain.AccessClaims, error) {
	return f.parseAccessFn(token)
}

func (f fakeAuthService) ForgotPassword(context.Context, string) error { return nil }

func (f fakeAuthService) CheckResetToken(context.Context, string) (authdomain.ResetTokenValidity, error) {
	return authdomain.ResetTokenValidity{}, nil
}

func (f fakeAuthService) ResetPassword(context.Context, string, string, string) error { return nil }

func (f fakeAuthService) ChangePassword(context.Context, uuid.UUID, string, string, string, bool) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

type fakeRoleLookup struct {
	names []string
}

func (f fakeRoleLookup) ListRoleNames(context.Context, uuid.UUID) ([]string, error) {
	return f.names, nil
}

func TestHealthLive(t *testing.T) {
	t.Parallel()

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger:  slog.New(slog.DiscardHandler),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/health/live", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusOK, recorder.Code)
	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.NotEmpty(t, recorder.Header().Get("X-Request-ID"))

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
}

func TestContractOperationIsRegistered(t *testing.T) {
	t.Parallel()

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger:  slog.New(slog.DiscardHandler),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/auth/oauth/google/callback", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "operation.not_implemented", envelope.Error.Code)
	require.Equal(t, map[string]any{
		"operation_id": "auth.oauth.callback",
		"route_group":  "auth",
		"auth_mode":    "none",
	}, envelope.Error.Details)
}

func TestLoginValidationWithoutAuthService(t *testing.T) {
	t.Parallel()

	// Without Auth wired, login stays a contract stub.
	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger:  slog.New(slog.DiscardHandler),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)
}

func TestAccessLogCarriesRouteGroup(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	router.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/admin/users", nil),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, "admin.users.list", entry["operation_id"])
	require.Equal(t, "admin", entry["route_group"])
	require.Equal(t, "required", entry["auth_mode"])
}

func TestAdminContentRoutesRequireAdminOrEditorRole(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	cases := map[string]struct {
		deps httpadapter.Dependencies
		path string
		body string
	}{
		"categories": {httpadapter.Dependencies{Categories: &categoryservice.CategoryService{}}, "/api/v1/admin/categories", `{"name":"Tech"}`},
		"tags":       {httpadapter.Dependencies{Tags: &tagservice.Service{}}, "/api/v1/admin/tags", `{"name":"Go"}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			deps := tc.deps
			deps.Logger = slog.New(slog.DiscardHandler)
			deps.Health = healthservice.New("test")
			deps.Roles = fakeRoleLookup{names: []string{"author"}}
			deps.Auth = fakeAuthService{
				parseAccessFn: func(token string) (authdomain.AccessClaims, error) {
					require.Equal(t, "test", token)
					return authdomain.AccessClaims{Subject: userID}, nil
				},
			}

			router, err := httpadapter.NewRouter(deps)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer test")
			router.ServeHTTP(recorder, request)

			require.Equal(t, nethttp.StatusForbidden, recorder.Code)

			var envelope responses.Envelope
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.False(t, envelope.OK)
			require.Equal(t, "forbidden", envelope.Error.Code)
		})
	}
}
