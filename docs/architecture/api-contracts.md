# API Contracts

This project uses two machine-readable contracts in `contracts/`:

- `contracts/openapi.yaml` — synchronous REST API entry (OpenAPI 3.1, `$ref` split)
- `contracts/openapi.bundle.yaml` — committed bundled OpenAPI (served by Swagger UI)
- `contracts/openapi.bundle.deref.yaml` — fully dereferenced bundle (route parity checks)
- `contracts/asyncapi.yaml` — asynchronous events, streams, jobs (AsyncAPI 2.6)
- `contracts/swagger/index.html` — Swagger UI page embedded with the API

These files are the published contract standard for HTTP and events. The Gin route table in
`internal/adapters/inbound/routes/api.go` is the runtime source of truth for mounted
paths — update Go first, then keep OpenAPI aligned (`make routes-check`).

## Repository layout

```text
contracts/
├── embed.go                 # go:embed OpenAPI bundle + Swagger UI
├── openapi.yaml
├── openapi.bundle.yaml
├── openapi.bundle.deref.yaml
├── asyncapi.yaml
└── swagger/
    └── index.html
```

## In-process Swagger UI (Go)

When `APP_SWAGGER_ENABLED` is true (default for `APP_ENV=local`), `app serve` mounts:

- [http://localhost:8080/swagger](http://localhost:8080/swagger) — Swagger UI
- [http://localhost:8080/openapi.yaml](http://localhost:8080/openapi.yaml) — embedded `openapi.bundle.yaml`

Disable in production with `APP_SWAGGER_ENABLED=false`.

## Local validation

```bash
make routes-check   # routes.Register* smoke tests

python3 -m pip install --upgrade pyyaml openapi-spec-validator
python3 - <<'PY'
import yaml
from openapi_spec_validator import validate_spec
with open("contracts/openapi.bundle.yaml") as f:
    validate_spec(yaml.safe_load(f))
print("OpenAPI validation OK")
PY
```

### AsyncAPI

Validate `contracts/asyncapi.yaml` with any AsyncAPI 2.6–compatible tool when you change event contracts.

## Change workflow

1. Edit Go routes in `internal/adapters/inbound/routes/api.go` (+ handlers).
2. Align `paths/` / `components/` / `contracts/openapi.yaml`.
3. Refresh committed bundles (`openapi.bundle.yaml`, `openapi.bundle.deref.yaml`).
4. Run `make routes-check` and `make test`.
