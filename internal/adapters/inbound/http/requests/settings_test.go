package requests

import (
	"testing"
)

func TestUpdateSettingsValidation(t *testing.T) {
	t.Parallel()

	update := map[string]any{"key": "site.title", "value": "Blog", "version": 3}

	tooMany := make([]any, 101)
	for i := range tooMany {
		tooMany[i] = update
	}

	runBindCases[UpdateSettings](t, []bindCase{
		{name: "valid", body: with(nil, "updates", []any{update, map[string]any{"key": "site.logo", "value": nil}})},
		{name: "updates required", body: `{}`, want: errs("updates", msgRequired("updates"))},
		{name: "updates empty", body: `{"updates":[]}`, want: errs("updates", msgMinItems("updates", 1))},
		{name: "too many updates", body: with(nil, "updates", tooMany), want: errs("updates", msgMaxItems("updates", 100))},
		{name: "key required", body: `{"updates":[{"value":1}]}`, want: errs("key", msgRequired("key"))},
		{
			name: "key too long",
			body: with(nil, "updates", []any{map[string]any{"key": long(129)}}),
			want: errs("key", msgMaxChars("key", 128)),
		},
		{
			name: "negative version",
			body: `{"updates":[{"key":"k","version":-1}]}`,
			want: errs("version", msgMin("version", 0)),
		},
	})
}
