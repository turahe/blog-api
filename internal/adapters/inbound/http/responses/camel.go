package responses

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

// CamelKey turns a stored snake_case field name into the camelCase name the API uses.
func CamelKey(name string) string {
	head, rest, found := strings.Cut(name, "_")
	if !found {
		return name
	}

	var b strings.Builder

	b.WriteString(head)

	for part := range strings.SplitSeq(rest, "_") {
		if part == "" {
			continue
		}

		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}

	return b.String()
}

// camelKeys copies m with every top-level key in camelCase; values are kept as they are.
func camelKeys[V any](m map[string]V) map[string]V {
	out := make(map[string]V, len(m))
	for key, value := range m {
		out[CamelKey(key)] = value
	}

	return out
}

// camelTree renames the keys of every object in v, at any depth. Use it only where all keys
// are field names, never where keys are data (setting keys, tag names, enum values).
func camelTree(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}

	var generic any
	if json.Unmarshal(raw, &generic) != nil {
		return v
	}

	return renameKeys(generic)
}

func renameKeys(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[CamelKey(key)] = renameKeys(value)
		}

		return out
	case []any:
		for i, value := range typed {
			typed[i] = renameKeys(value)
		}

		return typed
	default:
		return v
	}
}

func camelNames(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = CamelKey(name)
	}

	return out
}

// FieldViolations renders domain violations with the field named as in requests.
func FieldViolations(violations []postdomain.FieldViolation) []gin.H {
	out := make([]gin.H, len(violations))
	for i, v := range violations {
		out[i] = gin.H{"field": CamelKey(v.Field), "code": v.Code, "message": v.Message}
	}

	return out
}
