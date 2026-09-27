package requests

import (
	"testing"
)

func TestCreateTagValidation(t *testing.T) {
	t.Parallel()

	runBindCases[CreateTag](t, []bindCase{
		{name: "valid", body: `{"name":"Go","slug":"go"}`},
		{name: "slug optional", body: `{"name":"Go"}`},
		{name: "name required", body: `{}`, want: errs("name", msgRequired("name"))},
		{name: "name too long", body: with(nil, "name", long(101)), want: errs("name", msgMaxChars("name", 100))},
		{name: "slug too long", body: with(nil, "name", "Go", "slug", long(101)), want: errs("slug", msgMaxChars("slug", 100))},
	})
}

func TestUpdateTagValidation(t *testing.T) {
	t.Parallel()

	runBindCases[UpdateTag](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: `{"name":"Go","slug":"go"}`},
		{name: "name too long", body: with(nil, "name", long(101)), want: errs("name", msgMaxChars("name", 100))},
		{name: "slug too long", body: with(nil, "slug", long(101)), want: errs("slug", msgMaxChars("slug", 100))},
	})
}

func TestMergeTagValidation(t *testing.T) {
	t.Parallel()

	runBindCases[MergeTag](t, []bindCase{
		{name: "valid", body: with(nil, "intoId", idA.String())},
		{name: "intoId required", body: `{}`, want: errs("intoId", msgRequired("intoId"))},
		{name: "intoId must be a uuid", body: `{"intoId":"x"}`, want: errs("intoId", msgUUID("intoId"))},
	})
}
