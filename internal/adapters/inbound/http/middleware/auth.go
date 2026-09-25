// Package middleware provides Gin middleware: authentication, request ids, access logs, and rate limits.
package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
)

// Gin context keys set by Auth for downstream handlers.
const (
	ContextUserIDKey    = "auth_user_id"
	ContextClaimsKey    = "auth_claims"
	authorizationHeader = "Authorization"
)

// ErrorCodeImpersonationEnded rejects an impersonation token whose session is over.
const ErrorCodeImpersonationEnded = "auth.impersonation_ended"

// ImpersonationVerifier confirms on every request that an impersonation token's session is
// still active; it returns impdomain.ErrEnded when it is not.
type ImpersonationVerifier interface {
	Verify(ctx context.Context, sessionID string, actorID, targetID uuid.UUID) error
}

// BearerAuth requires a valid Bearer access token. Impersonation tokens also need an active
// session; without a verifier they are refused.
func BearerAuth(auth authports.Service, sessions ImpersonationVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authorizationHeader)
		if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			responses.Failure(c, 401, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		raw := strings.TrimSpace(header[len("Bearer "):])

		claims, err := auth.ParseAccessToken(raw)
		if err != nil {
			responses.Failure(c, 401, responses.ErrorCodeUnauthorized, "Invalid or expired access token")
			return
		}

		if err := verifyImpersonation(c, sessions, claims); err != nil {
			if errors.Is(err, impdomain.ErrEnded) {
				responses.Failure(c, 401, ErrorCodeImpersonationEnded, "The impersonation session has ended")
				return
			}

			responses.Internal(c, err, "Failed to verify the impersonation session")

			return
		}

		setIdentity(c, claims)
		c.Next()
	}
}

// OptionalBearerAuth attaches identity when a valid token is present; never 401s. An
// impersonation token whose session has ended is ignored.
func OptionalBearerAuth(auth authports.Service, sessions ImpersonationVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authorizationHeader)
		if header != "" && strings.HasPrefix(strings.ToLower(header), "bearer ") {
			raw := strings.TrimSpace(header[len("Bearer "):])
			if claims, err := auth.ParseAccessToken(raw); err == nil && verifyImpersonation(c, sessions, claims) == nil {
				setIdentity(c, claims)
			}
		}

		c.Next()
	}
}

func verifyImpersonation(c *gin.Context, sessions ImpersonationVerifier, claims authdomain.AccessClaims) error {
	if !claims.Impersonating() {
		return nil
	}

	if sessions == nil {
		return impdomain.ErrEnded
	}

	return sessions.Verify(c.Request.Context(), claims.SessionID, *claims.Actor, claims.Subject)
}

func setIdentity(c *gin.Context, claims authdomain.AccessClaims) {
	c.Set(ContextUserIDKey, claims.Subject)
	c.Set(ContextClaimsKey, claims)

	if claims.Impersonating() {
		c.Header(HeaderImpersonationSession, claims.SessionID)
	}
}

// CurrentUserID returns the authenticated subject from context.
func CurrentUserID(c *gin.Context) (uuid.UUID, bool) {
	value, ok := c.Get(ContextUserIDKey)
	if !ok {
		return uuid.Nil, false
	}

	id, ok := value.(uuid.UUID)

	return id, ok
}

// CurrentSignInFamily returns the refresh-session family the request's token was issued for;
// uuid.Nil for impersonation tokens and tokens that name none.
func CurrentSignInFamily(c *gin.Context) uuid.UUID {
	value, ok := c.Get(ContextClaimsKey)
	if !ok {
		return uuid.Nil
	}

	claims, ok := value.(authdomain.AccessClaims)
	if !ok || claims.Impersonating() {
		return uuid.Nil
	}

	return claims.FamilyID
}

// Impersonation identifies the staff member behind an impersonation token.
type Impersonation struct {
	ActorID   uuid.UUID
	SessionID uuid.UUID
}

// CurrentImpersonation returns the impersonator and session when the request carries an
// impersonation token.
func CurrentImpersonation(c *gin.Context) (Impersonation, bool) {
	value, ok := c.Get(ContextClaimsKey)
	if !ok {
		return Impersonation{}, false
	}

	claims, ok := value.(authdomain.AccessClaims)
	if !ok || !claims.Impersonating() {
		return Impersonation{}, false
	}

	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return Impersonation{}, false
	}

	return Impersonation{ActorID: *claims.Actor, SessionID: sessionID}, true
}
