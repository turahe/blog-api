package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

var _ authports.OAuthStateStore = (*OAuthStates)(nil)

const oauthStatePrefix = "oauth:state:"

// OAuthStates keeps pending OAuth authorization requests in Redis, keyed by the
// SHA-256 of the state so a Redis dump does not reveal usable states.
type OAuthStates struct {
	client goredis.Cmdable
}

// NewOAuthStates returns a Redis-backed authports.OAuthStateStore.
func NewOAuthStates(client goredis.Cmdable) *OAuthStates {
	return &OAuthStates{client: client}
}

type oauthRecord struct {
	Provider     string `json:"provider"`
	RedirectURI  string `json:"redirect_uri"`
	CodeVerifier string `json:"code_verifier"`
}

func oauthKey(state string) string {
	sum := sha256.Sum256([]byte(state))
	return oauthStatePrefix + hex.EncodeToString(sum[:])
}

// Save implements authports.OAuthStateStore.
func (s *OAuthStates) Save(ctx context.Context, state string, value authdomain.OAuthState, ttl time.Duration) error {
	payload, err := json.Marshal(oauthRecord(value))
	if err != nil {
		return fmt.Errorf("encode oauth state: %w", err)
	}

	return s.client.Set(ctx, oauthKey(state), payload, ttl).Err()
}

// Consume implements authports.OAuthStateStore; GETDEL makes each state single use.
func (s *OAuthStates) Consume(ctx context.Context, state string) (authdomain.OAuthState, error) {
	if state == "" {
		return authdomain.OAuthState{}, authdomain.ErrOAuthStateInvalid
	}

	raw, err := s.client.GetDel(ctx, oauthKey(state)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return authdomain.OAuthState{}, authdomain.ErrOAuthStateInvalid
	}

	if err != nil {
		return authdomain.OAuthState{}, err
	}

	var rec oauthRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return authdomain.OAuthState{}, fmt.Errorf("decode oauth state: %w", err)
	}

	return authdomain.OAuthState(rec), nil
}
