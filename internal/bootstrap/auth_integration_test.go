package bootstrap_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"github.com/turahe/blog-api/internal/platform/security/jwt"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	cyclePassword = "correct horse battery staple"
	refreshTTL    = 30 * 24 * time.Hour
	shortTTL      = 7 * 24 * time.Hour // a login without "remember me"
)

var (
	dbOnce sync.Once
	testDB *gorm.DB
	errDB  error
)

// testClock is a settable clock shared by the service under test.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

type uuidGen struct{}

func (uuidGen) New() uuid.UUID { return uuid.New() }

// authStack is the HTTP router over a real AuthService, PostgreSQL repositories (inside a
// rolled-back transaction), ES256 tokens and Argon2id hashing.
type authStack struct {
	router nethttp.Handler
	tx     *gorm.DB
	clock  *testClock
	email  string
}

func newAuthStack(t *testing.T) *authStack {
	t.Helper()

	tx := migratedTx(t)
	clock := &testClock{t: time.Now().UTC()}
	tokens := newTokens(t)
	hasher := password.New()

	hash, err := hasher.Hash(cyclePassword)
	require.NoError(t, err)

	name := "cycle" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	email := name + "@example.test"
	require.NoError(t, tx.Exec("INSERT INTO users (email, username, full_name, password_hash) VALUES (?, ?, ?, ?)",
		email, name, name, hash).Error)

	auth := authservice.New(persistence.NewUserRepository(tx), persistence.NewSessionRepository(tx),
		persistence.NewResetTokenRepository(tx), hasher, tokens, clock, uuidGen{},
		authservice.Config{AccessTTL: 15 * time.Minute, RefreshTTL: refreshTTL}, nil)

	gin.SetMode(gin.TestMode)

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger: slog.New(slog.DiscardHandler), Health: healthservice.New("test"), Version: "test", Auth: auth,
	})
	require.NoError(t, err)

	return &authStack{router: router, tx: tx, clock: clock, email: email}
}

func migratedTx(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping auth integration test against PostgreSQL")
	}

	dbOnce.Do(func() {
		sqlDB, err := sql.Open("pgx", dsn)
		if err != nil {
			errDB = err
			return
		}

		if err = migrations.Up(sqlDB); err != nil {
			errDB = err
			return
		}

		testDB, errDB = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}),
			&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	})
	require.NoError(t, errDB)

	tx := testDB.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { tx.Rollback() })

	return tx
}

func newTokens(t *testing.T) *jwt.Service {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	tokens, err := jwt.New(
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		strings.Repeat("k", 32), "blog-api-test")
	require.NoError(t, err)

	return tokens
}

type reply struct {
	status int
	data   map[string]any
	code   string
}

func (s *authStack) do(t *testing.T, method, path, bearer string, body any) reply {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")

	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	var envelope struct {
		Data  map[string]any `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())

	return reply{status: w.Code, data: envelope.Data, code: envelope.Error.Code}
}

func (s *authStack) login(t *testing.T) (access, refresh string) {
	t.Helper()
	return s.loginWith(t, false)
}

func (s *authStack) loginWith(t *testing.T, remember bool) (access, refresh string) {
	t.Helper()

	r := s.do(t, nethttp.MethodPost, "/api/v1/auth/login", "",
		map[string]any{"email": s.email, "password": cyclePassword, "remember": remember})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	access, _ = r.data["access_token"].(string)
	refresh, _ = r.data["refresh_token"].(string)

	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)

	return access, refresh
}

func (s *authStack) refresh(t *testing.T, token string) reply {
	t.Helper()
	return s.do(t, nethttp.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": token})
}

func (s *authStack) liveSessions(t *testing.T) int64 {
	t.Helper()

	var n int64
	require.NoError(t, s.tx.Raw(`SELECT count(*) FROM refresh_sessions rs JOIN users u ON u.id = rs.user_id
		WHERE u.email = ? AND rs.revoked_at IS NULL`, s.email).Row().Scan(&n))

	return n
}

// newestLifetime is expires_at - created_at of the user's most recent live session.
func (s *authStack) newestLifetime(t *testing.T) time.Duration {
	t.Helper()

	var seconds float64
	require.NoError(t, s.tx.Raw(`SELECT extract(epoch FROM rs.expires_at - rs.created_at)
		FROM refresh_sessions rs JOIN users u ON u.id = rs.user_id
		WHERE u.email = ? AND rs.revoked_at IS NULL ORDER BY rs.id DESC LIMIT 1`, s.email).Row().Scan(&seconds))

	return time.Duration(seconds * float64(time.Second)).Round(time.Second)
}

func TestAuthRefreshKeepsSessionLifetime(t *testing.T) {
	t.Parallel()

	s := newAuthStack(t)

	_, short := s.loginWith(t, false)
	require.Equal(t, shortTTL, s.newestLifetime(t))

	r := s.refresh(t, short)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, shortTTL, s.newestLifetime(t), "rotation does not extend a short session")

	_, long := s.loginWith(t, true)
	require.Equal(t, refreshTTL, s.newestLifetime(t))

	r = s.refresh(t, long)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, refreshTTL, s.newestLifetime(t))
}

func TestAuthLoginRefreshLogoutCycle(t *testing.T) {
	t.Parallel()

	s := newAuthStack(t)

	r := s.do(t, nethttp.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": s.email, "password": "wrong password"})
	require.Equal(t, nethttp.StatusUnauthorized, r.status)
	require.Equal(t, "unauthorized", r.code)

	_, refresh := s.login(t)
	require.Equal(t, int64(1), s.liveSessions(t))

	r = s.refresh(t, refresh)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	access, _ := r.data["access_token"].(string)
	rotated, _ := r.data["refresh_token"].(string)
	require.NotEqual(t, refresh, rotated, "refresh rotates the token")
	require.Equal(t, int64(1), s.liveSessions(t), "the old session is replaced, not kept")

	r = s.do(t, nethttp.MethodPost, "/api/v1/auth/logout", "", map[string]any{"refresh_token": rotated})
	require.Equal(t, nethttp.StatusUnauthorized, r.status, "logout requires the access token")

	r = s.do(t, nethttp.MethodPost, "/api/v1/auth/logout", access, map[string]any{"refresh_token": rotated})
	require.Equal(t, nethttp.StatusOK, r.status, r.code)
	require.Equal(t, int64(0), s.liveSessions(t))

	r = s.refresh(t, rotated)
	require.Equal(t, nethttp.StatusUnauthorized, r.status, "a logged-out refresh token is dead")
	require.Equal(t, "unauthorized", r.code)
}

func TestAuthRefreshRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	s := newAuthStack(t)
	_, refresh := s.login(t)

	s.clock.advance(shortTTL + time.Second)

	r := s.refresh(t, refresh)
	require.Equal(t, nethttp.StatusUnauthorized, r.status)
	require.Equal(t, "unauthorized", r.code)
	require.Equal(t, int64(1), s.liveSessions(t), "expiry does not revoke the family")
}

func TestAuthRefreshReuseRevokesFamily(t *testing.T) {
	t.Parallel()

	s := newAuthStack(t)
	_, first := s.login(t)
	_, other := s.login(t)

	r := s.refresh(t, first)
	require.Equal(t, nethttp.StatusOK, r.status, r.code)

	second, _ := r.data["refresh_token"].(string)

	r = s.refresh(t, first)
	require.Equal(t, nethttp.StatusUnauthorized, r.status, "a rotated token cannot be reused")
	require.Equal(t, "unauthorized", r.code)

	r = s.refresh(t, second)
	require.Equal(t, nethttp.StatusUnauthorized, r.status, "reuse revokes the token's successor too")

	r = s.refresh(t, other)
	require.Equal(t, nethttp.StatusOK, r.status, "other sessions (families) are untouched")
}

func TestAuthRefreshRejectsUnknownToken(t *testing.T) {
	t.Parallel()

	s := newAuthStack(t)

	for _, token := range []string{"not-a-real-token", "   "} {
		r := s.refresh(t, token)
		require.Equal(t, nethttp.StatusUnauthorized, r.status, token)
	}
}
