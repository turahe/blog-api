package handlers

import (
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// resourceErrors maps the validation/not-found/conflict/in-use sentinels of a
// CRUD resource onto error envelopes for its service; anything else is a 500.
type resourceErrors struct {
	service   int
	name      string // capitalized resource name used in messages, e.g. "Category"
	inUseCode string

	validation error
	notFound   error
	conflict   error
	inUse      error
}

// write sends the envelope for err and reports whether it did (false when err is nil).
func (r resourceErrors) write(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	status, caseCode := nethttp.StatusInternalServerError, responses.CaseInternalError
	code, message := responses.ErrorCodeInternal, "Failed to process "+strings.ToLower(r.name)

	switch {
	case errors.Is(err, r.validation):
		status, caseCode = nethttp.StatusBadRequest, responses.CaseValidation
		code, message = responses.ErrorCodeValidation, err.Error()
	case errors.Is(err, r.notFound):
		status, caseCode = nethttp.StatusNotFound, responses.CaseNotFound
		code, message = responses.ErrorCodeNotFound, r.name+" not found"
	case errors.Is(err, r.conflict):
		status, caseCode = nethttp.StatusConflict, responses.CaseConflict
		code, message = responses.ErrorCodeConflict, r.name+" conflict"
	case errors.Is(err, r.inUse):
		status, caseCode = nethttp.StatusConflict, responses.CaseConflict
		code, message = r.inUseCode, r.name+" in use"
	}

	responses.FailureFor(c, status, responses.FailureOpts{
		Service: r.service,
		Case:    caseCode,
		Code:    code,
		Message: message,
	})

	return true
}
