// Package challenge stores pending two-factor logins in Redis.
package challenge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

var _ authports.ChallengeStore = (*Store)(nil)

const (
	keyPrefix     = "2fa:challenge:"
	fieldLogin    = "login"
	fieldAttempts = "attempts"
)

// attemptScript increments the counter only while the challenge exists, so an
// attempt on an expired challenge cannot recreate it without a TTL.
var attemptScript = goredis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 0 then return -1 end
return redis.call("HINCRBY", KEYS[1], ARGV[1], 1)
`)

// Store keeps each challenge as a Redis hash that expires with the challenge.
type Store struct {
	client goredis.Cmdable
}

// New returns a Redis-backed authports.ChallengeStore.
func New(client goredis.Cmdable) *Store {
	return &Store{client: client}
}

type record struct {
	UserUUID  string `json:"user_uuid"`
	Remember  bool   `json:"remember"`
	UserAgent string `json:"user_agent,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
}

// Save implements authports.ChallengeStore.
func (s *Store) Save(ctx context.Context, tokenHash string, login authdomain.PendingLogin, ttl time.Duration) error {
	payload, err := json.Marshal(record{
		UserUUID: login.UserUUID.String(), Remember: login.Remember,
		UserAgent: login.UserAgent, IPAddress: login.IPAddress,
	})
	if err != nil {
		return fmt.Errorf("encode challenge: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, keyPrefix+tokenHash, fieldLogin, payload, fieldAttempts, 0)
	pipe.PExpire(ctx, keyPrefix+tokenHash, ttl)
	_, err = pipe.Exec(ctx)

	return err
}

// Get implements authports.ChallengeStore.
func (s *Store) Get(ctx context.Context, tokenHash string) (authdomain.PendingLogin, error) {
	raw, err := s.client.HGet(ctx, keyPrefix+tokenHash, fieldLogin).Bytes()
	if errors.Is(err, goredis.Nil) {
		return authdomain.PendingLogin{}, authdomain.ErrChallengeInvalid
	}

	if err != nil {
		return authdomain.PendingLogin{}, err
	}

	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return authdomain.PendingLogin{}, fmt.Errorf("decode challenge: %w", err)
	}

	login := authdomain.PendingLogin{Remember: rec.Remember, UserAgent: rec.UserAgent, IPAddress: rec.IPAddress}
	if err := login.UserUUID.UnmarshalText([]byte(rec.UserUUID)); err != nil {
		return authdomain.PendingLogin{}, fmt.Errorf("decode challenge user: %w", err)
	}

	return login, nil
}

// Attempt implements authports.ChallengeStore.
func (s *Store) Attempt(ctx context.Context, tokenHash string) (int, error) {
	n, err := attemptScript.Run(ctx, s.client, []string{keyPrefix + tokenHash}, fieldAttempts).Int()
	if err != nil {
		return 0, err
	}

	if n < 0 {
		return 0, authdomain.ErrChallengeInvalid
	}

	return n, nil
}

// Consume implements authports.ChallengeStore.
func (s *Store) Consume(ctx context.Context, tokenHash string) (bool, error) {
	n, err := s.client.Del(ctx, keyPrefix+tokenHash).Result()
	if err != nil {
		return false, err
	}

	return n == 1, nil
}
