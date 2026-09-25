package event

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const asyncAPIPath = "../../../docs/architecture/asyncapi.yaml"

// TestEventTypesHaveAsyncAPIChannels keeps asyncapi.yaml authoritative: every event type
// constant in types.go must be a documented channel.
func TestEventTypesHaveAsyncAPIChannels(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(asyncAPIPath)
	require.NoError(t, err)

	var spec struct {
		Channels map[string]any `yaml:"channels"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))
	require.NotEmpty(t, spec.Channels)

	types := eventTypeConstants(t)
	require.NotEmpty(t, types)

	for name, channel := range types {
		require.Contains(t, spec.Channels, channel, "event.%s (%q) has no channel in asyncapi.yaml", name, channel)
	}
}

// eventTypeConstants returns the string constants in types.go, except aggregate types.
func eventTypeConstants(t *testing.T) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, 0)
	require.NoError(t, err)

	constants := map[string]string{}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, ident := range value.Names {
				if strings.HasPrefix(ident.Name, "Aggregate") || i >= len(value.Values) {
					continue
				}

				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				channel, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)

				constants[ident.Name] = channel
			}
		}
	}

	return constants
}
