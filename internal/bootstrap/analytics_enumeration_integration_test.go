package bootstrap_test

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ingestAnswer is everything a caller learns from an ingest response except the event id,
// which is random or echoed back.
type ingestAnswer struct {
	Status       int
	Code         string
	ErrorCode    string
	DataKeys     []string
	CacheControl string
}

func (s *analyticsStack) answer(t *testing.T, path string, body any, headers map[string]string) ingestAnswer {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUA)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	var envelope struct {
		Code  json.Number    `json:"code"`
		Data  map[string]any `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())

	keys := make([]string, 0, len(envelope.Data))
	for k := range envelope.Data {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return ingestAnswer{
		Status: w.Code, Code: envelope.Code.String(), ErrorCode: envelope.Error.Code, DataKeys: keys,
		CacheControl: w.Header().Get("Cache-Control"),
	}
}

// requireSameAnswers asserts every request in cases gets the answer of the first one.
func requireSameAnswers(t *testing.T, s *analyticsStack, path string, cases map[string]func() (any, map[string]string)) ingestAnswer {
	t.Helper()

	var (
		want  ingestAnswer
		first string
	)

	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		body, headers := cases[name]()
		got := s.answer(t, path, body, headers)

		if first == "" {
			want, first = got, name
			continue
		}

		assert.Equal(t, want, got, "%s answers %s like %s", path, name, first)
	}

	return want
}

// TestAnalyticsIngestCannotEnumerateContentOrUsers checks that nothing a caller sends to the
// ingest routes changes the answer based on what exists: paths and result ids are never
// looked up, bearer tokens are ignored, and consent tokens are answered alike whether unknown,
// refused, or missing.
//
//nolint:paralleltest // subtests share one transaction and flip the consent setting in turn
func TestAnalyticsIngestCannotEnumerateContentOrUsers(t *testing.T) {
	t.Parallel()

	s := newAnalyticsStack(t)
	access, _ := s.login(t)

	var user struct {
		UUID     uuid.UUID
		Username string
	}
	require.NoError(t, s.tx.Raw("SELECT uuid, username FROM users WHERE email = ?", s.email).Scan(&user).Error)

	granted, refused := s.consentToken(t, true), s.consentToken(t, false)
	unknown := strings.Repeat("a", len(granted))
	session := uuid.NewString

	t.Run("consent tokens while consent is required", func(t *testing.T) {
		got := requireSameAnswers(t, s, "/api/v1/analytics/ingest/page-view", map[string]func() (any, map[string]string){
			"no token": func() (any, map[string]string) { return map[string]any{"sessionId": session(), "path": "/"}, nil },
			"unknown token": func() (any, map[string]string) {
				return map[string]any{"sessionId": session(), "path": "/"}, map[string]string{"X-Consent-Token": unknown}
			},
			"refused token": func() (any, map[string]string) {
				return map[string]any{"sessionId": session(), "path": "/"}, map[string]string{"X-Consent-Token": refused}
			},
		})
		assert.Equal(t, nethttp.StatusForbidden, got.Status)
		assert.Equal(t, "analytics.consent_required", got.ErrorCode)
	})

	require.NoError(t, s.tx.Exec(`INSERT INTO settings (key, value) VALUES ('analytics.consent_required', 'false')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`).Error)

	t.Run("consent tokens while consent is optional", func(t *testing.T) {
		cases := map[string]func() (any, map[string]string){}
		for name, token := range map[string]string{"no token": "", "unknown token": unknown, "refused token": refused, "granted token": granted} {
			cases[name] = func() (any, map[string]string) {
				return map[string]any{"sessionId": session(), "path": "/"}, map[string]string{"X-Consent-Token": token}
			}
		}

		got := requireSameAnswers(t, s, "/api/v1/analytics/ingest/page-view", cases)
		assert.Equal(t, nethttp.StatusAccepted, got.Status)
		assert.Equal(t, []string{"id"}, got.DataKeys)
		assert.Equal(t, "no-store", got.CacheControl)
	})

	t.Run("bearer tokens are ignored", func(t *testing.T) {
		cases := map[string]func() (any, map[string]string){}
		for name, auth := range map[string]string{"none": "", "valid": "Bearer " + access, "garbage": "Bearer not-a-token"} {
			cases[name] = func() (any, map[string]string) {
				return map[string]any{"sessionId": session(), "path": "/"}, map[string]string{"Authorization": auth}
			}
		}

		assert.Equal(t, nethttp.StatusAccepted, requireSameAnswers(t, s, "/api/v1/analytics/ingest/page-view", cases).Status)
	})

	t.Run("paths are never looked up", func(t *testing.T) {
		for _, route := range []string{"page-view", "navigation", "time-spent"} {
			cases := map[string]func() (any, map[string]string){}
			for name, path := range map[string]string{"existing user": "/users/" + user.Username, "missing user": "/users/nobody-" + session()[:8]} {
				cases[name] = func() (any, map[string]string) {
					body := map[string]any{"sessionId": session(), "path": path, "to": path, "transition": "internal", "from": "/"}
					if route == "time-spent" {
						body["viewId"], body["focusSeconds"] = uuid.NewString(), 5
					}

					return body, nil
				}
			}

			got := requireSameAnswers(t, s, "/api/v1/analytics/ingest/"+route, cases)
			assert.Equal(t, nethttp.StatusAccepted, got.Status, route)
		}
	})

	t.Run("result ids are never looked up", func(t *testing.T) {
		cases := map[string]func() (any, map[string]string){}
		for name, id := range map[string]uuid.UUID{"existing user id": user.UUID, "random id": uuid.New()} {
			cases[name] = func() (any, map[string]string) {
				return map[string]any{
					"sessionId": session(), "searchId": uuid.NewString(), "position": 1, "resourceType": "post", "resourceId": id,
				}, nil
			}
		}

		assert.Equal(t, nethttp.StatusAccepted, requireSameAnswers(t, s, "/api/v1/analytics/ingest/search-click", cases).Status)
	})
}
