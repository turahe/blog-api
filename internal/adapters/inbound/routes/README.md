# Routes

Laravel-style Gin registration for the HTTP API.

| File | Role |
| --- | --- |
| [meta.go](meta.go) | `Route` metadata, `Bind`, `RouteOf` |
| [router.go](router.go) | `NewRouter` — engine + global hooks (middleware injected) |
| [register.go](register.go) | `Register` entry + bind helpers |
| `health.go`, `auth.go`, `public.go`, `me.go`, `admin.go`, … | `Register*` domain mounts |
| [controllers.go](controllers.go) | handler bags + `NotImplemented` stub |

Wire services in `../http/router.go` (`http.NewRouter` → `routes.NewRouter`).
Published HTTP docs are swag-generated under [`docs/`](../../../../docs/) (`make swagger`).

```bash
go test ./internal/adapters/inbound/routes/...
```

`swagger_parity_test.go` fails when an implemented route has no swag annotation, or when
`docs/swagger.json` documents a route that is not mounted. Routes mounted with a nil handler
(501 stubs) are exempt. Use `{param1}` in `@Router` paths to match the Gin `:param1` names.
