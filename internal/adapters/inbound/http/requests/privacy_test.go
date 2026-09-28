package requests

import (
	"testing"
)

func TestUpdatePrivacyValidation(t *testing.T) {
	t.Parallel()

	runBindCases[UpdatePrivacy](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{
			name: "valid",
			body: with(nil, "visibilityProfile", "unlisted", "visibilityEmail", true, "visibilityContact", false,
				"searchAllowIndexing", true, "currentPassword", "pw"),
		},
		{name: "visibility unknown", body: with(nil, "visibilityProfile", "friends"), want: errs("visibilityProfile", msgOneOf("visibilityProfile", "public unlisted private"))},
		{name: "password too long", body: with(nil, "currentPassword", long(129)), want: errs("currentPassword", msgMaxChars("currentPassword", 128))},
	})
}

func TestEraseAccountValidation(t *testing.T) {
	t.Parallel()

	runBindCases[EraseAccount](t, []bindCase{
		{name: "valid", body: `{"currentPassword":"pw"}`},
		{name: "password required", body: `{}`, want: errs("currentPassword", msgRequired("currentPassword"))},
		{name: "password too long", body: with(nil, "currentPassword", long(129)), want: errs("currentPassword", msgMaxChars("currentPassword", 128))},
	})
}
