package requests

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

const formField = "_form"

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

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

// validationErrorDetails builds a Laravel-like errors bag keyed by dotted JSON
// path (items.0.kind):
//
//	{ "email": ["The email field is required."], ... }
//
// Errors that belong to no single field (malformed or missing body) use _form.
func validationErrorDetails(err error) map[string][]string {
	if verrs, ok := errors.AsType[validator.ValidationErrors](err); ok {
		bag := make(map[string][]string, len(verrs))
		for _, fe := range verrs {
			field := fieldPath(fe)
			bag[field] = append(bag[field], validationMessage(field, fe))
		}

		return bag
	}

	if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok && typeErr.Field != "" {
		return map[string][]string{typeErr.Field: {typeMessage(typeErr.Field, typeErr.Type)}}
	}

	return map[string][]string{formField: {bodyMessage(err)}}
}

// fieldPath turns a validator namespace such as CreateIssue.lists[0] into lists.0.
func fieldPath(fe validator.FieldError) string {
	_, path, ok := strings.Cut(fe.Namespace(), ".")
	if !ok {
		path = fe.Field()
	}

	path = strings.NewReplacer("[", ".", "]", "").Replace(path)
	if path == "" {
		return formField
	}

	return path
}

func bodyMessage(err error) string {
	if syntaxErr, ok := errors.AsType[*json.SyntaxError](err); ok {
		return fmt.Sprintf("The request body must be valid JSON (syntax error at byte %d).", syntaxErr.Offset)
	}

	switch {
	case errors.Is(err, io.EOF):
		return "The request body is required."
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "The request body must be valid JSON."
	default:
		return "The request body is invalid."
	}
}

func typeMessage(field string, typ reflect.Type) string {
	if typ == nil {
		return fmt.Sprintf("The %s field is invalid.", field)
	}

	if reflect.PointerTo(typ).Implements(textUnmarshalerType) {
		return fmt.Sprintf("The %s field must be a string.", field)
	}

	if rule, ok := typeRules[typ.Kind()]; ok {
		return fmt.Sprintf("The %s field %s.", field, rule)
	}

	return fmt.Sprintf("The %s field is invalid.", field)
}

const (
	ruleInteger = "must be an integer"
	ruleNumber  = "must be a number"
	ruleArray   = "must be an array"
	ruleObject  = "must be an object"
)

var typeRules = map[reflect.Kind]string{
	reflect.String:  "must be a string",
	reflect.Bool:    "must be true or false",
	reflect.Int:     ruleInteger,
	reflect.Int8:    ruleInteger,
	reflect.Int16:   ruleInteger,
	reflect.Int32:   ruleInteger,
	reflect.Int64:   ruleInteger,
	reflect.Uint:    ruleInteger,
	reflect.Uint8:   ruleInteger,
	reflect.Uint16:  ruleInteger,
	reflect.Uint32:  ruleInteger,
	reflect.Uint64:  ruleInteger,
	reflect.Float32: ruleNumber,
	reflect.Float64: ruleNumber,
	reflect.Slice:   ruleArray,
	reflect.Array:   ruleArray,
	reflect.Map:     ruleObject,
	reflect.Struct:  ruleObject,
}

func validationMessage(field string, fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("The %s field is required.", field)
	case "email":
		return fmt.Sprintf("The %s field must be a valid email address.", field)
	case "uuid":
		return fmt.Sprintf("The %s field must be a valid UUID.", field)
	case "oneof":
		return fmt.Sprintf("The selected %s is invalid.", field)
	case "datetime":
		return fmt.Sprintf("The %s field must match the format %s.", field, fe.Param())
	case "min":
		return sizeMessage(field, fe, "must be at least %s", "must have at least %s items")
	case "max":
		return sizeMessage(field, fe, "must not be greater than %s", "must not have more than %s items")
	case "gt":
		return fmt.Sprintf("The %s field must be greater than %s.", field, fe.Param())
	case "gte":
		return fmt.Sprintf("The %s field must be greater than or equal to %s.", field, fe.Param())
	case "eqfield":
		return fmt.Sprintf("The %s field must match %s.", field, lowerFirst(fe.Param()))
	default:
		return fmt.Sprintf("The %s field is invalid.", field)
	}
}

// sizeMessage words min/max rules by kind, like Laravel's string/numeric/array variants.
func sizeMessage(field string, fe validator.FieldError, bound, items string) string {
	kind := fe.Kind()
	if kind == reflect.String {
		return fmt.Sprintf("The %s field "+bound+" characters.", field, fe.Param())
	}

	if kind == reflect.Slice || kind == reflect.Array || kind == reflect.Map {
		return fmt.Sprintf("The %s field "+items+".", field, fe.Param())
	}

	return fmt.Sprintf("The %s field "+bound+".", field, fe.Param())
}

// lowerFirst turns a Go field name such as NewPassword into its JSON name, newPassword.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}

	r, size := utf8.DecodeRuneInString(s)

	return string(unicode.ToLower(r)) + s[size:]
}
