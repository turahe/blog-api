package service

import (
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/event"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// userPayload is UserEventPayload in docs/architecture/asyncapi.yaml, without the email:
// events carry identifiers, not contact details.
type userPayload struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	Status   string    `json:"status"`
	Roles    []string  `json:"roles"`
}

func userCreatedEvent(user userdomain.User, roles []string) event.Event {
	if roles == nil {
		roles = []string{}
	}

	return event.New(event.UserCreated, event.AggregateUser, user.UUID, nil, user.CreatedAt, userPayload{
		UserID: user.UUID, Username: user.Username, Status: string(user.Status), Roles: roles,
	})
}

// passwordResetPayload is AuthPasswordResetRequestedPayload. It is only recorded when a
// token was issued, so it never reveals whether an unknown address was tried.
type passwordResetPayload struct {
	UserID    uuid.UUID `json:"user_id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

func passwordResetRequestedEvent(user userdomain.User, token authdomain.PasswordResetToken) event.Event {
	return event.New(event.UserPasswordResetRequested, event.AggregateUser, user.UUID, nil, token.CreatedAt,
		passwordResetPayload{UserID: user.UUID, Status: "issued", ExpiresAt: token.ExpiresAt})
}
