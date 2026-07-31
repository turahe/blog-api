package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
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

func TestHealthLive(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodGet, "/health/live", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusOK, recorder.Code)
	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.NotEmpty(t, recorder.Header().Get("X-Request-ID"))

	var envelope Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
}

func TestContractOperationIsRegistered(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/oauth/google/callback", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)

	var envelope Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "operation.not_implemented", envelope.Error.Code)
	require.Equal(t, map[string]any{
		"operation_id": "auth.oauth.callback",
		"route_group":  "auth",
		"auth_mode":    "none",
	}, envelope.Error.Details)
}

func TestLoginValidationWithoutAuthService(t *testing.T) {
	// Without Auth wired, login stays a contract stub.
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)
}

func TestAccessLogCarriesRouteGroup(t *testing.T) {
	var logs bytes.Buffer
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	router.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(nethttp.MethodGet, "/api/v1/admin/users", nil),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, "admin.users.list", entry["operation_id"])
	require.Equal(t, "admin", entry["route_group"])
	require.Equal(t, "required", entry["auth_mode"])
}

func TestAdminTagsRouteRequiresAdminOrEditorRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	router, err := NewRouter(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health: healthservice.New("test"),
		Auth: fakeAuthService{
			parseAccessFn: func(token string) (authdomain.AccessClaims, error) {
				require.Equal(t, "test", token)
				return authdomain.AccessClaims{Subject: userID}, nil
			},
		},
		Tags:  &tagservice.Service{},
		Roles: fakeRoleLookup{names: []string{"author"}},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/tags", bytes.NewBufferString(`{"name":"Go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer test")
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusForbidden, recorder.Code)

	var envelope Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "forbidden", envelope.Error.Code)
}
