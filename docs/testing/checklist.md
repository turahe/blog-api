# Testing Checklist

## Before PR

- [ ] `go test -count=1 ./...` passes
- [ ] `go vet ./...` and `gofmt` clean (`make lint`)
- [ ] New behavior covered by unit or HTTP test
- [ ] Negative paths assert envelope `error.code` (not only status)
- [ ] If routes or swagger annotations changed: `make swagger` + commit `docs/` + `make routes-check` green
- [ ] No real secrets in fixtures; use [.env.example](../../.env.example) shapes only

## Auth / RBAC changes

- [ ] 401 without credentials where `AuthRequired`
- [ ] 403 without permission
- [ ] 404 for privacy-hidden public profiles (not 403) when required by policy
- [ ] CSRF / step-up cases where documented

## Media / uploads

- [ ] Invalid MIME / oversize / malware → no FK / no stored object
- [ ] Happy path creates media_asset + expected variants

## Do not merge if

- tests were deleted to “make CI green”
- only happy-path coverage for a security-sensitive endpoint
- `internal/adapters/inbound/routes/api.go` and OpenAPI left out of sync (`make routes-check` fails)
