# API Contracts

This project uses two machine-readable contracts in `contracts/`:

- `contracts/openapi.yaml` — synchronous REST API (OpenAPI 3.1)
- `contracts/asyncapi.yaml` — asynchronous events, streams, jobs (AsyncAPI 2.6)

These files are the canonical source of truth for all API surfaces. Any new HTTP endpoint, message, or event must be added to the relevant contract first.

## Repository layout

```text
contracts/
├── openapi.yaml
└── asyncapi.yaml
```

## Generating documentation

### OpenAPI (REST)

You can render the OpenAPI file using Swagger UI, Redoc, or equivalent.

Example with Redocly CLI:

```bash
npx @redocly/cli preview-docs contracts/openapi.yaml
```

Example with Swagger UI CLI:

```bash
npx swagger-ui-watcher contracts/openapi.yaml
```

### AsyncAPI (events and streams)

Render the AsyncAPI file with the official AsyncAPI generator:

```bash
npx @asyncapi/cli generate html contracts/asyncapi.yaml -o .tmp/asyncapi-html
```

## Local validation

### YAML + OpenAPI validation

```bash
# Install validators
python3 -m pip install --upgrade pyyaml openapi-spec-validator

# Validate
python3 - <<'PY'
import yaml
from openapi_spec_validator import validate_spec
with open("contracts/openapi.yaml") as f:
    spec = yaml.safe_load(f)
validate_spec(spec)
print("OpenAPI validation OK")
PY
```

### AsyncAPI structural validation

There is no lightweight Python validator for AsyncAPI available in this environment, so the CI/CD pipeline should use the official AsyncAPI CLI:

```bash
npx @asyncapi/cli validate contracts/asyncapi.yaml
```

A lightweight structural check for AsyncAPI 2.x in this project:

```bash
python3 - <<'PY'
import yaml
with open("contracts/asyncapi.yaml") as f:
    a = yaml.safe_load(f)
required = ["asyncapi", "info", "channels"]
missing = [k for k in required if k not in a]
assert not missing, f"missing required keys: {missing}"
assert str(a["asyncapi"]).startswith("2."), "AsyncAPI version must be 2.x"
print("AsyncAPI structure OK, channels:", len(a.get("channels", {})))
PY
```

## How to keep contracts in sync

- Before implementing a new endpoint:
  1. Update `contracts/openapi.yaml` with path, schemas, security requirements, and status codes
  2. Open a review for the contract changes first
- Before implementing a new event or stream:
  1. Update `contracts/asyncapi.yaml` with the channel, publish/subscribe operations, and message schema
  2. Add or update cross references in `docs/backend/events.md` and related feature docs
- When changing response shapes, error codes, or permissions:
  1. Update the contract file
  2. Update tests that exercise that surface
  3. Add a row to `CHANGELOG.md`

## CI checks

CI should validate contracts on every pull request:

```yaml
# sketch (pseudo-config)
steps:
  - name: Validate OpenAPI
    run: |
      pip install --quiet openapi-spec-validator pyyaml
      python3 - <<'PY'
      import yaml
      from openapi_spec_validator import validate_spec
      with open("contracts/openapi.yaml") as f:
          validate_spec(yaml.safe_load(f))
      PY
  - name: Validate AsyncAPI
    run: npx @asyncapi/cli validate contracts/asyncapi.yaml
```

## Contract usage patterns

### Backend

- HTTP routers and handlers should match paths defined in `openapi.yaml` exactly
- Outbox event publishers in Watermill should publish event names declared in `asyncapi.yaml`
- SSE endpoints for notifications and admin analytics streams correspond to the bindings declared in the AsyncAPI file

### Frontend / admin UI

- TypeScript types can be generated from the OpenAPI contract using `openapi-typescript` or equivalent
- SSE event handlers should follow the event shape in the AsyncAPI contract
- Mock servers for local frontend development can be generated from both contracts

### Testing

- Contract-based tests should verify that:
  - All declared HTTP paths return the documented status codes for authenticated and anonymous callers
  - All published messages match the AsyncAPI message schemas
  - Security rules (bearer auth, scopes, CSRF, permissions) match enforcement in the application
