package requests

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidationMessageHelpers(t *testing.T) {
	t.Parallel()
	require.Equal(t, "new_password", camelToSnake("NewPassword"))
	require.Equal(t, map[string][]string{"_form": {"The request body is invalid."}}, ValidationErrorDetails(io.EOF))
}
