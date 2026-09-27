package requests

import (
	"testing"
)

func TestAdminCreateUserValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"email": "a@example.com", "username": "alice", "fullName": "Alice", "password": long(12)}

	runBindCases[AdminCreateUser](t, []bindCase{
		{name: "valid", body: with(base, "roles", []string{"editor"})},
		{name: "email required", body: with(base, "email", absent), want: errs("email", msgRequired("email"))},
		{name: "email format", body: with(base, "email", "nope"), want: errs("email", msgEmail("email"))},
		{name: "username too short", body: with(base, "username", "ab"), want: errs("username", msgMinChars("username", 3))},
		{name: "username too long", body: with(base, "username", long(33)), want: errs("username", msgMaxChars("username", 32))},
		{name: "fullName required", body: with(base, "fullName", absent), want: errs("fullName", msgRequired("fullName"))},
		{name: "fullName too long", body: with(base, "fullName", long(121)), want: errs("fullName", msgMaxChars("fullName", 120))},
		{name: "password too short", body: with(base, "password", long(11)), want: errs("password", msgMinChars("password", 12))},
		{name: "password too long", body: with(base, "password", long(129)), want: errs("password", msgMaxChars("password", 128))},
		{name: "too many roles", body: with(base, "roles", make([]string, 21)), want: errs("roles", msgMaxItems("roles", 20))},
		{name: "empty role", body: with(base, "roles", []string{""}), want: errs("roles.0", msgRequired("roles.0"))},
		{name: "role too long", body: with(base, "roles", []string{"a", long(65)}), want: errs("roles.1", msgMaxChars("roles.1", 64))},
	})
}

func TestAdminResetPasswordValidation(t *testing.T) {
	t.Parallel()

	runBindCases[AdminResetPassword](t, []bindCase{
		{name: "empty body keeps default", body: `{}`},
		{name: "explicit revoke", body: `{"revokeSessions": false}`},
		{name: "revoke must be a bool", body: `{"revokeSessions": "no"}`, want: errs("revokeSessions", "The revokeSessions field must be true or false.")},
	})
}
