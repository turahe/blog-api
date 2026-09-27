package requests

import (
	"testing"
)

func TestCreateRoleValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"name": "editor"}

	runBindCases[CreateRole](t, []bindCase{
		{name: "valid", body: with(base, "description", "Edits", "permissions", []string{"posts.write"})},
		{name: "name required", body: with(base, "name", absent), want: errs("name", msgRequired("name"))},
		{name: "name too short", body: with(base, "name", "e"), want: errs("name", msgMinChars("name", 2))},
		{name: "name too long", body: with(base, "name", long(61)), want: errs("name", msgMaxChars("name", 60))},
		{name: "description too long", body: with(base, "description", long(256)), want: errs("description", msgMaxChars("description", 255))},
		{name: "too many permissions", body: with(base, "permissions", make([]string, 201)), want: errs("permissions", msgMaxItems("permissions", 200))},
		{name: "empty permission", body: with(base, "permissions", []string{""}), want: errs("permissions.0", msgRequired("permissions.0"))},
		{name: "permission too long", body: with(base, "permissions", []string{long(129)}), want: errs("permissions.0", msgMaxChars("permissions.0", 128))},
	})
}

func TestUpdateRoleValidation(t *testing.T) {
	t.Parallel()

	runBindCases[UpdateRole](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "description too long", body: with(nil, "description", long(256)), want: errs("description", msgMaxChars("description", 255))},
	})
}

func TestSetRolePermissionsValidation(t *testing.T) {
	t.Parallel()

	runBindCases[SetRolePermissions](t, []bindCase{
		{name: "valid", body: `{"permissions":["posts.write"]}`},
		{name: "empty list revokes all", body: `{"permissions":[]}`},
		{name: "permissions required", body: `{}`, want: errs("permissions", msgRequired("permissions"))},
		{name: "too many permissions", body: with(nil, "permissions", make([]string, 201)), want: errs("permissions", msgMaxItems("permissions", 200))},
		{name: "permission too long", body: with(nil, "permissions", []string{long(129)}), want: errs("permissions.0", msgMaxChars("permissions.0", 128))},
	})
}

func TestAssignUserRolesValidation(t *testing.T) {
	t.Parallel()

	runBindCases[AssignUserRoles](t, []bindCase{
		{name: "valid", body: `{"roles":["editor"]}`},
		{name: "roles required", body: `{}`, want: errs("roles", msgRequired("roles"))},
		{name: "roles empty", body: `{"roles":[]}`, want: errs("roles", msgMinItems("roles", 1))},
		{name: "too many roles", body: with(nil, "roles", make([]string, 21)), want: errs("roles", msgMaxItems("roles", 20))},
		{name: "empty role", body: `{"roles":[""]}`, want: errs("roles.0", msgRequired("roles.0"))},
		{name: "role too long", body: with(nil, "roles", []string{long(65)}), want: errs("roles.0", msgMaxChars("roles.0", 64))},
	})
}
