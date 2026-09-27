package requests

import (
	"testing"
)

func TestStartImpersonationValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"targetUserId": idA.String(), "reason": "Ticket #4521", "currentPassword": "pw"}

	runBindCases[StartImpersonation](t, []bindCase{
		{name: "valid", body: with(base, "twoFactorCode", "123456")},
		{name: "target required", body: with(base, "targetUserId", absent), want: errs("targetUserId", msgRequired("targetUserId"))},
		{name: "target must be a uuid", body: with(base, "targetUserId", "x"), want: errs("targetUserId", msgUUID("targetUserId"))},
		{name: "reason required", body: with(base, "reason", absent), want: errs("reason", msgRequired("reason"))},
		{name: "reason too short", body: with(base, "reason", long(9)), want: errs("reason", msgMinChars("reason", 10))},
		{name: "reason too long", body: with(base, "reason", long(256)), want: errs("reason", msgMaxChars("reason", 255))},
		{name: "password required", body: with(base, "currentPassword", absent), want: errs("currentPassword", msgRequired("currentPassword"))},
		{name: "password too long", body: with(base, "currentPassword", long(129)), want: errs("currentPassword", msgMaxChars("currentPassword", 128))},
		{name: "code too long", body: with(base, "twoFactorCode", long(33)), want: errs("twoFactorCode", msgMaxChars("twoFactorCode", 32))},
	})
}
