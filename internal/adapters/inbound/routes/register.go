// Package routes mounts the versioned HTTP API with Laravel-style Register* functions.
package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware is applied to RouterGroups before domain Register* calls.
type AuthMiddleware struct {
	Optional gin.HandlersChain
	Required gin.HandlersChain
}

// Register mounts health probes and /api/v1
func Register(router gin.IRouter, c Controllers, auth AuthMiddleware) {
	if c.Stub == nil {
		c.Stub = NotImplemented
	}

	RegisterHealth(router, c)

	v1 := router.Group("/api/v1")
	RegisterPublic(v1, c)
	RegisterAuth(v1, auth, c)
	RegisterMe(v1, auth, c)
	RegisterAdmin(v1, auth, c)
	RegisterAnalytics(v1, c)
	RegisterContractStubs(v1, auth, c)
}

func bind(r gin.IRoutes, method, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	meta := Route{
		Method:      method,
		Path:        fullPath(r, path),
		OperationID: opID,
		Group:       group,
		Auth:        mode,
	}
	Bind(r, method, path, meta, OrStub(c.Stub, meta, h))
}

func fullPath(r gin.IRoutes, path string) string {
	if g, ok := r.(*gin.RouterGroup); ok {
		base := g.BasePath()
		if base == "/" {
			return path
		}
		if path == "" || path == "/" {
			return base
		}
		return base + path
	}
	return path
}

func get(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	bind(r, http.MethodGet, path, opID, group, mode, c, h)
}

func post(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	bind(r, http.MethodPost, path, opID, group, mode, c, h)
}

func put(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	bind(r, http.MethodPut, path, opID, group, mode, c, h)
}

func patch(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	bind(r, http.MethodPatch, path, opID, group, mode, c, h)
}

func del(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	bind(r, http.MethodDelete, path, opID, group, mode, c, h)
}
