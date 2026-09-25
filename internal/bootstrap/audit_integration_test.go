package bootstrap_test

import (
	"context"
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	"gorm.io/gorm"
)

func TestLoginAttemptsAppearInMyActivity(t *testing.T) {
	t.Parallel()

	var recorder *auditservice.Recorder

	s := newAuthStackWith(t, func(tx *gorm.DB, deps *httpadapter.Dependencies) {
		repo := persistence.NewAuditRepository(tx)
		// One flush on Close keeps the recorder off the transaction while requests run.
		recorder = auditservice.NewRecorder(repo, slog.New(slog.DiscardHandler), auditservice.RecorderOptions{FlushInterval: time.Hour})
		deps.Audit, deps.Activity = recorder, auditservice.NewActivity(repo)
	})

	r := s.do(t, nethttp.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": s.email, "password": "wrong password"})
	require.Equal(t, nethttp.StatusUnauthorized, r.status)

	access, _ := s.login(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, recorder.Close(ctx))

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/me/activity?category=login", nil)
	req.Header.Set("Authorization", "Bearer "+access)

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	var body struct {
		Data []struct {
			Action   string  `json:"action"`
			Category string  `json:"category"`
			Result   string  `json:"result"`
			IPPrefix *string `json:"ip_prefix"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)

	results := []string{body.Data[0].Result, body.Data[1].Result}
	require.ElementsMatch(t, []string{"success", "failure"}, results)

	for _, item := range body.Data {
		require.Equal(t, "auth.login", item.Action)
		require.Equal(t, "login", item.Category)
	}
}
