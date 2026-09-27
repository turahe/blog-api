package requests

import (
	"testing"
)

func TestPatchProfileValidation(t *testing.T) {
	t.Parallel()

	runBindCases[PatchProfile](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: `{"fullName":"A","socialLinks":{"github":"a"},"locale":"en_US","marketingConsent":true}`},
		{
			name: "nested type errors use dotted paths",
			body: `{"socialLinks":{"github":1}}`,
			want: errs("socialLinks.github", "The socialLinks.github field must be a string."),
		},
	})
}

func TestRequestEmailChangeValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"newEmail": "new@example.com", "passwordProof": "pw"}

	runBindCases[RequestEmailChange](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "email required", body: with(base, "newEmail", absent), want: errs("newEmail", msgRequired("newEmail"))},
		{name: "email too long", body: with(base, "newEmail", long(255)), want: errs("newEmail", msgMaxChars("newEmail", 254))},
		{name: "proof required", body: with(base, "passwordProof", absent), want: errs("passwordProof", msgRequired("passwordProof"))},
		{name: "proof too long", body: with(base, "passwordProof", long(129)), want: errs("passwordProof", msgMaxChars("passwordProof", 128))},
	})
}

func TestConfirmEmailChangeValidation(t *testing.T) {
	t.Parallel()

	runBindCases[ConfirmEmailChange](t, []bindCase{
		{name: "valid", body: `{"token":"t"}`},
		{name: "token required", body: `{}`, want: errs("token", msgRequired("token"))},
		{name: "token too long", body: with(nil, "token", long(513)), want: errs("token", msgMaxChars("token", 512))},
	})
}
