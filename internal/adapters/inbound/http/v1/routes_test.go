package v1

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func fullMiddleware() Middleware {
	return Middleware{
		ByAuth: map[AuthMode]gin.HandlersChain{
			AuthNone:     nil,
			AuthOptional: nil,
			AuthRequired: nil,
		},
		ByGroup: map[Group]gin.HandlersChain{
			GroupHealth:    nil,
			GroupAuth:      nil,
			GroupMe:        nil,
			GroupSelf:      nil,
			GroupPublic:    nil,
			GroupAdmin:     nil,
			GroupAnalytics: nil,
		},
	}
}

func TestEveryGeneratedRouteHasAKnownGroupAndAuthMode(t *testing.T) {
	mw := fullMiddleware()
	require.NotEmpty(t, Routes)

	for _, route := range Routes {
		require.Containsf(t, mw.ByGroup, route.Group,
			"%s %s has unknown group %q", route.Method, route.Path, route.Group)
		require.Containsf(t, mw.ByAuth, route.Auth,
			"%s %s has unknown auth mode %q", route.Method, route.Path, route.Auth)
	}
}

func TestRegisterRunsAuthChainBeforeGroupChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var called []string
	record := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			called = append(called, name)
			c.Next()
		}
	}

	mw := fullMiddleware()
	mw.ByAuth[AuthRequired] = gin.HandlersChain{record("auth")}
	mw.ByGroup[GroupMe] = gin.HandlersChain{record("group")}

	router := gin.New()
	err := Register(router, mw, func(route Route) gin.HandlerFunc {
		if route.OperationID != "me.get" {
			return nil
		}
		return func(c *gin.Context) {
			called = append(called, "handler")
			matched, ok := RouteOf(c)
			require.True(t, ok)
			require.Equal(t, "me.get", matched.OperationID)
			require.Equal(t, GroupMe, matched.Group)
			require.Equal(t, AuthRequired, matched.Auth)
			c.Status(nethttp.StatusNoContent)
		}
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/v1/me", nil))

	require.Equal(t, nethttp.StatusNoContent, recorder.Code)
	require.Equal(t, []string{"auth", "group", "handler"}, called)
}

func TestRegisterRejectsUnregisteredGroupOrAuthMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlerFor := func(Route) gin.HandlerFunc {
		return func(c *gin.Context) {}
	}

	t.Run("group", func(t *testing.T) {
		mw := fullMiddleware()
		delete(mw.ByGroup, GroupAdmin)
		err := Register(gin.New(), mw, handlerFor)
		require.ErrorContains(t, err, `no chain registered for group "admin"`)
	})

	t.Run("auth mode", func(t *testing.T) {
		mw := fullMiddleware()
		delete(mw.ByAuth, AuthOptional)
		err := Register(gin.New(), mw, handlerFor)
		require.ErrorContains(t, err, `no chain registered for auth mode "optional"`)
	})
}

func TestRegisterSkipsRoutesWithoutAHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(func(c *gin.Context) { c.Status(nethttp.StatusTeapot) })

	err := Register(router, fullMiddleware(), func(Route) gin.HandlerFunc { return nil })
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/v1/me", nil))
	require.Equal(t, nethttp.StatusTeapot, recorder.Code)
}
