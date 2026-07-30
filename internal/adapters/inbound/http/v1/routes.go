// Package v1 owns the contract-derived route table for the versioned API surface.
package v1

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// Group is the route group an operation belongs to. Groups mirror the operationId
// namespaces in the OpenAPI contract; see docs/backend/api.md for the grouping rules.
type Group string

const (
	GroupHealth    Group = "health"
	GroupAuth      Group = "auth"
	GroupMe        Group = "me"
	GroupSelf      Group = "self"
	GroupPublic    Group = "public"
	GroupAdmin     Group = "admin"
	GroupAnalytics Group = "analytics"
)

// AuthMode is the credential requirement declared by the operation's security block.
type AuthMode string

const (
	AuthNone     AuthMode = "none"
	AuthOptional AuthMode = "optional"
	AuthRequired AuthMode = "required"
)

const routeContextKey = "contract_route"

type Route struct {
	Method      string
	Path        string
	OperationID string
	Group       Group
	Auth        AuthMode
}

// Middleware holds the chains applied to a route before its handler. A route runs
// ByAuth[route.Auth] and then ByGroup[route.Group], so a caller is authenticated
// before any group-level authorization runs.
type Middleware struct {
	ByAuth  map[AuthMode]gin.HandlersChain
	ByGroup map[Group]gin.HandlersChain
}

// RouteOf returns the contract route matched for the request.
func RouteOf(c *gin.Context) (Route, bool) {
	value, exists := c.Get(routeContextKey)
	route, ok := value.(Route)
	return route, exists && ok
}

// Register binds every contract route to the handler returned by handlerFor, behind
// its auth and group middleware chains. Returning a nil handler leaves the route to
// whoever registered it already. Register reports an error when any route's group or
// auth mode has no registered chain, so a namespace added to the contract cannot be
// served without a deliberate middleware decision.
func Register(router gin.IRouter, mw Middleware, handlerFor func(Route) gin.HandlerFunc) error {
	chains := make([]gin.HandlersChain, len(Routes))
	for i, route := range Routes {
		chain, err := mw.chainFor(route)
		if err != nil {
			return err
		}
		chains[i] = chain
	}
	for i, route := range Routes {
		handler := handlerFor(route)
		if handler == nil {
			continue
		}
		handlers := make(gin.HandlersChain, 0, len(chains[i])+1)
		handlers = append(handlers, chains[i]...)
		handlers = append(handlers, handler)
		router.Handle(route.Method, route.Path, handlers...)
	}
	return nil
}

func (m Middleware) chainFor(route Route) (gin.HandlersChain, error) {
	auth, ok := m.ByAuth[route.Auth]
	if !ok {
		return nil, fmt.Errorf(
			"route %s %s: no chain registered for auth mode %q",
			route.Method, route.Path, route.Auth,
		)
	}
	group, ok := m.ByGroup[route.Group]
	if !ok {
		return nil, fmt.Errorf(
			"route %s %s: no chain registered for group %q",
			route.Method, route.Path, route.Group,
		)
	}
	chain := make(gin.HandlersChain, 0, len(auth)+len(group)+1)
	chain = append(chain, routeContext(route))
	chain = append(chain, auth...)
	return append(chain, group...), nil
}

func routeContext(route Route) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(routeContextKey, route)
		c.Next()
	}
}
