package requests

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
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
// `binding` tags. On failure it writes a Laravel-style validation_error envelope
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
		validationErrorDetails(err),
	)
}

// validationErrorDetails builds a Laravel-like errors bag:
//
//	{ "email": ["The email field is required."], ... }
func validationErrorDetails(err error) map[string][]string {
	if verrs, ok := errors.AsType[validator.ValidationErrors](err); ok {
		bag := make(map[string][]string, len(verrs))
		for _, fe := range verrs {
			field := fe.Field()
			if field == "" {
				field = "_form"
			}

			bag[field] = append(bag[field], validationMessage(fe))
		}

		return bag
	}

	return map[string][]string{"_form": {"The request body is invalid."}}
}

func validationMessage(fe validator.FieldError) string {
	field := fe.Field()
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("The %s field is required.", field)
	case "email":
		return fmt.Sprintf("The %s must be a valid email address.", field)
	case "min":
		return fmt.Sprintf("The %s must be at least %s characters.", field, fe.Param())
	case "max":
		return fmt.Sprintf("The %s may not be greater than %s characters.", field, fe.Param())
	case "eqfield":
		other := camelToSnake(fe.Param())
		return fmt.Sprintf("The %s field must match %s.", field, other)
	default:
		return fmt.Sprintf("The %s field is invalid.", field)
	}
}

func camelToSnake(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder

	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}

			b.WriteRune(unicode.ToLower(r))

			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}
