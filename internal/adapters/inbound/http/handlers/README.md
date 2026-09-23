# Handlers

Laravel-style HTTP handlers for the inbound adapter.

| File | Domain |
| --- | --- |
| [controllers.go](controllers.go) | Builds `routes.Controllers` from services |
| [health.go](health.go) | Live / ready / version |
| [auth_posts.go](auth_posts.go) | Auth + public posts |
| [posts.go](posts.go) | Admin posts |
| [users.go](users.go) | Me + admin users + RBAC gates |
| [categories.go](categories.go) | Categories |
| [tags.go](tags.go) | Tags |
| [media.go](media.go) | Media |

Request DTOs live in [`../requests/`](../requests/). Resource JSON lives in [`../responses/`](../responses/).
`../router.go` wires middleware and calls `routes.NewRouter`.
