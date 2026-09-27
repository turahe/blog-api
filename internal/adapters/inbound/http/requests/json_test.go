package requests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsJSONNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{name: "null", raw: json.RawMessage(`null`), want: true},
		{name: "padded null", raw: json.RawMessage(" null\n"), want: true},
		{name: "absent", raw: nil, want: false},
		{name: "string null", raw: json.RawMessage(`"null"`), want: false},
		{name: "value", raw: json.RawMessage(`"11111111-1111-4111-8111-111111111111"`), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, IsJSONNull(tt.raw))
		})
	}
}
