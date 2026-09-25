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

// Register mounts health probes and /api/v1 routes.
func Register(router gin.IRouter, c Controllers, auth AuthMiddleware) {
	if c.Stub == nil {
		c.Stub = NotImplemented
	}

	registerHealth(router, c)

	v1 := router.Group("/api/v1")
	registerPublic(v1, c)
	registerAuth(v1, auth, c)
	registerMe(v1, auth, c)
	registerComments(v1, auth, c)
	registerAdmin(v1, auth, c)
	registerAnalytics(v1, auth, c)
	registerNewsletter(v1, auth, c)
	registerContractStubs(v1, auth, c)
}

type routeSpec struct {
	method string
	path   string
	opID   string
	group  Group
	mode   AuthMode
	h      gin.HandlerFunc
}

func mount(r gin.IRoutes, c Controllers, spec routeSpec) {
	meta := Route{
		Method:      spec.method,
		Path:        fullPath(r, spec.path),
		OperationID: spec.opID,
		Group:       spec.group,
		Auth:        spec.mode,
	}

	handler := orStub(c.Stub, meta, spec.h)
	if c.Guard == nil {
		bindMeta(r, spec.path, meta, handler)
		return
	}

	bindMeta(r, spec.path, meta, c.Guard, handler)
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
	mount(r, c, routeSpec{method: http.MethodGet, path: path, opID: opID, group: group, mode: mode, h: h})
}

func post(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	mount(r, c, routeSpec{method: http.MethodPost, path: path, opID: opID, group: group, mode: mode, h: h})
}

func put(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	mount(r, c, routeSpec{method: http.MethodPut, path: path, opID: opID, group: group, mode: mode, h: h})
}

func patch(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	mount(r, c, routeSpec{method: http.MethodPatch, path: path, opID: opID, group: group, mode: mode, h: h})
}

func del(r gin.IRoutes, path, opID string, group Group, mode AuthMode, c Controllers, h gin.HandlerFunc) {
	mount(r, c, routeSpec{method: http.MethodDelete, path: path, opID: opID, group: group, mode: mode, h: h})
}
