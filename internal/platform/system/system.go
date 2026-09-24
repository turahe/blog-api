// Package system provides production implementations of clock and id generator ports.
package system

import (
	"time"

	"github.com/google/uuid"
)

// Clock returns the wall-clock time in UTC.
type Clock struct{}

// Now returns the current UTC time.
func (Clock) Now() time.Time { return time.Now().UTC() }

// UUIDGenerator returns random (v4) UUIDs.
type UUIDGenerator struct{}

// New returns a random UUID.
func (UUIDGenerator) New() uuid.UUID { return uuid.New() }
