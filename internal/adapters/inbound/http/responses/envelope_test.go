package responses

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertJSON compares the JSON encoding of got with want, as a client would see it.
func assertJSON(t *testing.T, want string, got any) {
	t.Helper()

	raw, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, want, string(raw))
}

func newTestContext(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	c.Request.Host = "example.com"

	return c, w
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "unset", want: ""},
		{name: "string", value: "req-1", want: "req-1"},
		{name: "not a string", value: 42, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := newTestContext(t, "/")
			if tt.value != nil {
				c.Set(ContextRequestIDKey, tt.value)
			}

			assert.Equal(t, tt.want, RequestID(c))
		})
	}
}

func TestSuccess(t *testing.T) {
	t.Parallel()

	c, w := newTestContext(t, "/")
	c.Set(ContextRequestIDKey, "req-1")

	Success(c, http.StatusOK, gin.H{"id": 1})

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"ok":true,"code":2000001,"data":{"id":1},"meta":{"requestId":"req-1"}}`, w.Body.String())
}

func TestSuccessFor(t *testing.T) {
	t.Parallel()

	c, w := newTestContext(t, "/")

	SuccessFor(c, http.StatusCreated, ServicePosts, CaseSuccess, gin.H{"id": "p1"})

	require.Equal(t, http.StatusCreated, w.Code)
	assert.JSONEq(t, `{"ok":true,"code":2010401,"data":{"id":"p1"},"meta":{}}`, w.Body.String())
}

func TestSuccessPaginatedLaravelShape(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		page         int
		perPage      int
		total        int64
		wantCurrent  int
		wantLast     int
		wantFrom     *int
		wantTo       *int
		wantPrevNull bool
		wantNextNull bool
	}{
		{
			name:         "first page with more",
			page:         1,
			perPage:      15,
			total:        40,
			wantCurrent:  1,
			wantLast:     3,
			wantFrom:     new(1),
			wantTo:       new(15),
			wantPrevNull: true,
			wantNextNull: false,
		},
		{
			name:         "middle page",
			page:         2,
			perPage:      15,
			total:        40,
			wantCurrent:  2,
			wantLast:     3,
			wantFrom:     new(16),
			wantTo:       new(30),
			wantPrevNull: false,
			wantNextNull: false,
		},
		{
			name:         "empty result",
			page:         1,
			perPage:      15,
			total:        0,
			wantCurrent:  1,
			wantLast:     1,
			wantFrom:     nil,
			wantTo:       nil,
			wantPrevNull: true,
			wantNextNull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users?page=2&perPage=15&q=a", nil)
			c.Request.Host = "example.com"

			items := []gin.H{{"id": 1}}
			SuccessPaginated(c, http.StatusOK, items, tt.page, tt.perPage, tt.total)

			require.Equal(t, http.StatusOK, w.Code)

			var envelope Envelope
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.True(t, envelope.OK)
			require.Equal(t, BuildResponseCode(http.StatusOK, ServicePlatform, CaseSuccess), envelope.Code)
			require.NotNil(t, envelope.Links)
			require.NotNil(t, envelope.Links.First)
			require.NotNil(t, envelope.Links.Last)
			require.Equal(t, tt.wantPrevNull, envelope.Links.Prev == nil)
			require.Equal(t, tt.wantNextNull, envelope.Links.Next == nil)
			require.Contains(t, *envelope.Links.First, "page=1")
			require.Contains(t, *envelope.Links.First, "q=a")

			metaBytes, err := json.Marshal(envelope.Meta)
			require.NoError(t, err)

			var meta PaginationMeta
			require.NoError(t, json.Unmarshal(metaBytes, &meta))
			require.Equal(t, tt.wantCurrent, meta.CurrentPage)
			require.Equal(t, tt.wantLast, meta.LastPage)
			require.Equal(t, tt.perPage, meta.PerPage)
			require.Equal(t, tt.total, meta.Total)
			require.Equal(t, "http://example.com/api/v1/users", meta.Path)

			if tt.wantFrom == nil {
				require.Nil(t, meta.From)
			} else {
				require.NotNil(t, meta.From)
				require.Equal(t, *tt.wantFrom, *meta.From)
			}

			if tt.wantTo == nil {
				require.Nil(t, meta.To)
			} else {
				require.NotNil(t, meta.To)
				require.Equal(t, *tt.wantTo, *meta.To)
			}
		})
	}
}

func TestSuccessPaginatedFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts PageOpts
		want string
	}{
		{
			name: "invalid page and per page fall back to defaults",
			opts: PageOpts{Data: []gin.H{}, Page: 0, PerPage: 0, Total: 20},
			want: `{
				"ok": true, "code": 2000001, "data": [],
				"links": {
					"first": "http://example.com/api/v1/posts?page=1",
					"last": "http://example.com/api/v1/posts?page=2",
					"prev": null,
					"next": "http://example.com/api/v1/posts?page=2"
				},
				"meta": {"currentPage": 1, "from": 1, "lastPage": 2, "path": "http://example.com/api/v1/posts", "perPage": 15, "to": 15, "total": 20}
			}`,
		},
		{
			name: "last partial page",
			opts: PageOpts{Service: ServicePosts, Data: []gin.H{}, Page: 2, PerPage: 15, Total: 20},
			want: `{
				"ok": true, "code": 2000401, "data": [],
				"links": {
					"first": "http://example.com/api/v1/posts?page=1",
					"last": "http://example.com/api/v1/posts?page=2",
					"prev": "http://example.com/api/v1/posts?page=1",
					"next": null
				},
				"meta": {"currentPage": 2, "from": 16, "lastPage": 2, "path": "http://example.com/api/v1/posts", "perPage": 15, "to": 20, "total": 20}
			}`,
		},
		{
			name: "page beyond the last has no range",
			opts: PageOpts{Service: ServicePosts, Data: []gin.H{}, Page: 5, PerPage: 10, Total: 20},
			want: `{
				"ok": true, "code": 2000401, "data": [],
				"links": {
					"first": "http://example.com/api/v1/posts?page=1",
					"last": "http://example.com/api/v1/posts?page=2",
					"prev": "http://example.com/api/v1/posts?page=4",
					"next": null
				},
				"meta": {"currentPage": 5, "from": null, "lastPage": 2, "path": "http://example.com/api/v1/posts", "perPage": 10, "to": null, "total": 20}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, w := newTestContext(t, "/api/v1/posts?page=9")

			SuccessPaginatedFor(c, http.StatusOK, tt.opts)

			require.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, tt.want, w.Body.String())
		})
	}
}

func TestFailure(t *testing.T) {
	t.Parallel()

	c, w := newTestContext(t, "/")
	c.Set(ContextRequestIDKey, "req-1")

	Failure(c, http.StatusNotFound, ErrorCodeNotFound, "Post not found.")

	require.Equal(t, http.StatusNotFound, w.Code)
	assert.True(t, c.IsAborted())
	assert.JSONEq(t, `{
		"ok": false, "code": 4040005, "meta": {"requestId": "req-1"},
		"error": {"code": "not_found", "message": "Post not found."}
	}`, w.Body.String())
}

func TestFailureWithDetails(t *testing.T) {
	t.Parallel()

	c, w := newTestContext(t, "/")

	FailureWithDetails(c, http.StatusBadRequest, ErrorCodeValidation, "The given data was invalid.",
		gin.H{"title": []string{"The title field is required."}})

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{
		"ok": false, "code": 4000002, "meta": {},
		"message": "The given data was invalid.",
		"errors":  {"title": ["The title field is required."]},
		"error": {
			"code": "validation_error", "message": "The given data was invalid.",
			"details": {"title": ["The title field is required."]}
		}
	}`, w.Body.String())
}

func TestRecordError(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want []error
	}{
		{name: "nil is ignored", err: nil, want: nil},
		{name: "error is attached", err: errBoom, want: []error{errBoom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := newTestContext(t, "/")

			RecordError(c, tt.err)

			var got []error
			for _, e := range c.Errors {
				got = append(got, e.Err)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInternal(t *testing.T) {
	t.Parallel()

	errDB := errors.New("db: connection reset")
	c, w := newTestContext(t, "/")

	Internal(c, errDB, "Could not load posts.")

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Len(t, c.Errors, 1)
	require.ErrorIs(t, c.Errors.Last().Err, errDB)
	assert.JSONEq(t, `{
		"ok": false, "code": 5000009, "meta": {},
		"error": {"code": "internal_error", "message": "Could not load posts."}
	}`, w.Body.String())
}

func TestFailureFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		opts   FailureOpts
		want   string
	}{
		{
			name:   "explicit service and case",
			status: http.StatusConflict,
			opts:   FailureOpts{Service: ServiceComments, Case: CaseUnprocessable, Code: "comment.not_editable", Message: "Too late."},
			want:   `{"ok":false,"code":4090307,"meta":{},"error":{"code":"comment.not_editable","message":"Too late."}}`,
		},
		{
			name:   "zero service and case are derived",
			status: http.StatusTooManyRequests,
			opts:   FailureOpts{Code: "rate_limited", Message: "Slow down."},
			want:   `{"ok":false,"code":4290008,"meta":{},"error":{"code":"rate_limited","message":"Slow down."}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, w := newTestContext(t, "/")

			FailureFor(c, tt.status, tt.opts)

			require.Equal(t, tt.status, w.Code)
			assert.True(t, c.IsAborted())
			assert.JSONEq(t, tt.want, w.Body.String())
		})
	}
}

func TestAbsolutePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		headers map[string]string
		tls     bool
		want    string
	}{
		{name: "plain http", host: "example.com", want: "http://example.com/a"},
		{name: "tls", host: "example.com", tls: true, want: "https://example.com/a"},
		{name: "forwarded https", host: "example.com", headers: map[string]string{"X-Forwarded-Proto": "https"}, want: "https://example.com/a"},
		{name: "forwarded http overrides tls", host: "example.com", tls: true, headers: map[string]string{"X-Forwarded-Proto": "http"}, want: "http://example.com/a"},
		{name: "unknown forwarded proto is ignored", host: "example.com", headers: map[string]string{"X-Forwarded-Proto": "ftp"}, want: "http://example.com/a"},
		{name: "forwarded host", host: "internal:8080", headers: map[string]string{"X-Forwarded-Host": "blog.example.com"}, want: "http://blog.example.com/a"},
		{name: "missing host", host: "", want: "http://localhost/a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, _ := newTestContext(t, "/a")
			c.Request.Host = tt.host

			for k, v := range tt.headers {
				c.Request.Header.Set(k, v)
			}

			if tt.tls {
				c.Request.TLS = &tls.ConnectionState{}
			}

			assert.Equal(t, tt.want, absolutePath(c))
		})
	}
}
