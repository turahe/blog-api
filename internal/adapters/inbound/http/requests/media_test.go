package requests

import (
	"testing"
)

func TestPresignMediaValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"originalFilename": "a.png", "contentType": "image/png", "sizeBytes": 1024}

	runBindCases[PresignMedia](t, []bindCase{
		{name: "valid", body: with(base, "tags", []string{"cover"})},
		{name: "filename required", body: with(base, "originalFilename", absent), want: errs("originalFilename", msgRequired("originalFilename"))},
		{name: "filename too long", body: with(base, "originalFilename", long(256)), want: errs("originalFilename", msgMaxChars("originalFilename", 255))},
		{name: "contentType required", body: with(base, "contentType", absent), want: errs("contentType", msgRequired("contentType"))},
		{name: "size required", body: with(base, "sizeBytes", absent), want: errs("sizeBytes", msgRequired("sizeBytes"))},
		{name: "size positive", body: with(base, "sizeBytes", -1), want: errs("sizeBytes", "The sizeBytes field must be greater than 0.")},
		{name: "size must be an integer", body: with(base, "sizeBytes", "big"), want: errs("sizeBytes", "The sizeBytes field must be an integer.")},
	})
}

func TestPatchMediaTagsValidation(t *testing.T) {
	t.Parallel()

	runBindCases[PatchMediaTags](t, []bindCase{
		{name: "valid", body: `{"tags":["a","b"]}`},
		{name: "empty list clears", body: `{"tags":[]}`},
		{name: "tags required", body: `{}`, want: errs("tags", msgRequired("tags"))},
	})
}
