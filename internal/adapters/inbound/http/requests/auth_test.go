package requests

import (
	"testing"
)

func TestLoginValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"email": "a@example.com", "password": "x"}

	runBindCases[Login](t, []bindCase{
		{name: "valid", body: with(base, "remember", true)},
		{name: "email required", body: with(base, "email", absent), want: errs("email", msgRequired("email"))},
		{name: "email format", body: with(base, "email", "nope"), want: errs("email", msgEmail("email"))},
		{name: "password required", body: with(base, "password", absent), want: errs("password", msgRequired("password"))},
		{name: "remember must be a bool", body: with(base, "remember", "yes"), want: errs("remember", "The remember field must be true or false.")},
	})
}

func TestTwoFactorChallengeValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"challengeToken": "tok", "code": "123456"}

	runBindCases[TwoFactorChallenge](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "token required", body: with(base, "challengeToken", absent), want: errs("challengeToken", msgRequired("challengeToken"))},
		{name: "token too long", body: with(base, "challengeToken", long(129)), want: errs("challengeToken", msgMaxChars("challengeToken", 128))},
		{name: "code required", body: with(base, "code", absent), want: errs("code", msgRequired("code"))},
		{name: "code too long", body: with(base, "code", long(33)), want: errs("code", msgMaxChars("code", 32))},
	})
}

func TestTwoFactorCodeValidation(t *testing.T) {
	t.Parallel()

	runBindCases[TwoFactorCode](t, []bindCase{
		{name: "valid", body: `{"code":"123456"}`},
		{name: "code required", body: `{}`, want: errs("code", msgRequired("code"))},
		{name: "code too long", body: with(nil, "code", long(33)), want: errs("code", msgMaxChars("code", 32))},
	})
}

func TestDisableTwoFactorValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"password": "pw", "code": "123456"}

	runBindCases[DisableTwoFactor](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "password required", body: with(base, "password", absent), want: errs("password", msgRequired("password"))},
		{name: "password too long", body: with(base, "password", long(129)), want: errs("password", msgMaxChars("password", 128))},
		{name: "code required", body: with(base, "code", absent), want: errs("code", msgRequired("code"))},
		{name: "code too long", body: with(base, "code", long(33)), want: errs("code", msgMaxChars("code", 32))},
	})
}

func TestOAuthCallbackValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"code": "c", "state": "s"}

	runBindCases[OAuthCallback](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "code required", body: with(base, "code", absent), want: errs("code", msgRequired("code"))},
		{name: "code too long", body: with(base, "code", long(2049)), want: errs("code", msgMaxChars("code", 2048))},
		{name: "state required", body: with(base, "state", absent), want: errs("state", msgRequired("state"))},
		{name: "state too long", body: with(base, "state", long(129)), want: errs("state", msgMaxChars("state", 128))},
	})
}

func TestRefreshValidation(t *testing.T) {
	t.Parallel()

	runBindCases[Refresh](t, []bindCase{
		{name: "valid", body: `{"refreshToken":"r"}`},
		{name: "token required", body: `{}`, want: errs("refreshToken", msgRequired("refreshToken"))},
	})
}

func TestLogoutValidation(t *testing.T) {
	t.Parallel()

	runBindCases[Logout](t, []bindCase{
		{name: "token optional", body: `{}`},
		{name: "token given", body: `{"refreshToken":"r"}`},
	})
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"email": "a@example.com", "username": "alice", "fullName": "Alice", "password": long(12)}

	runBindCases[Register](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "email required", body: with(base, "email", absent), want: errs("email", msgRequired("email"))},
		{name: "email format", body: with(base, "email", "nope"), want: errs("email", msgEmail("email"))},
		{name: "username required", body: with(base, "username", absent), want: errs("username", msgRequired("username"))},
		{name: "username too short", body: with(base, "username", "ab"), want: errs("username", msgMinChars("username", 3))},
		{name: "username too long", body: with(base, "username", long(33)), want: errs("username", msgMaxChars("username", 32))},
		{name: "fullName required", body: with(base, "fullName", absent), want: errs("fullName", msgRequired("fullName"))},
		{name: "fullName too long", body: with(base, "fullName", long(121)), want: errs("fullName", msgMaxChars("fullName", 120))},
		{name: "password required", body: with(base, "password", absent), want: errs("password", msgRequired("password"))},
		{name: "password too short", body: with(base, "password", long(11)), want: errs("password", msgMinChars("password", 12))},
		{name: "password too long", body: with(base, "password", long(129)), want: errs("password", msgMaxChars("password", 128))},
	})
}

func TestVerifyEmailValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"token": "t", "password": "pw"}

	runBindCases[VerifyEmail](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "token required", body: with(base, "token", absent), want: errs("token", msgRequired("token"))},
		{name: "token too long", body: with(base, "token", long(513)), want: errs("token", msgMaxChars("token", 512))},
		{name: "password required", body: with(base, "password", absent), want: errs("password", msgRequired("password"))},
		{name: "password too long", body: with(base, "password", long(129)), want: errs("password", msgMaxChars("password", 128))},
	})
}

func TestForgotPasswordValidation(t *testing.T) {
	t.Parallel()

	runBindCases[ForgotPassword](t, []bindCase{
		{name: "valid", body: `{"emailOrUsername":"alice"}`},
		{name: "required", body: `{}`, want: errs("emailOrUsername", msgRequired("emailOrUsername"))},
	})
}

func TestResetPasswordValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"token": "t", "newPassword": long(12), "confirmPassword": long(12)}

	runBindCases[ResetPassword](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "token required", body: with(base, "token", absent), want: errs("token", msgRequired("token"))},
		{
			name: "new password too short",
			body: with(base, "newPassword", long(11), "confirmPassword", long(11)),
			want: errs("newPassword", msgMinChars("newPassword", 12)),
		},
		{name: "confirmation required", body: with(base, "confirmPassword", absent), want: errs("confirmPassword", msgRequired("confirmPassword"))},
		{
			name: "confirmation mismatch",
			body: with(base, "confirmPassword", long(13)),
			want: errs("confirmPassword", "The confirmPassword field must match newPassword."),
		},
	})
}

func TestChangePasswordValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"currentPassword": "old", "newPassword": long(12), "confirmPassword": long(12)}

	runBindCases[ChangePassword](t, []bindCase{
		{name: "valid", body: with(base, "revokeAllSessions", true)},
		{name: "current required", body: with(base, "currentPassword", absent), want: errs("currentPassword", msgRequired("currentPassword"))},
		{name: "new required", body: with(base, "newPassword", absent), want: map[string][]string{
			"newPassword":     {msgRequired("newPassword")},
			"confirmPassword": {"The confirmPassword field must match newPassword."},
		}},
		{
			name: "new password too long",
			body: with(base, "newPassword", long(129), "confirmPassword", long(129)),
			want: errs("newPassword", msgMaxChars("newPassword", 128)),
		},
		{
			name: "confirmation mismatch",
			body: with(base, "confirmPassword", "other"),
			want: errs("confirmPassword", "The confirmPassword field must match newPassword."),
		},
	})
}
