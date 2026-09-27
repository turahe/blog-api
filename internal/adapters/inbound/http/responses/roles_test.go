package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
)

func TestRole(t *testing.T) {
	t.Parallel()

	role := func(name string, permissions []string) rbacdomain.Role {
		return rbacdomain.Role{
			UUID:        uuid.MustParse("0198a1b2-0000-7000-8000-000000005001"),
			Name:        name,
			Description: "Role " + name,
			Permissions: permissions,
			CreatedAt:   time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
			UpdatedAt:   time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
		}
	}

	const times = `"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-02T10:00:00Z"`

	tests := []struct {
		name string
		role rbacdomain.Role
		want string
	}{
		{
			name: "built-in administrator is protected",
			role: role(rbacdomain.ProtectedRole, []string{"*"}),
			want: `{"id": "0198a1b2-0000-7000-8000-000000005001", "name": "admin", "description": "Role admin",
				"permissions": ["*"], "protected": true, ` + times + `}`,
		},
		{
			name: "custom role",
			role: role("editor", []string{"posts.publish", "posts.update"}),
			want: `{"id": "0198a1b2-0000-7000-8000-000000005001", "name": "editor", "description": "Role editor",
				"permissions": ["posts.publish", "posts.update"], "protected": false, ` + times + `}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Role(tt.role))
		})
	}
}

func TestPermission(t *testing.T) {
	t.Parallel()

	permission := rbacdomain.Permission{
		UUID:        uuid.MustParse("0198a1b2-0000-7000-8000-000000005002"),
		Key:         "posts.publish",
		Description: "Publish posts",
	}

	assertJSON(t, `{"id": "0198a1b2-0000-7000-8000-000000005002", "key": "posts.publish", "description": "Publish posts"}`,
		Permission(permission))
}
