package responses

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// ContextRequestIDKey stores the correlation id on the Gin context.
const ContextRequestIDKey = "request_id"

// Envelope is the standard API response wrapper.
// Paginated lists add Links and Laravel-style pagination fields on Meta.
// Code is the packed response code from BuildResponseCode.
type Envelope struct {
	OK    bool       `json:"ok"`
	Code  int        `json:"code"`
	Data  any        `json:"data,omitempty"`
	Links *PageLinks `json:"links,omitempty"`
	Meta  any        `json:"meta,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

// Meta is the non-paginated success/error meta (request correlation only).
type Meta struct {
	RequestID string `json:"request_id,omitempty"`
}

// PageLinks is Laravel-style pagination link URLs (null when unavailable).
type PageLinks struct {
	First *string `json:"first"`
	Last  *string `json:"last"`
	Prev  *string `json:"prev"`
	Next  *string `json:"next"`
}

// PaginationMeta is Laravel-style length-aware pagination meta.
type PaginationMeta struct {
	RequestID   string `json:"request_id,omitempty"`
	CurrentPage int    `json:"current_page"`
	From        *int   `json:"from"`
	LastPage    int    `json:"last_page"`
	Path        string `json:"path"`
	PerPage     int    `json:"per_page"`
	To          *int   `json:"to"`
	Total       int64  `json:"total"`
}

// ErrorBody is the machine-readable error payload.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// RequestID returns the request correlation id from context (empty if unset).
func RequestID(c *gin.Context) string {
	value, _ := c.Get(ContextRequestIDKey)
	id, _ := value.(string)
	return id
}

// Success writes a platform success envelope.
func Success(c *gin.Context, status int, data any) {
	SuccessFor(c, status, ServicePlatform, CaseSuccess, data)
}

// SuccessFor writes a success envelope with an explicit service and case code.
func SuccessFor(c *gin.Context, status, service, caseCode int, data any) {
	c.JSON(status, Envelope{
		OK:   true,
		Code: BuildResponseCode(status, service, caseCode),
		Data: data,
		Meta: &Meta{RequestID: RequestID(c)},
	})
}

// SuccessPaginated writes a Laravel-style paginated envelope for the platform service.
func SuccessPaginated(c *gin.Context, status int, data any, page, perPage int, total int64) {
	SuccessPaginatedFor(c, status, ServicePlatform, data, page, perPage, total)
}

// SuccessPaginatedFor is SuccessPaginated with an explicit service code (case = Success).
func SuccessPaginatedFor(c *gin.Context, status, service int, data any, page, perPage int, total int64) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 15
	}

	lastPage := int((total + int64(perPage) - 1) / int64(perPage))
	if lastPage < 1 {
		lastPage = 1
	}

	var from, to *int
	if total > 0 && page <= lastPage {
		f := (page-1)*perPage + 1
		t := page * perPage
		if int64(t) > total {
			t = int(total)
		}
		if f <= int(total) {
			from, to = &f, &t
		}
	}

	path := absolutePath(c)
	first := absolutePageURL(c, 1)
	last := absolutePageURL(c, lastPage)
	var prev, next *string
	if page > 1 {
		u := absolutePageURL(c, page-1)
		prev = &u
	}
	if page < lastPage {
		u := absolutePageURL(c, page+1)
		next = &u
	}

	c.JSON(status, Envelope{
		OK:   true,
		Code: BuildResponseCode(status, service, CaseSuccess),
		Data: data,
		Links: &PageLinks{
			First: &first,
			Last:  &last,
			Prev:  prev,
			Next:  next,
		},
		Meta: &PaginationMeta{
			RequestID:   RequestID(c),
			CurrentPage: page,
			From:        from,
			LastPage:    lastPage,
			Path:        path,
			PerPage:     perPage,
			To:          to,
			Total:       total,
		},
	})
}

// SuccessWithMeta is retained for rare non-pagination meta; prefer Success or SuccessPaginated.
func SuccessWithMeta(c *gin.Context, status int, data any, meta *Meta) {
	if meta == nil {
		meta = &Meta{RequestID: RequestID(c)}
	} else if meta.RequestID == "" {
		meta.RequestID = RequestID(c)
	}
	c.JSON(status, Envelope{
		OK:   true,
		Code: BuildResponseCode(status, ServicePlatform, CaseSuccess),
		Data: data,
		Meta: meta,
	})
}

// Failure writes a platform error envelope.
func Failure(c *gin.Context, status int, code, message string) {
	FailureFor(c, status, ServicePlatform, CaseCodeForStatus(status), code, message, nil)
}

// FailureWithDetails writes a platform error envelope with details.
func FailureWithDetails(c *gin.Context, status int, code, message string, details any) {
	FailureFor(c, status, ServicePlatform, CaseCodeForStatus(status), code, message, details)
}

// FailureFor writes an error envelope with an explicit service and case code.
func FailureFor(c *gin.Context, status, service, caseCode int, code, message string, details any) {
	c.AbortWithStatusJSON(status, Envelope{
		OK:    false,
		Code:  BuildResponseCode(status, service, caseCode),
		Meta:  &Meta{RequestID: RequestID(c)},
		Error: &ErrorBody{Code: code, Message: message, Details: details},
	})
}

func absolutePath(c *gin.Context) string {
	return requestScheme(c) + "://" + requestHost(c) + c.Request.URL.Path
}

func absolutePageURL(c *gin.Context, page int) string {
	q := c.Request.URL.Query()
	q.Set("page", strconv.Itoa(page))
	return absolutePath(c) + "?" + q.Encode()
}

func requestScheme(c *gin.Context) string {
	if proto := c.GetHeader("X-Forwarded-Proto"); proto == "https" || proto == "http" {
		return proto
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHost(c *gin.Context) string {
	if host := c.GetHeader("X-Forwarded-Host"); host != "" {
		return host
	}
	if c.Request.Host != "" {
		return c.Request.Host
	}
	return "localhost"
}
