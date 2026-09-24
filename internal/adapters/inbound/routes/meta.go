// Package routes owns route metadata (group, auth mode, operationId) for logging and stubs.
package routes

import "github.com/gin-gonic/gin"

// Group is the route group an operation belongs to. See docs/backend/api.md.
type Group string

const (
	GroupHealth      Group = "health"
	GroupAuth        Group = "auth"
	GroupSelfService Group = "self-service"
	GroupPublic      Group = "public"
	GroupAdmin       Group = "admin"
	GroupAnalytics   Group = "analytics"
)

// AuthMode is the credential requirement for the route.
type AuthMode string

const (
	AuthNone     AuthMode = "none"
	AuthOptional AuthMode = "optional"
	AuthRequired AuthMode = "required"
)

const routeContextKey = "contract_route"

// Route is metadata attached to a mounted Gin handler (access logs, 501 stubs).
type Route struct {
	Method      string
	Path        string
	OperationID string
	Group       Group
	Auth        AuthMode
}

// RouteOf returns the matched route metadata for the request.
func RouteOf(c *gin.Context) (Route, bool) {
	value, exists := c.Get(routeContextKey)
	route, ok := value.(Route)
	return route, exists && ok
}

// withMeta stores route metadata on the Gin context before the handler runs.
func withMeta(route Route) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(routeContextKey, route)
		c.Next()
	}
}

// bindMeta mounts relative path with route metadata prepended to the handler chain.
// route.Path should be the full path for logs/stubs; path is what Gin registers.
func bindMeta(r gin.IRoutes, path string, route Route, handlers ...gin.HandlerFunc) {
	chain := make(gin.HandlersChain, 0, 1+len(handlers))
	chain = append(chain, withMeta(route))
	chain = append(chain, handlers...)
	r.Handle(route.Method, path, chain...)
}

// orStub returns handler, or stub(route) when handler is nil.
func orStub(stub func(Route) gin.HandlerFunc, route Route, handler gin.HandlerFunc) gin.HandlerFunc {
	if handler != nil {
		return handler
	}
	return stub(route)
}
