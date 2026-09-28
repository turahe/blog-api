package requests

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
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
	maps.Copy(fields, base)

	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic(fmt.Sprintf("with: key %v is not a string", pairs[i]))
		}

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
func msgEmail(f string) string    { return "The " + f + " must be a valid email address." }
func msgUUID(f string) string     { return "The " + f + " must be a valid UUID." }
func msgDate(f string) string     { return "The " + f + " must be a valid date and time." }

func msgOneOf(f, options string) string {
	return "The " + f + " must be one of: " + options + "."
}

// min and max read as character counts whatever the field kind.
func msgMin(f string, n int) string {
	return "The " + f + " must be at least " + strconv.Itoa(n) + " characters."
}

func msgMax(f string, n int) string {
	return "The " + f + " may not be greater than " + strconv.Itoa(n) + " characters."
}

func msgMinChars(f string, n int) string { return msgMin(f, n) }
func msgMaxChars(f string, n int) string { return msgMax(f, n) }
func msgMinItems(f string, n int) string { return msgMin(f, n) }
func msgMaxItems(f string, n int) string { return msgMax(f, n) }

func msgGeneral(message string) map[string][]string {
	return map[string][]string{"general": {message}}
}

// bindDetails binds body into dst like BindJSON and returns the validation bag, or nil when valid.
func bindDetails(t *testing.T, body string, dst any) map[string][]string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	if err := binding.JSON.Bind(req, dst); err != nil {
		return FormatValidationError(err)
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
		Query  string `form:"query"           binding:"required"`
	}

	err := binding.Validator.ValidateStruct(&sample{})
	require.Error(t, err)

	assert.Equal(t, map[string][]string{
		"named":  {"The named field is required."},
		"plain":  {"The plain field is required."},
		"hidden": {"The hidden field is required."},
		"query":  {"The query field is required."},
	}, FormatValidationError(err))
}

func TestBindJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	code := responses.BuildResponseCode(http.StatusBadRequest, responses.ServicePlatform, responses.CaseCodeForStatus(http.StatusBadRequest))

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
			wantJSON: fmt.Sprintf(`{"ok":false,"code":%d,"meta":{"requestId":"req-1"},
				"message":"The given data was invalid.",
				"errors":{"email":["The email must be a valid email address."],"password":["The password field is required."]},
				"error":{
				"code":"validation_error","message":"The given data was invalid.",
				"details":{"email":["The email must be a valid email address."],"password":["The password field is required."]}}}`,
				code),
		},
		{
			name:   "empty body is a general error",
			body:   ``,
			wantOK: false,
			wantJSON: fmt.Sprintf(`{"ok":false,"code":%d,"meta":{"requestId":"req-1"},
				"message":"The given data was invalid.",
				"errors":{"general":["EOF"]},
				"error":{
				"code":"validation_error","message":"The given data was invalid.",
				"details":{"general":["EOF"]}}}`,
				code),
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
	assert.Equal(t, map[string]any{"general": []any{"boom"}}, envelope.Error.Details)
}

func TestFormatValidationError(t *testing.T) {
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
			want: msgGeneral("invalid character '/' looking for beginning of object key string"),
		},
		{
			name: "truncated body",
			body: `{"title": "a"`,
			dst:  &CreatePost{},
			want: msgGeneral("unexpected EOF"),
		},
		{
			name: "wrong JSON type is a general error",
			body: `{"title": "a", "slug": "a", "content": "b", "categoryId": 1}`,
			dst:  &CreatePost{},
			want: msgGeneral("json: cannot unmarshal number into Go struct field CreatePost.categoryId of type string"),
		},
		{
			name: "wrong top-level type is a general error",
			body: `[]`,
			dst:  &CreatePost{},
			want: msgGeneral("json: cannot unmarshal array into Go value of type requests.CreatePost"),
		},
		{
			name: "rule failures",
			body: `{"title": "` + strings.Repeat("x", 256) + `", "categoryId": "nope"}`,
			dst:  &CreatePost{},
			want: map[string][]string{
				"title":      {msgMax("title", 255)},
				"slug":       {msgRequired("slug")},
				"content":    {msgRequired("content")},
				"categoryId": {msgUUID("categoryId")},
			},
		},
		{
			name: "nested fields use their own name",
			body: `{"items": [{"mediaAssetId": "nope", "sortOrder": -1}]}`,
			dst:  &ReplacePostMedia{},
			want: map[string][]string{
				"mediaAssetId": {msgUUID("mediaAssetId")},
				"kind":         {msgRequired("kind")},
				"sortOrder":    {"The sortOrder must be greater than or equal to 0."},
			},
		},
		{
			name: "min on a slice and oneof",
			body: `{"subject": "s", "bodyMarkdown": "b", "lists": [], "status": "sent"}`,
			dst:  &NewsletterIssueCreate{},
			want: map[string][]string{
				"lists":  {msgMin("lists", 1)},
				"status": {msgOneOf("status", "draft scheduled queued")},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			err := binding.JSON.Bind(req, tc.dst)
			require.Error(t, err)
			require.Equal(t, tc.want, FormatValidationError(err))
		})
	}
}

func TestFormatValidationErrorWithoutNamespace(t *testing.T) {
	t.Parallel()

	engine, ok := binding.Validator.Engine().(*validator.Validate)
	require.True(t, ok)

	err := engine.Var("", "required")
	require.Error(t, err)

	assert.Equal(t, map[string][]string{"": {"The  field is required."}}, FormatValidationError(err))
}

// fakeFieldError drives getFieldName and getErrorMessage with names and tags the validator
// never produces on its own.
type fakeFieldError struct {
	validator.FieldError

	tag, param, field, structField, namespace string
}

func (f fakeFieldError) Tag() string         { return f.tag }
func (f fakeFieldError) Param() string       { return f.param }
func (f fakeFieldError) Field() string       { return f.field }
func (f fakeFieldError) StructField() string { return f.structField }
func (f fakeFieldError) Namespace() string   { return f.namespace }

func TestGetFieldName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fe   fakeFieldError
		want string
	}{
		{name: "registered json name", fe: fakeFieldError{field: "fullName", structField: "FullName", namespace: "Req.fullName"}, want: "fullName"},
		{name: "no json name", fe: fakeFieldError{field: "FullName", structField: "FullName", namespace: "Req.FullName"}, want: "fullName"},
		{name: "struct field without field name", fe: fakeFieldError{structField: "FullName", namespace: "Req.FullName"}, want: "fullName"},
		{name: "no struct context keeps field", fe: fakeFieldError{field: "Email", structField: "Email", namespace: "Email"}, want: "Email"},
		{name: "struct field only", fe: fakeFieldError{structField: "Email"}, want: "email"},
		{name: "nothing", fe: fakeFieldError{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, getFieldName(tt.fe))
		})
	}
}

func TestToCamelCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{in: "", want: ""},
		{in: "FirstName", want: "firstName"},
		{in: "firstName", want: "firstName"},
		{in: "ID", want: "iD"},
		{in: "_x", want: "_x"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, toCamelCase(tt.in))
		})
	}
}

func TestGetErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag, param, want string
	}{
		{tag: "required", want: "The f field is required."},
		{tag: "email", want: "The f must be a valid email address."},
		{tag: "min", param: "3", want: "The f must be at least 3 characters."},
		{tag: "max", param: "9", want: "The f may not be greater than 9 characters."},
		{tag: "len", param: "4", want: "The f must be exactly 4 characters."},
		{tag: "numeric", want: "The f must be a number."},
		{tag: "alpha", want: "The f may only contain letters."},
		{tag: "alphanum", want: "The f may only contain letters and numbers."},
		{tag: "url", want: "The f must be a valid URL."},
		{tag: "uuid", want: "The f must be a valid UUID."},
		{tag: "oneof", param: "a b", want: "The f must be one of: a b."},
		{tag: "gte", param: "0", want: "The f must be greater than or equal to 0."},
		{tag: "lte", param: "5", want: "The f must be less than or equal to 5."},
		{tag: "gt", param: "0", want: "The f must be greater than 0."},
		{tag: "lt", param: "5", want: "The f must be less than 5."},
		{tag: "eq", param: "x", want: "The f must be equal to x."},
		{tag: "ne", param: "x", want: "The f must not be equal to x."},
		{tag: "unique", want: "The f has already been taken."},
		{tag: "exists", want: "The selected f is invalid."},
		{tag: "date", want: "The f must be a valid date."},
		{tag: "datetime", param: "2006-01-02", want: "The f must be a valid date and time."},
		{tag: "timezone", want: "The f must be a valid timezone."},
		{tag: "json", want: "The f must be a valid JSON string."},
		{tag: "ip", want: "The f must be a valid IP address."},
		{tag: "ipv4", want: "The f must be a valid IPv4 address."},
		{tag: "ipv6", want: "The f must be a valid IPv6 address."},
		{tag: "base64", want: "The f must be a valid base64 string."},
		{tag: "required_if", param: "kind cover", want: "The f field is required when kind cover is present."},
		{tag: "required_unless", param: "kind cover", want: "The f field is required unless kind cover is present."},
		{tag: "required_with", param: "Other", want: "The f field is required when Other is present."},
		{tag: "required_without", param: "Other", want: "The f field is required when Other is not present."},
		{tag: "eqfield", param: "Password", want: "The f field is invalid. (eqfield: Password)"},
		{tag: "e164", want: "The f field is invalid. (e164)"},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, getErrorMessage(fakeFieldError{tag: tt.tag, param: tt.param}, "f"))
		})
	}
}

type ruleSample struct {
	Required string   `json:"required" binding:"required"`
	Email    string   `json:"email"    binding:"omitempty,email"`
	ID       string   `json:"id"       binding:"omitempty,uuid"`
	Choice   string   `json:"choice"   binding:"omitempty,oneof=a b"`
	Date     string   `json:"date"     binding:"omitempty,datetime=2006-01-02"`
	MinStr   string   `json:"minStr"   binding:"omitempty,min=3"`
	MaxStr   string   `json:"maxStr"   binding:"max=2"`
	MinList  []string `json:"minList"  binding:"omitempty,min=2"`
	MinNum   int      `json:"minNum"   binding:"min=5"`
	Positive int      `json:"positive" binding:"gt=0"`
	Password string   `json:"password"`
	Confirm  string   `json:"confirm"  binding:"eqfield=Password"`
	Site     string   `json:"site"     binding:"omitempty,url"`
	Code     string   `json:"code"     binding:"omitempty,len=4"`
	Zone     string   `json:"zone"     binding:"omitempty,timezone"`
}

func TestValidationMessagesThroughBinding(t *testing.T) {
	t.Parallel()

	base := map[string]any{"required": "x", "minNum": 5, "positive": 1}

	runBindCases[ruleSample](t, []bindCase{
		{name: "valid", body: with(base, "email", "a@example.com", "confirm", "", "password", "")},
		{name: "required", body: with(base, "required", absent), want: errs("required", msgRequired("required"))},
		{name: "email", body: with(base, "email", "nope"), want: errs("email", msgEmail("email"))},
		{name: "uuid", body: with(base, "id", "nope"), want: errs("id", msgUUID("id"))},
		{name: "oneof", body: with(base, "choice", "c"), want: errs("choice", msgOneOf("choice", "a b"))},
		{name: "datetime", body: with(base, "date", "01/02/2026"), want: errs("date", msgDate("date"))},
		{name: "min string", body: with(base, "minStr", "ab"), want: errs("minStr", msgMin("minStr", 3))},
		{name: "max string", body: with(base, "maxStr", "abc"), want: errs("maxStr", msgMax("maxStr", 2))},
		{name: "min slice", body: with(base, "minList", []string{"a"}), want: errs("minList", msgMin("minList", 2))},
		{name: "min number", body: with(base, "minNum", 4), want: errs("minNum", msgMin("minNum", 5))},
		{name: "gt", body: with(base, "positive", 0), want: errs("positive", "The positive must be greater than 0.")},
		{name: "eqfield", body: with(base, "password", "a", "confirm", "b"), want: errs("confirm", "The confirm field is invalid. (eqfield: Password)")},
		{name: "url", body: with(base, "site", "not a url"), want: errs("site", "The site must be a valid URL.")},
		{name: "len", body: with(base, "code", "12345"), want: errs("code", "The code must be exactly 4 characters.")},
		{name: "timezone", body: with(base, "zone", "Mars/Olympus"), want: errs("zone", "The zone must be a valid timezone.")},
	})
}
