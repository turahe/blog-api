package routes_test

import (
	"encoding/json"
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

var ginParam = regexp.MustCompile(`:(\w+)`)

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
	handlerType := reflect.TypeFor[gin.HandlerFunc]()

	for _, field := range v.Fields() {
		switch {
		case field.Type() == handlerType && field.IsNil():
			field.Set(reflect.ValueOf(gin.HandlerFunc(func(*gin.Context) {})))
		case field.Kind() == reflect.Struct:
			fillHandlers(field)
		}
	}
}

func swaggerOperations(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(swaggerPath)
	require.NoError(t, err)

	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &spec))

	var ops []string

	for path, methods := range spec.Paths {
		for method := range methods {
			ops = append(ops, strings.ToUpper(method)+" "+path)
		}
	}

	return ops
}
