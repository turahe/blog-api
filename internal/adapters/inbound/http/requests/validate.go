package requests

import (
	"errors"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	pkgresponse "github.com/turahe/pkg/response"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

func init() {
	engine, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}

	engine.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}

		if name != "" {
			return name
		}

		return field.Name
	})
}

// BindJSON decodes JSON into dst and runs go-playground/validator rules from
// `binding` tags. On failure it writes a validation_error envelope
// (details = map[field][]messages) and returns false.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		FailValidation(c, err)
		return false
	}

	return true
}

// FailValidation writes a validation_error envelope from a bind/validate error.
func FailValidation(c *gin.Context, err error) {
	responses.FailureWithDetails(
		c,
		400,
		responses.ErrorCodeValidation,
		"The given data was invalid.",
		FormatValidationError(err),
	)
}

// FormatValidationError converts err into a map of field names to error message slices.
// Field messages come from github.com/turahe/pkg/response. Wrapped validator errors are
// unwrapped first; anything else becomes {"general": [err.Error()]}.
func FormatValidationError(err error) map[string][]string {
	if validationErrors, ok := errors.AsType[validator.ValidationErrors](err); ok {
		return pkgresponse.FormatValidationError(validationErrors)
	}

	return pkgresponse.FormatValidationError(err)
}
