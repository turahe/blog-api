package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

var (
	impSessionID = uuid.MustParse("0198a1b2-0000-7000-8000-000000000101")
	impActorID   = uuid.MustParse("0198a1b2-0000-7000-8000-000000000102")
	impTargetID  = uuid.MustParse("0198a1b2-0000-7000-8000-000000000103")
	impStartedAt = time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
)

func activeImpersonation() impdomain.Session {
	return impdomain.Session{
		UUID:       impSessionID,
		ActorUUID:  impActorID,
		TargetUUID: impTargetID,
		State:      impdomain.StateActive,
		Reason:     "ticket 42",
		IP:         "203.0.113.7",
		StartedAt:  impStartedAt,
		ExpiresAt:  impStartedAt.Add(15 * time.Minute),
	}
}

const impSessionIDsJSON = `"impersonationSessionId": "0198a1b2-0000-7000-8000-000000000101",
	"impersonatorUserId": "0198a1b2-0000-7000-8000-000000000102", "targetUserId": "0198a1b2-0000-7000-8000-000000000103",
	"reason": "ticket 42", "startedAt": "2026-08-01T10:00:00Z", "expiresAt": "2026-08-01T10:15:00Z"`

func TestImpersonationSession(t *testing.T) {
	t.Parallel()

	ended := activeImpersonation()
	ended.State = impdomain.StateExited
	ended.EndedAt = new(time.Date(2026, 8, 1, 10, 5, 0, 0, time.UTC))
	ended.EndReason = new(impdomain.EndManual)

	tests := []struct {
		name    string
		session impdomain.Session
		active  bool
		want    string
	}{
		{
			name:    "active",
			session: activeImpersonation(),
			active:  true,
			want:    `{` + impSessionIDsJSON + `, "state": "active", "active": true, "endedAt": null, "endReason": null}`,
		},
		{
			name:    "ended manually",
			session: ended,
			active:  false,
			want: `{` + impSessionIDsJSON + `, "state": "exited", "active": false,
				"endedAt": "2026-08-01T10:05:00Z", "endReason": "manual_exit"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, ImpersonationSession(tt.session, tt.active))
		})
	}
}

func TestImpersonationStarted(t *testing.T) {
	t.Parallel()

	target := userdomain.User{
		UUID:         impTargetID,
		Email:        "reader@example.com",
		Username:     "reader",
		FullName:     "Rea Der",
		PasswordHash: "secret-hash",
	}

	assertJSON(t, `{
		"session": {`+impSessionIDsJSON+`, "state": "active", "active": true, "endedAt": null, "endReason": null},
		"targetUser": {"id": "0198a1b2-0000-7000-8000-000000000103", "username": "reader",
			"email": "reader@example.com", "fullName": "Rea Der"},
		"accessToken": "imp-token", "tokenType": "Bearer", "expiresIn": 900
	}`, ImpersonationStarted(activeImpersonation(), target, "imp-token"))
}
