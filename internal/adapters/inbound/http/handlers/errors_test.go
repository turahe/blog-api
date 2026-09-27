package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

var (
	errWidgetInvalid  = errors.New("widget name is required")
	errWidgetNotFound = errors.New("widget not found")
	errWidgetConflict = errors.New("widget conflict")
	errWidgetInUse    = errors.New("widget in use")
)

func TestResourceErrorsWrite(t *testing.T) {
	t.Parallel()

	widgets := resourceErrors{
		service: responses.ServiceTags, name: "Widget", inUseCode: "widget_in_use",
		validation: errWidgetInvalid, notFound: errWidgetNotFound, conflict: errWidgetConflict, inUse: errWidgetInUse,
	}

	tests := []struct {
		name     string
		err      error
		written  bool
		status   int
		code     string
		message  string
		recorded bool
	}{
		{name: "nil writes nothing", err: nil},
		{name: "validation", err: fmt.Errorf("create: %w", errWidgetInvalid), written: true, status: nethttp.StatusBadRequest, code: "validation_error", message: "create: widget name is required"},
		{name: "not found", err: errWidgetNotFound, written: true, status: nethttp.StatusNotFound, code: "not_found", message: "Widget not found"},
		{name: "conflict", err: errWidgetConflict, written: true, status: nethttp.StatusConflict, code: "conflict", message: "Widget conflict"},
		{name: "in use", err: errWidgetInUse, written: true, status: nethttp.StatusConflict, code: "widget_in_use", message: "Widget in use"},
		{name: "unknown", err: errors.New("db down"), written: true, status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to process widget", recorded: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/", nil)

			require.Equal(t, tc.written, widgets.write(c, tc.err))
			require.Equal(t, tc.recorded, len(c.Errors) > 0)

			if !tc.written {
				require.Zero(t, w.Body.Len())
				return
			}

			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))
		})
	}
}
