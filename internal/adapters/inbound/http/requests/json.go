package requests

import (
	"encoding/json"
	"strings"
)

// IsJSONNull reports whether raw is the JSON literal null.
func IsJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
