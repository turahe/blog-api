package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

func TestUser(t *testing.T) {
	t.Parallel()

	pending := userdomain.User{
		ID:           1,
		UUID:         uuid.MustParse("0198a1b2-0000-7000-8000-000000009001"),
		Email:        "gopher@example.com",
		Username:     "gopher",
		FullName:     "Go Pher",
		PasswordHash: "secret-hash",
		Status:       userdomain.StatusPending,
		CreatedAt:    time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		UpdatedAt:    time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	}

	verified := pending
	verified.Status = userdomain.StatusActive
	verified.EmailVerifiedAt = new(time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC))
	verified.LoginCount = 3

	const common = `"id": "0198a1b2-0000-7000-8000-000000009001", "email": "gopher@example.com", "username": "gopher",
		"fullName": "Go Pher", "createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T10:00:00Z"`

	tests := []struct {
		name string
		user userdomain.User
		want string
	}{
		{
			name: "unverified",
			user: pending,
			want: `{` + common + `, "status": "pending", "emailVerifiedAt": null, "loginCount": 0}`,
		},
		{
			name: "verified",
			user: verified,
			want: `{` + common + `, "status": "active", "emailVerifiedAt": "2026-08-01T10:30:00Z", "loginCount": 3}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, User(tt.user))
		})
	}
}
