package handlers

import (
	"errors"
	nethttp "net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEmailChangeHandlerFailures(t *testing.T) {
	t.Parallel()

	user := testUserID
	requestBody := `{"newEmail":"new@example.com","passwordProof":"Secret123456"}`

	tests := []struct {
		name    string
		handler func(*fakeEmailChanger) gin.HandlerFunc
		user    *uuid.UUID
		body    string
		err     error
		status  int
		code    string
	}{
		{name: "request needs sign-in", handler: requestHandler, body: requestBody, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "request service failure", handler: requestHandler, user: &user, body: requestBody, err: errors.New("smtp down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
		{name: "confirm needs sign-in", handler: confirmHandler, body: `{"token":"t"}`, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "confirm malformed body", handler: confirmHandler, user: &user, body: `{`, status: nethttp.StatusBadRequest, code: "validation_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, tc.handler(&fakeEmailChanger{err: tc.err}), profileRequest{
				method: nethttp.MethodPost, target: "/", body: tc.body, contentType: jsonContent, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func requestHandler(f *fakeEmailChanger) gin.HandlerFunc { return meRequestEmailChangeHandler(f) }

func confirmHandler(f *fakeEmailChanger) gin.HandlerFunc { return meConfirmEmailChangeHandler(f) }
