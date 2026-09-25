package service_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

func TestAccountWritesRecordEvents(t *testing.T) {
	t.Parallel()

	existing := userdomain.User{
		UUID: uuid.New(), Email: "a@example.com", Username: "a", FullName: "A",
		PasswordHash: "hash:OldPassword1!", Status: userdomain.StatusActive,
	}
	users := &memUsers{
		byEmail: map[string]userdomain.User{existing.Email: existing},
		byID:    map[uuid.UUID]userdomain.User{existing.UUID: existing},
	}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	events := &eventtest.Recorder{}
	svc := newService(users, sessions, resets, &capturingSink{}).WithRoles(newMemRoles("editor")).WithEvents(events.Unit())

	in := validNewUser()
	in.Roles = []string{"editor"}
	created, err := svc.AdminCreateUser(t.Context(), in)
	require.NoError(t, err)

	require.NoError(t, svc.ForgotPassword(t.Context(), "nobody@example.com"))
	require.NoError(t, svc.ForgotPassword(t.Context(), existing.Email))

	require.Equal(t, []string{event.UserCreated, event.UserPasswordResetRequested}, events.Types(),
		"unknown addresses record nothing")

	recorded := events.Events()
	require.Equal(t, created.UUID, recorded[0].AggregateID)
	require.Equal(t, existing.UUID, recorded[1].AggregateID)

	payload, err := json.Marshal(recorded[0].Payload)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "ada@example.com", "no email in events")
}
