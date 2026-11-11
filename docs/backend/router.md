
# Router Documentation

This document provides an overview of the router design and route registration approach for the backend project using the Gin web framework.

## Router Structure

Routes are grouped logically by function area inside `internal/adapters/inbound/routes/` using a `*_router.go` file per area. All registration functions are exported so callers can embed just the subsets they need, or compose the full application via the main `Register` entrypoint.

The eight area registration functions are:

| File                       | Exported function                       | Purpose                                                        |
|----------------------------|-----------------------------------------|----------------------------------------------------------------|
| `common_router.go`         | `RegisterCommonRouter`                  | Liveness/readiness/version probes (no version prefix).        |
| `public_router.go`         | `RegisterPublicContentRouter`           | Public posts/tags/categories/users/settings/search.           |
| `user_auth_router.go`      | `RegisterUserAuthRouter`                | User-facing `/auth` routes (login/register/2FA/OAuth/logout). |
| `me_router.go`             | `RegisterMeRouter`                      | Authenticated `/me` self-service (profile/activity/2FA/…).    |
| `comments_router.go`       | `RegisterCommentsRouter`                | Public / self / admin comment routes.                         |
| `admin_auth_router.go`     | `RegisterAdminAuthRouter`               | Admin login + the full `/admin/*` protected tree.             |
| `analytics_router.go`      | `RegisterAnalyticsRouter`               | Public track + admin reports + exports.                       |
| `newsletter_router.go`     | `RegisterNewsletterRouter`              | Public subscribe/confirm/unsubscribe + admin send/management. |

### Main Route Registration

The main entrypoint is `routes.Register`. It composes the area `Register*Router` functions above — mounting `RegisterCommonRouter` at the root and the remaining areas under `/api/v1`:

```go
package main

import (
	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
)

func main() {
	engine := gin.New()
	var controllers routes.Controllers
	var auth routes.AuthMiddleware
	routes.Register(engine, controllers, auth)
}
```

`routes.Register` uses `gin.IRouter`, so callers can pass either a `*gin.Engine` (as shown) or their own sub-group. Internally it:

1. Fills any nil handler with the controllers stub (`routes.NotImplemented`).
2. Calls `RegisterCommonRouter(router.Group(""), c)` so `/health/*` has no `/api/v1` prefix.
3. Builds `v1 := router.Group("/api/v1")` and composes the remaining seven area routers onto it.

The existing `routes.NewRouter(...)` (which wires global middleware, Swagger, `/api/v1/health` alias, `NoRoute`, `NoMethod`, and trusted proxies) delegates to this same `Register` function so the two entrypoints always share a single route list.

### Example: Common Routes

The common routes include the general-purpose liveness, readiness, and version probes used for Kubernetes health checks and build-version reporting:

```go
func RegisterCommonRouter(router *gin.RouterGroup, c routes.Controllers) {
	routes.GET(router, "/health/live",    "health.live",    routes.GroupHealth, routes.AuthNone, c, c.Health.Live)
	routes.GET(router, "/health/ready",   "health.ready",   routes.GroupHealth, routes.AuthNone, c, c.Health.Ready)
	routes.GET(router, "/health/version", "health.version", routes.GroupHealth, routes.AuthNone, c, c.Health.Version)
}
```

- `routes.GET` (and `POST`/`PUT`/`PATCH`/`DELETE`) are helpers that bind an `OperationID`, route group enum, and auth-mode enum to each endpoint (see `meta.go`) so that tests and access logs can read those values off a matched request.
- Handlers are provided by the `routes.Controllers` struct so every registration entrypoint accepts the same controller set.

## Adding More Routes

1. Decide which area owns the route (create a new `foo_router.go` file for a genuinely new area) and add the endpoint inside the matching `RegisterFooRouter(router, auth, c)` function using one of the `routes.GET` / `routes.POST` / … helpers that already bind auth-mode + OperationID.
2. If a new area file was added, export a `RegisterFooRouter(router, auth, c)` and call it from `routes.Register` in `register.go` so both entrypoints (embedded callers and `NewRouter`) pick it up.
3. Existing area functions already in use: `RegisterCommonRouter`, `RegisterPublicContentRouter`, `RegisterUserAuthRouter`, `RegisterMeRouter`, `RegisterCommentsRouter`, `RegisterAdminAuthRouter`, `RegisterAnalyticsRouter`, `RegisterNewsletterRouter`.

## Example Directory Layout

```
internal/adapters/inbound/
├── http/
│   ├── router.go          # NewRouter(...) — wires middleware + delegated to routes.NewRouter
│   ├── handlers/          # HTTP handler implementations
│   └── middleware/
└── routes/
    ├── register.go                # AuthMiddleware struct + exported Register(…) composer
    ├── router.go                  # NewRouter(…) — global m/w, Swagger, /api/v1/health alias, NoRoute/NoMethod
    ├── controllers.go             # Controllers struct — per-operation handler fields
    ├── meta.go                    # RouteSpec, OperationID / Group / AuthMode enums, helpers
    ├── common_router.go           # RegisterCommonRouter  — health/live/ready/version
    ├── public_router.go           # RegisterPublicContentRouter — posts/tags/categories/users/settings
    ├── user_auth_router.go        # RegisterUserAuthRouter — /auth
    ├── me_router.go               # RegisterMeRouter       — /me
    ├── comments_router.go         # RegisterCommentsRouter — public/self/admin comment routes
    ├── admin_auth_router.go       # RegisterAdminAuthRouter — /admin (auth.login + protected tree)
    ├── analytics_router.go        # RegisterAnalyticsRouter
    └── newsletter_router.go       # RegisterNewsletterRouter
```

Route registration typically occurs in `main` during application startup: build the handler controllers (see `internal/adapters/inbound/http/router.go:NewRouter`), construct the `routes.AuthMiddleware` optional/required chains, then call either `routes.NewRouter(routes.Dependencies{…})` (the all-in-one that wires Swagger/global m/w too) or `routes.Register(engine, c, auth)` if the caller already owns the engine.

## Best Practices

- Keep route definitions modular by function area — one exported `Register*Router` per `*_router.go` file.
- Use the `routes.GET` / `routes.POST` / … helpers that declare an `OperationID`, `Group` enum, and `AuthMode` enum at the single call site — tests enforce single-source-of-truth for those values.
- Prefer `routes.Register` (or an area-specific `Register*Router`) for embedded/test usage; `routes.NewRouter` exists for full-application startup.
- Add tests for auth-mode, handler execution order, and new exported `Register*` functions inside `register_test.go`.
- When adding an endpoint, update the Swagger spec through the existing single-source annotation pipeline used by the CI swagger workflow.

## Summary

The router keeps two entrypoints aligned: a full-app constructor (`routes.NewRouter`) and a composable exported surface (`routes.Register`, the eight area `Register*Router` functions). Every handler, URL, and OperationID is registered exactly once inside an area `Register*Router`, and those same functions are reused whether the caller embeds a single area or wires up the whole application.
