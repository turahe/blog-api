# API Contracts

## HTTP (Swagger / OpenAPI 2 via swag)

The published HTTP contract is generated from handler annotations by [swag](https://github.com/swaggo/swag):

| Artifact | Role |
| --- | --- |
| Handler `// @…` comments | Source of truth for operations |
| [docs/docs.go](../docs.go), [docs/swagger.json](../swagger.json), [docs/swagger.yaml](../swagger.yaml) | Generated OpenAPI (committed) |
| `/swagger/*` | Swagger UI (when `APP_SWAGGER_ENABLED=true`, default on `APP_ENV=local`) |

Regenerate after annotation changes:

```bash
make swagger   # swag fmt + swag init -g main.go -o docs --parseDependency --parseInternal
```

CI ([.github/workflows/swagger.yml](../../.github/workflows/swagger.yml)) regenerates and fails if `docs/` is stale.

Envelope shape and pagination: [data-wrapping-and-pagination.md](../backend/data-wrapping-and-pagination.md).
Packed `code`: [response-codes.md](../backend/response-codes.md).
Route groups / auth modes: [api.md](../backend/api.md).

## Async (AsyncAPI)

Event / stream / job contracts live in [asyncapi.yaml](./asyncapi.yaml) (AsyncAPI 2.6).
Validate with any AsyncAPI 2.6–compatible tool when you change event contracts.

## Workflow

1. Implement or change Gin routes in `internal/adapters/inbound/routes/`.
2. Annotate the corresponding handler factories (`// @Summary`, `@Router`, `@Security`, …).
3. Run `make swagger` and commit `docs/`.
4. Run `make routes-check` / `make test`.
