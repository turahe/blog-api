package service

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

const (
	// saltLoadTimeout bounds the one store call a replica makes per day.
	saltLoadTimeout = time.Second
	// saltRetry is how long a replica waits before asking a failing store again.
	saltRetry = 10 * time.Second
)

// dailySalts caches the salt of the current UTC day. Without a store, or while the store
// fails, a random salt that lives only in this process stands in for the day, so unique
// visitor counts across replicas are approximate until the shared salt loads.
type dailySalts struct {
	store  ports.SaltStore
	logger *slog.Logger

	mu      sync.Mutex
	day     string
	salt    []byte
	shared  bool
	retryAt time.Time
}

func (d *dailySalts) at(ctx context.Context, now time.Time) []byte {
	day := domain.SaltDay(now)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.day == day && (d.shared || d.store == nil || now.Before(d.retryAt)) {
		return d.salt
	}

	if d.store != nil {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saltLoadTimeout)
		salt, err := d.store.DailySalt(loadCtx, day, randomSalt(), domain.SaltKeepFrom(now))

		cancel()

		if err == nil && len(salt) == domain.SaltBytes {
			d.day, d.salt, d.shared = day, salt, true

			return salt
		}

		if err == nil {
			err = errors.New("stored salt has the wrong length")
		}

		if d.logger != nil {
			d.logger.Warn("analytics visitor salt unavailable; using a replica-local salt", "error", err, "retry_in", saltRetry)
		}

		d.retryAt = now.Add(saltRetry)
	}

	if d.day != day {
		d.day, d.salt, d.shared = day, randomSalt(), false
	}

	return d.salt
}

func randomSalt() []byte {
	salt := make([]byte, domain.SaltBytes)
	_, _ = rand.Read(salt)

	return salt
}
