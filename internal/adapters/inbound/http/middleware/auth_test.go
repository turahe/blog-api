package middleware

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

// recordingAuth records the raw token it was asked to parse and returns err or claims.
type recordingAuth struct {
	authports.Service

	claims authdomain.AccessClaims
	err    error
	seen   *string
}

func (a recordingAuth) ParseAccessToken(raw string) (authdomain.AccessClaims, error) {
	*a.seen = raw
	return a.claims, a.err
}

func TestBearerAuth(t *testing.T) {
	t.Parallel()

	subject := uuid.New()

	tests := []struct {
		name       string
		header     string
		parseErr   error
		wantStatus int
		wantToken  string
		wantMsg    string
	}{
		{name: "missing header", wantStatus: nethttp.StatusUnauthorized, wantMsg: "Authentication required"},
		{name: "basic scheme", header: "Basic dXNlcjpwdw==", wantStatus: nethttp.StatusUnauthorized, wantMsg: "Authentication required"},
		{name: "bearer without separator", header: "Bearertoken", wantStatus: nethttp.StatusUnauthorized, wantMsg: "Authentication required"},
		{
			name: "token fails to parse", header: "Bearer bad-token", parseErr: authdomain.ErrInvalidToken,
			wantStatus: nethttp.StatusUnauthorized, wantToken: "bad-token", wantMsg: "Invalid or expired access token",
		},
		{name: "valid token", header: "Bearer good-token", wantStatus: nethttp.StatusNoContent, wantToken: "good-token"},
		{name: "scheme is case-insensitive and token trimmed", header: "bearer   good-token  ", wantStatus: nethttp.StatusNoContent, wantToken: "good-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gin.SetMode(gin.TestMode)

			var (
				seenToken string
				reached   bool
				userID    uuid.UUID
			)

			auth := recordingAuth{claims: authdomain.AccessClaims{Subject: subject}, err: tt.parseErr, seen: &seenToken}

			router := gin.New()
			router.GET("/x", BearerAuth(auth, nil), func(c *gin.Context) {
				reached = true
				userID, _ = CurrentUserID(c)
				c.Status(nethttp.StatusNoContent)
			})

			req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, tt.wantToken, seenToken)

			if tt.wantStatus != nethttp.StatusNoContent {
				assert.False(t, reached)

				var body struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, responses.ErrorCodeUnauthorized, body.Error.Code)
				assert.Equal(t, tt.wantMsg, body.Error.Message)

				return
			}

			assert.True(t, reached)
			assert.Equal(t, subject, userID)
		})
	}
}

func TestOptionalBearerAuthIgnoresMissingAndInvalidTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		parseErr error
		wantUser bool
	}{
		{name: "no header"},
		{name: "non-bearer scheme", header: "Basic abc"},
		{name: "invalid token", header: "Bearer bad", parseErr: authdomain.ErrInvalidToken},
		{name: "valid token", header: "Bearer good", wantUser: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gin.SetMode(gin.TestMode)

			var (
				seen   string
				signed bool
			)

			auth := recordingAuth{claims: authdomain.AccessClaims{Subject: uuid.New()}, err: tt.parseErr, seen: &seen}

			router := gin.New()
			router.GET("/x", OptionalBearerAuth(auth, nil), func(c *gin.Context) {
				_, signed = CurrentUserID(c)
				c.Status(nethttp.StatusNoContent)
			})

			req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, nethttp.StatusNoContent, w.Code)
			assert.Equal(t, tt.wantUser, signed)
		})
	}
}

func TestCurrentUserID(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	tests := []struct {
		name   string
		value  any
		set    bool
		want   uuid.UUID
		wantOK bool
	}{
		{name: "unset"},
		{name: "wrong type", value: id.String(), set: true},
		{name: "set", value: id, set: true, want: id, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.set {
				c.Set(ContextUserIDKey, tt.value)
			}

			got, ok := CurrentUserID(c)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCurrentSignInFamily(t *testing.T) {
	t.Parallel()

	family := uuid.New()
	impersonation := impersonationClaims()
	impersonation.FamilyID = family

	tests := []struct {
		name  string
		value any
		set   bool
		want  uuid.UUID
	}{
		{name: "no claims", want: uuid.Nil},
		{name: "claims of the wrong type", value: "claims", set: true, want: uuid.Nil},
		{name: "impersonation token", value: impersonation, set: true, want: uuid.Nil},
		{name: "token without a family", value: authdomain.AccessClaims{Subject: uuid.New()}, set: true, want: uuid.Nil},
		{name: "sign-in token", value: authdomain.AccessClaims{Subject: uuid.New(), FamilyID: family}, set: true, want: family},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.set {
				c.Set(ContextClaimsKey, tt.value)
			}

			assert.Equal(t, tt.want, CurrentSignInFamily(c))
		})
	}
}

func TestCurrentImpersonation(t *testing.T) {
	t.Parallel()

	claims := impersonationClaims()
	badSession := impersonationClaims()
	badSession.SessionID = "not-a-uuid"

	tests := []struct {
		name   string
		value  any
		set    bool
		wantOK bool
	}{
		{name: "no claims"},
		{name: "claims of the wrong type", value: 42, set: true},
		{name: "regular token", value: authdomain.AccessClaims{Subject: uuid.New()}, set: true},
		{name: "session id not a uuid", value: badSession, set: true},
		{name: "impersonation token", value: claims, set: true, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.set {
				c.Set(ContextClaimsKey, tt.value)
			}

			imp, ok := CurrentImpersonation(c)
			require.Equal(t, tt.wantOK, ok)

			if !tt.wantOK {
				assert.Equal(t, Impersonation{}, imp)
				return
			}

			assert.Equal(t, *claims.Actor, imp.ActorID)
			assert.Equal(t, claims.SessionID, imp.SessionID.String())
		})
	}
}
