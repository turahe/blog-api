package routes_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
)

const swaggerPath = "../../../../docs/swagger.json"

var (
	ginParam     = regexp.MustCompile(`:(\w+)`)
	swaggerParam = regexp.MustCompile(`\{\w+\}`)
)

// TestSwaggerMatchesImplementedRoutes checks that every implemented route is documented in
// docs/swagger.json and every documented operation is mounted. Routes still mounted as 501
// stubs are excluded: they have no handler to annotate yet.
func TestSwaggerMatchesImplementedRoutes(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	stubs := map[string]bool{}
	controllers := routes.Controllers{Stub: func(r routes.Route) gin.HandlerFunc {
		stubs[r.Method+" "+ginParam.ReplaceAllString(r.Path, "{$1}")] = true
		return routes.NotImplemented(r)
	}}
	fillHandlers(reflect.ValueOf(&controllers).Elem())

	router := gin.New()
	routes.Register(router, controllers, routes.AuthMiddleware{})

	var mounted []string

	for _, r := range router.Routes() {
		key := r.Method + " " + ginParam.ReplaceAllString(r.Path, "{$1}")
		if !stubs[key] {
			mounted = append(mounted, key)
		}
	}

	documented := swaggerOperations(t)

	var undocumented, unmounted []string

	for _, key := range mounted {
		if !slices.Contains(documented, key) {
			undocumented = append(undocumented, key)
		}
	}

	for _, key := range documented {
		if !slices.Contains(mounted, key) {
			unmounted = append(unmounted, key)
		}
	}

	slices.Sort(undocumented)
	slices.Sort(unmounted)
	require.Empty(t, undocumented, "implemented routes missing from docs/swagger.json (add swag annotations, run make swagger)")
	require.Empty(t, unmounted, "docs/swagger.json operations with no implemented route")
}

// fillHandlers sets every nil gin.HandlerFunc field, recursively, to a no-op handler, so
// only routes registered with an explicit nil reach the Stub hook.
func fillHandlers(v reflect.Value) {
	fillHandlersWith(v, func(*gin.Context) {})
}

func fillHandlersWith(v reflect.Value, handler gin.HandlerFunc) {
	handlerType := reflect.TypeFor[gin.HandlerFunc]()

	for _, field := range v.Fields() {
		switch {
		case field.Type() == handlerType && field.IsNil():
			field.Set(reflect.ValueOf(handler))
		case field.Kind() == reflect.Struct:
			fillHandlersWith(field, handler)
		}
	}
}

// swaggerOperation is the part of an operation the parity tests read.
type swaggerOperation struct {
	Security []map[string][]string `json:"security"`
}

func swaggerSpec(t *testing.T) map[string]swaggerOperation {
	t.Helper()

	raw, err := os.ReadFile(swaggerPath)
	require.NoError(t, err)

	var spec struct {
		Paths map[string]map[string]swaggerOperation `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &spec))

	ops := map[string]swaggerOperation{}

	for path, methods := range spec.Paths {
		for method, op := range methods {
			ops[strings.ToUpper(method)+" "+path] = op
		}
	}

	return ops
}

func swaggerOperations(t *testing.T) []string {
	t.Helper()

	var keys []string
	for key := range swaggerSpec(t) {
		keys = append(keys, key)
	}

	return keys
}

// TestSwaggerSecurityMatchesAuthMode checks that required-auth routes declare the Bearer
// scheme and anonymous routes declare none. Optional routes may do either.
func TestSwaggerSecurityMatchesAuthMode(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	var mode routes.AuthMode

	controllers := routes.Controllers{Stub: routes.NotImplemented}
	fillHandlersWith(reflect.ValueOf(&controllers).Elem(), func(c *gin.Context) {
		route, _ := routes.RouteOf(c)
		mode = route.Auth
	})

	router := gin.New()
	routes.Register(router, controllers, routes.AuthMiddleware{})

	var mismatched []string

	for key, op := range swaggerSpec(t) {
		method, path, _ := strings.Cut(key, " ")
		mode = ""

		router.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), method, swaggerParam.ReplaceAllString(path, "x"), nil))

		bearer := len(op.Security) > 0

		switch {
		case mode == "":
			mismatched = append(mismatched, key+": documented route was not reached")
		case mode == routes.AuthRequired && !bearer:
			mismatched = append(mismatched, key+": auth required but no @Security")
		case mode == routes.AuthNone && bearer:
			mismatched = append(mismatched, key+": anonymous but has @Security")
		}
	}

	slices.Sort(mismatched)
	require.Empty(t, mismatched)
}
