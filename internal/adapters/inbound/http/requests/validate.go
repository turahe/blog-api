package requests

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

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

// FormatValidationError converts err into a map of field names to error message slices. If err is
// validator.ValidationErrors, fields are mapped; otherwise returns {"general": [err.Error()]}.
func FormatValidationError(err error) map[string][]string {
	errs := make(map[string][]string)

	if validationErrors, ok := errors.AsType[validator.ValidationErrors](err); ok {
		for _, fieldError := range validationErrors {
			fieldName := getFieldName(fieldError)
			errs[fieldName] = append(errs[fieldName], getErrorMessage(fieldError, fieldName))
		}
	} else {
		errs["general"] = []string{err.Error()}
	}

	return errs
}

// getFieldName returns the JSON name registered on the validator, or the camelCased struct
// field when the field has no json tag.
func getFieldName(fieldError validator.FieldError) string {
	structField := fieldError.StructField()
	namespace := fieldError.Namespace()
	fieldName := fieldError.Field()

	hasStructContext := structField != "" && namespace != "" && len(strings.Split(namespace, ".")) >= 2
	if hasStructContext {
		if fieldName != "" && fieldName != structField {
			return fieldName
		}

		return toCamelCase(structField)
	}

	if fieldName != "" {
		return fieldName
	}

	if structField != "" {
		return toCamelCase(structField)
	}

	return strings.ToLower(fieldName)
}

// toCamelCase converts "FirstName" to "firstName".
func toCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}

	if s[0] >= 'a' && s[0] <= 'z' {
		return s
	}

	return strings.ToLower(s[:1]) + s[1:]
}

// errorMessages holds a message per validator tag. %[1]s is the field name and %[2]s the rule
// parameter; the explicit indexes let a message leave the parameter out.
var errorMessages = map[string]string{
	"required":         "The %[1]s field is required.",
	"email":            "The %[1]s must be a valid email address.",
	"min":              "The %[1]s must be at least %[2]s characters.",
	"max":              "The %[1]s may not be greater than %[2]s characters.",
	"len":              "The %[1]s must be exactly %[2]s characters.",
	"numeric":          "The %[1]s must be a number.",
	"alpha":            "The %[1]s may only contain letters.",
	"alphanum":         "The %[1]s may only contain letters and numbers.",
	"url":              "The %[1]s must be a valid URL.",
	"uuid":             "The %[1]s must be a valid UUID.",
	"oneof":            "The %[1]s must be one of: %[2]s.",
	"gte":              "The %[1]s must be greater than or equal to %[2]s.",
	"lte":              "The %[1]s must be less than or equal to %[2]s.",
	"gt":               "The %[1]s must be greater than %[2]s.",
	"lt":               "The %[1]s must be less than %[2]s.",
	"eq":               "The %[1]s must be equal to %[2]s.",
	"ne":               "The %[1]s must not be equal to %[2]s.",
	"unique":           "The %[1]s has already been taken.",
	"exists":           "The selected %[1]s is invalid.",
	"date":             "The %[1]s must be a valid date.",
	"datetime":         "The %[1]s must be a valid date and time.",
	"timezone":         "The %[1]s must be a valid timezone.",
	"json":             "The %[1]s must be a valid JSON string.",
	"ip":               "The %[1]s must be a valid IP address.",
	"ipv4":             "The %[1]s must be a valid IPv4 address.",
	"ipv6":             "The %[1]s must be a valid IPv6 address.",
	"base64":           "The %[1]s must be a valid base64 string.",
	"required_if":      "The %[1]s field is required when %[2]s is present.",
	"required_unless":  "The %[1]s field is required unless %[2]s is present.",
	"required_with":    "The %[1]s field is required when %[2]s is present.",
	"required_without": "The %[1]s field is required when %[2]s is not present.",
}

// getErrorMessage generates a human-readable error message from a validation error.
func getErrorMessage(fieldError validator.FieldError, fieldName string) string {
	if format, ok := errorMessages[fieldError.Tag()]; ok {
		return fmt.Sprintf(format, fieldName, fieldError.Param())
	}

	if fieldError.Param() != "" {
		return fmt.Sprintf("The %s field is invalid. (%s: %s)", fieldName, fieldError.Tag(), fieldError.Param())
	}

	return fmt.Sprintf("The %s field is invalid. (%s)", fieldName, fieldError.Tag())
}
