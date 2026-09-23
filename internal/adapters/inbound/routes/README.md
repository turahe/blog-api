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
OpenAPI under `contracts/` stays the published client contract.

```bash
go test ./internal/adapters/inbound/routes/...
```
