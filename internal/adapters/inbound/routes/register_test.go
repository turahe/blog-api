package routes_test

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
)

func TestRegisterRunsAuthBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var called []string
	record := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			called = append(called, name)
			c.Next()
		}
	}

	router := gin.New()
	routes.Register(router, routes.Controllers{
		Stub: routes.NotImplemented,
		Users: routes.Users{
			MeGet: func(c *gin.Context) {
				called = append(called, "handler")
				matched, ok := routes.RouteOf(c)
				require.True(t, ok)
				require.Equal(t, "me.get", matched.OperationID)
				require.Equal(t, routes.GroupSelfService, matched.Group)
				require.Equal(t, routes.AuthRequired, matched.Auth)
				c.Status(nethttp.StatusNoContent)
			},
		},
	}, routes.AuthMiddleware{
		Required: gin.HandlersChain{record("auth")},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/v1/me", nil))

	require.Equal(t, nethttp.StatusNoContent, recorder.Code)
	require.Equal(t, []string{"auth", "handler"}, called)
}

func TestRegisterUsesStubWhenHandlerNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes.Register(router, routes.Controllers{Stub: routes.NotImplemented}, routes.AuthMiddleware{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/oauth/google/callback", nil))
	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)
}
