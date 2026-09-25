// Package domain holds analytics consent records: a pseudonymous per-browser subject and
// its decision for each purpose.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNotFound means the consent or subject does not exist, or the caller cannot prove
	// it owns it.
	ErrNotFound = errors.New("consent not found")
	// ErrValidation means the request is malformed.
	ErrValidation = errors.New("validation error")
)

// Purpose is what a consent allows.
type Purpose string

// Purposes. Analytics allows pseudonymous measurement tied to the subject token only;
// authenticated analytics additionally links the subject to the signed-in user.
const (
	PurposeAnalytics              Purpose = "analytics"
	PurposeAuthenticatedAnalytics Purpose = "authenticated_analytics"
)

// Purposes lists every purpose in display order.
var Purposes = []Purpose{PurposeAnalytics, PurposeAuthenticatedAnalytics}

// Status is a purpose's current decision. Rejected means it was never granted; withdrawn
// means it was granted and later revoked.
type Status string

// Statuses.
const (
	StatusGranted   Status = "granted"
	StatusRejected  Status = "rejected"
	StatusWithdrawn Status = "withdrawn"
)

// Subject is one browser's consent identity. Its token is a bearer secret held by the
// client; only its hash is stored.
type Subject struct {
	UUID       uuid.UUID
	UserUUID   *uuid.UUID
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Consent is a subject's decision for one purpose.
type Consent struct {
	UUID          uuid.UUID
	SubjectUUID   uuid.UUID
	Purpose       Purpose
	Status        Status
	PolicyVersion string
	DecidedAt     time.Time
	WithdrawnAt   *time.Time
}

// Granted reports whether the consent currently allows its purpose.
func (c Consent) Granted() bool { return c.Status == StatusGranted }

// Decide returns the consent after a new decision: granting sets granted, and refusing a
// granted purpose withdraws it, otherwise it is rejected.
func (c Consent) Decide(granted bool, policyVersion string, at time.Time) Consent {
	c.PolicyVersion = policyVersion
	c.DecidedAt = at

	switch {
	case granted:
		c.Status = StatusGranted
		c.WithdrawnAt = nil
	case c.Status == StatusGranted:
		c.Status = StatusWithdrawn
		c.WithdrawnAt = &at
	case c.Status == "":
		c.Status = StatusRejected
	}

	return c
}

// ValidPurpose reports whether p is a known purpose.
func ValidPurpose(p Purpose) bool { return slices.Contains(Purposes, p) }

var policyVersionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// ValidPolicyVersion reports whether v is 1-32 letters, digits, dots, underscores, or hyphens.
func ValidPolicyVersion(v string) bool { return policyVersionPattern.MatchString(v) }

// tokenBytes is the subject token's entropy.
const tokenBytes = 32

// NewToken returns a random subject token and its storage hash.
func NewToken() (string, string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}

	token := base64.RawURLEncoding.EncodeToString(raw)

	return token, HashToken(token), nil
}

// HashToken is the stored form of a subject token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
