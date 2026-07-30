package http

import "github.com/gin-gonic/gin"

type Envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Meta  *Meta      `json:"meta,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

type Meta struct {
	RequestID string `json:"request_id,omitempty"`
	Page      int    `json:"page,omitempty"`
	PerPage   int    `json:"per_page,omitempty"`
	Total     int64  `json:"total,omitempty"`
}

func successWithMeta(c *gin.Context, status int, data any, meta *Meta) {
	if meta == nil {
		meta = &Meta{RequestID: requestID(c)}
	} else if meta.RequestID == "" {
		meta.RequestID = requestID(c)
	}
	c.JSON(status, Envelope{
		OK:   true,
		Data: data,
		Meta: meta,
	})
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func success(c *gin.Context, status int, data any) {
	c.JSON(status, Envelope{
		OK:   true,
		Data: data,
		Meta: &Meta{RequestID: requestID(c)},
	})
}

func failure(c *gin.Context, status int, code, message string) {
	failureWithDetails(c, status, code, message, nil)
}

func failureWithDetails(c *gin.Context, status int, code, message string, details any) {
	c.AbortWithStatusJSON(status, Envelope{
		OK:    false,
		Meta:  &Meta{RequestID: requestID(c)},
		Error: &ErrorBody{Code: code, Message: message, Details: details},
	})
}
