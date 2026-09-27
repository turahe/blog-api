package requests

import (
	"testing"
)

func TestRestoreRevisionValidation(t *testing.T) {
	t.Parallel()

	runBindCases[RestoreRevision](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: `{"restoreNote":"back to v2"}`},
		{name: "note too long", body: with(nil, "restoreNote", long(1001)), want: errs("restoreNote", msgMaxChars("restoreNote", 1000))},
	})
}
