package requests

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// absent removes a key from the base object in with.
type absentField struct{}

var absent absentField

// with returns base as JSON after applying key/value pairs; a value of absent deletes the key.
func with(base map[string]any, pairs ...any) string {
	fields := make(map[string]any, len(base))
	for k, v := range base {
		fields[k] = v
	}

	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i].(string)
		if pairs[i+1] == absent {
			delete(fields, key)
			continue
		}

		fields[key] = pairs[i+1]
	}

	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}

	return string(raw)
}

func long(n int) string { return strings.Repeat("x", n) }

func errs(field, message string) map[string][]string {
	return map[string][]string{field: {message}}
}

func msgRequired(f string) string { return "The " + f + " field is required." }
func msgEmail(f string) string    { return "The " + f + " field must be a valid email address." }
func msgUUID(f string) string     { return "The " + f + " field must be a valid UUID." }
func msgOneOf(f string) string    { return "The selected " + f + " is invalid." }
func msgDate(f string) string     { return "The " + f + " field must match the format 2006-01-02." }
func msgMinChars(f string, n int) string {
	return "The " + f + " field must be at least " + strconv.Itoa(n) + " characters."
}

func msgMaxChars(f string, n int) string {
	return "The " + f + " field must not be greater than " + strconv.Itoa(n) + " characters."
}

func msgMinItems(f string, n int) string {
	return "The " + f + " field must have at least " + strconv.Itoa(n) + " items."
}

func msgMaxItems(f string, n int) string {
	return "The " + f + " field must not have more than " + strconv.Itoa(n) + " items."
}

func msgMin(f string, n int) string {
	return "The " + f + " field must be at least " + strconv.Itoa(n) + "."
}
func msgMax(f string, n int) string {
	return "The " + f + " field must not be greater than " + strconv.Itoa(n) + "."
}

// bindDetails binds body into dst like BindJSON and returns the validation bag, or nil when valid.
func bindDetails(t *testing.T, body string, dst any) map[string][]string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	if err := binding.JSON.Bind(req, dst); err != nil {
		return validationErrorDetails(err)
	}

	return nil
}

type bindCase struct {
	name string
	body string
	want map[string][]string
}

// runBindCases binds each body into a fresh T; want nil means the body is valid.
func runBindCases[T any](t *testing.T, cases []bindCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var dst T
			assert.Equal(t, tc.want, bindDetails(t, tc.body, &dst))
		})
	}
}

func TestInitRegistersJSONFieldNames(t *testing.T) {
	t.Parallel()

	type sample struct {
		Named  string `json:"named,omitempty" binding:"required"`
		Plain  string `binding:"required"`
		Hidden string `json:"-"               binding:"required"`
	}

	err := binding.Validator.ValidateStruct(&sample{})
	require.Error(t, err)

	assert.Equal(t, map[string][]string{
		"named":  {"The named field is required."},
		"Plain":  {"The Plain field is required."},
		"Hidden": {"The Hidden field is required."},
	}, validationErrorDetails(err))
}

func TestBindJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		body     string
		wantOK   bool
		wantDst  Login
		wantJSON string
	}{
		{
			name:    "valid body binds",
			body:    `{"email":"a@example.com","password":"pw","remember":true}`,
			wantOK:  true,
			wantDst: Login{Email: "a@example.com", Password: "pw", Remember: true},
		},
		{
			name:   "invalid body writes a validation envelope",
			body:   `{"email":"nope"}`,
			wantOK: false,
			wantJSON: fmt.Sprintf(`{"ok":false,"code":%d,"meta":{"requestId":"req-1"},"error":{
				"code":"validation_error","message":"The given data was invalid.",
				"details":{"email":["The email field must be a valid email address."],"password":["The password field is required."]}}}`,
				responses.BuildResponseCode(http.StatusBadRequest, responses.ServicePlatform, responses.CaseCodeForStatus(http.StatusBadRequest))),
		},
		{
			name:   "empty body is a form error",
			body:   ``,
			wantOK: false,
			wantJSON: fmt.Sprintf(`{"ok":false,"code":%d,"meta":{"requestId":"req-1"},"error":{
				"code":"validation_error","message":"The given data was invalid.",
				"details":{"_form":["The request body is required."]}}}`,
				responses.BuildResponseCode(http.StatusBadRequest, responses.ServicePlatform, responses.CaseCodeForStatus(http.StatusBadRequest))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(responses.ContextRequestIDKey, "req-1")

			var dst Login

			require.Equal(t, tt.wantOK, BindJSON(c, &dst))

			if tt.wantOK {
				assert.Equal(t, tt.wantDst, dst)
				assert.False(t, c.IsAborted())
				assert.Empty(t, w.Body.String())

				return
			}

			assert.True(t, c.IsAborted())
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.JSONEq(t, tt.wantJSON, w.Body.String())
		})
	}
}

func TestFailValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)

	FailValidation(c, errors.New("boom"))

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, envelope.OK)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, responses.ErrorCodeValidation, envelope.Error.Code)
	assert.Equal(t, map[string]any{"_form": []any{"The request body is invalid."}}, envelope.Error.Details)
}

func TestValidationMessageHelpers(t *testing.T) {
	t.Parallel()
	require.Equal(t, "newPassword", lowerFirst("NewPassword"))
	require.Equal(t, map[string][]string{"_form": {"The request body is required."}}, validationErrorDetails(io.EOF))
}

func TestValidationErrorDetails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		dst  any
		want map[string][]string
	}{
		{
			name: "comment and raw newline are a syntax error",
			body: "{\n  \"title\": \"a\",\n  // \"slug\": \"a\",\n  \"content\": \"line\nline\"\n}",
			dst:  &CreatePost{},
			want: map[string][]string{"_form": {"The request body must be valid JSON (syntax error at byte 21)."}},
		},
		{
			name: "truncated body",
			body: `{"title": "a"`,
			dst:  &CreatePost{},
			want: map[string][]string{"_form": {"The request body must be valid JSON."}},
		},
		{
			name: "wrong JSON type is keyed by field",
			body: `{"title": "a", "slug": "a", "content": "b", "categoryId": 1}`,
			dst:  &CreatePost{},
			want: map[string][]string{"categoryId": {"The categoryId field must be a string."}},
		},
		{
			name: "wrong JSON type for an integer",
			body: `{"items": [{"mediaAssetId": "x", "kind": "cover", "sortOrder": "1"}]}`,
			dst:  &ReplacePostMedia{},
			want: map[string][]string{"items.0.sortOrder": {"The items.0.sortOrder field must be an integer."}},
		},
		{
			name: "wrong top-level type is a form error",
			body: `[]`,
			dst:  &CreatePost{},
			want: map[string][]string{"_form": {"The request body is invalid."}},
		},
		{
			name: "rule failures use Laravel wording",
			body: `{"title": "` + strings.Repeat("x", 256) + `", "categoryId": "nope"}`,
			dst:  &CreatePost{},
			want: map[string][]string{
				"title":      {"The title field must not be greater than 255 characters."},
				"slug":       {"The slug field is required."},
				"content":    {"The content field is required."},
				"categoryId": {"The categoryId field must be a valid UUID."},
			},
		},
		{
			name: "nested fields use dotted paths",
			body: `{"items": [{"mediaAssetId": "nope", "sortOrder": -1}]}`,
			dst:  &ReplacePostMedia{},
			want: map[string][]string{
				"items.0.mediaAssetId": {"The items.0.mediaAssetId field must be a valid UUID."},
				"items.0.kind":         {"The items.0.kind field is required."},
				"items.0.sortOrder":    {"The items.0.sortOrder field must be greater than or equal to 0."},
			},
		},
		{
			name: "size rules are worded by kind",
			body: `{"subject": "s", "bodyMarkdown": "b", "lists": [], "status": "sent"}`,
			dst:  &NewsletterIssueCreate{},
			want: map[string][]string{
				"lists":  {"The lists field must have at least 1 items."},
				"status": {"The selected status is invalid."},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			err := binding.JSON.Bind(req, tc.dst)
			require.Error(t, err)
			require.Equal(t, tc.want, validationErrorDetails(err))
		})
	}
}

func TestFieldPath(t *testing.T) {
	t.Parallel()

	engine, ok := binding.Validator.Engine().(*validator.Validate)
	require.True(t, ok)

	err := engine.Var("", "required")
	require.Error(t, err)

	assert.Equal(t, map[string][]string{"_form": {"The _form field is required."}}, validationErrorDetails(err),
		"a variable without a namespace belongs to the whole form")
}

func TestBodyMessage(t *testing.T) {
	t.Parallel()

	var syntaxErr *json.SyntaxError
	require.ErrorAs(t, json.Unmarshal([]byte(`{"a":}`), &map[string]any{}), &syntaxErr)

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "syntax error", err: syntaxErr, want: "The request body must be valid JSON (syntax error at byte 6)."},
		{name: "empty body", err: io.EOF, want: "The request body is required."},
		{name: "wrapped empty body", err: fmt.Errorf("decode: %w", io.EOF), want: "The request body is required."},
		{name: "truncated body", err: io.ErrUnexpectedEOF, want: "The request body must be valid JSON."},
		{name: "anything else", err: errors.New("boom"), want: "The request body is invalid."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, bodyMessage(tt.err))
		})
	}
}

func TestTypeMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{name: "unknown type", typ: nil, want: "The f field is invalid."},
		{name: "text unmarshaler", typ: reflect.TypeFor[uuid.UUID](), want: "The f field must be a string."},
		{name: "string", typ: reflect.TypeFor[string](), want: "The f field must be a string."},
		{name: "bool", typ: reflect.TypeFor[bool](), want: "The f field must be true or false."},
		{name: "int64", typ: reflect.TypeFor[int64](), want: "The f field must be an integer."},
		{name: "uint8", typ: reflect.TypeFor[uint8](), want: "The f field must be an integer."},
		{name: "float32", typ: reflect.TypeFor[float32](), want: "The f field must be a number."},
		{name: "slice", typ: reflect.TypeFor[[]string](), want: "The f field must be an array."},
		{name: "array", typ: reflect.TypeFor[[2]int](), want: "The f field must be an array."},
		{name: "map", typ: reflect.TypeFor[map[string]int](), want: "The f field must be an object."},
		{name: "struct", typ: reflect.TypeFor[struct{ A int }](), want: "The f field must be an object."},
		{name: "kind without a rule", typ: reflect.TypeFor[chan int](), want: "The f field is invalid."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, typeMessage("f", tt.typ))
		})
	}
}

type ruleSample struct {
	Required string         `json:"required" binding:"required"`
	Email    string         `json:"email"    binding:"omitempty,email"`
	ID       string         `json:"id"       binding:"omitempty,uuid"`
	Choice   string         `json:"choice"   binding:"omitempty,oneof=a b"`
	Date     string         `json:"date"     binding:"omitempty,datetime=2006-01-02"`
	MinStr   string         `json:"minStr"   binding:"omitempty,min=3"`
	MaxStr   string         `json:"maxStr"   binding:"max=2"`
	MinList  []string       `json:"minList"  binding:"omitempty,min=2"`
	MaxList  []string       `json:"maxList"  binding:"max=1"`
	MaxMap   map[string]int `json:"maxMap"   binding:"max=1"`
	MinNum   int            `json:"minNum"   binding:"min=5"`
	MaxNum   int            `json:"maxNum"   binding:"max=5"`
	Positive int            `json:"positive" binding:"gt=0"`
	NonNeg   int            `json:"nonNeg"   binding:"gte=0"`
	Password string         `json:"password"`
	Confirm  string         `json:"confirm"  binding:"eqfield=Password"`
	Site     string         `json:"site"     binding:"omitempty,url"`
}

func TestValidationMessage(t *testing.T) {
	t.Parallel()

	base := map[string]any{"required": "x", "minNum": 5, "positive": 1}

	runBindCases[ruleSample](t, []bindCase{
		{name: "valid", body: with(base, "email", "a@example.com", "confirm", "", "password", "")},
		{name: "required", body: with(base, "required", absent), want: errs("required", "The required field is required.")},
		{name: "email", body: with(base, "email", "nope"), want: errs("email", "The email field must be a valid email address.")},
		{name: "uuid", body: with(base, "id", "nope"), want: errs("id", "The id field must be a valid UUID.")},
		{name: "oneof", body: with(base, "choice", "c"), want: errs("choice", "The selected choice is invalid.")},
		{name: "datetime", body: with(base, "date", "01/02/2026"), want: errs("date", "The date field must match the format 2006-01-02.")},
		{name: "min string", body: with(base, "minStr", "ab"), want: errs("minStr", "The minStr field must be at least 3 characters.")},
		{name: "max string", body: with(base, "maxStr", "abc"), want: errs("maxStr", "The maxStr field must not be greater than 2 characters.")},
		{name: "min slice", body: with(base, "minList", []string{"a"}), want: errs("minList", "The minList field must have at least 2 items.")},
		{name: "max slice", body: with(base, "maxList", []string{"a", "b"}), want: errs("maxList", "The maxList field must not have more than 1 items.")},
		{name: "max map", body: with(base, "maxMap", map[string]int{"a": 1, "b": 2}), want: errs("maxMap", "The maxMap field must not have more than 1 items.")},
		{name: "min number", body: with(base, "minNum", 4), want: errs("minNum", "The minNum field must be at least 5.")},
		{name: "max number", body: with(base, "maxNum", 6), want: errs("maxNum", "The maxNum field must not be greater than 5.")},
		{name: "gt", body: with(base, "positive", 0), want: errs("positive", "The positive field must be greater than 0.")},
		{name: "gte", body: with(base, "nonNeg", -1), want: errs("nonNeg", "The nonNeg field must be greater than or equal to 0.")},
		{name: "eqfield", body: with(base, "password", "a", "confirm", "b"), want: errs("confirm", "The confirm field must match password.")},
		{name: "other rule", body: with(base, "site", "not a url"), want: errs("site", "The site field is invalid.")},
	})
}

func TestLowerFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{in: "", want: ""},
		{in: "NewPassword", want: "newPassword"},
		{in: "already", want: "already"},
		{in: "Ärger", want: "ärger"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, lowerFirst(tt.in))
		})
	}
}
