package persistence

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelTableNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model interface{ TableName() string }
		want  string
	}{
		{UserModel{}, "users"},
		{RefreshSessionModel{}, "refresh_sessions"},
		{PostModel{}, "posts"},
		{MediaAssetModel{}, "media_assets"},
		{RoleModel{}, "roles"},
		{PermissionModel{}, "permissions"},
		{UserRoleModel{}, "user_roles"},
		{RolePermissionModel{}, "role_permissions"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.model.TableName())
		})
	}
}
