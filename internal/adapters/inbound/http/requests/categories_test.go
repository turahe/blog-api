package requests

import (
	"testing"
)

func TestCreateCategoryValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"name": "Go"}

	runBindCases[CreateCategory](t, []bindCase{
		{name: "valid minimal", body: with(base)},
		{
			name: "valid full",
			body: with(base, "slug", "go", "description", "d", "parentId", idA.String(), "imageId", idB.String(), "beforeId", idC.String()),
		},
		{name: "name required", body: with(base, "name", absent), want: errs("name", msgRequired("name"))},
		{name: "name too long", body: with(base, "name", long(256)), want: errs("name", msgMaxChars("name", 255))},
		{name: "slug too long", body: with(base, "slug", long(256)), want: errs("slug", msgMaxChars("slug", 255))},
		{name: "description too long", body: with(base, "description", long(2001)), want: errs("description", msgMaxChars("description", 2000))},
		{name: "parentId must be a uuid", body: with(base, "parentId", "x"), want: errs("parentId", msgUUID("parentId"))},
		{name: "imageId must be a uuid", body: with(base, "imageId", "x"), want: errs("imageId", msgUUID("imageId"))},
		{name: "beforeId must be a uuid", body: with(base, "beforeId", "x"), want: errs("beforeId", msgUUID("beforeId"))},
	})
}
