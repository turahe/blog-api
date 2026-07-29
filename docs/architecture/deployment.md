# Deployment

## Environments

Recommended environments:

- local
- staging
- production

## Local Environment

Use Docker Compose as the standard local environment.

Local Compose should be able to run:

- API service
- PostgreSQL
- Redis
- local message broker or Watermill-compatible development transport if needed
- local S3-compatible object storage such as MinIO for media workflows
- optional worker service for async consumers

## Runtime Components

- API service
- PostgreSQL
- Redis
- Watermill transport backend or configured message broker
- object storage bucket for originals and optionally cached variants
- worker process for async jobs if consumers are split from the API

## Configuration

Use environment variables for:

- application address and mode
- database DSN
- Redis connection settings
- session secrets
- encryption keys
- OAuth provider credentials
- email provider credentials
- Watermill transport configuration
- media storage driver, bucket, region, endpoint, and credentials
- image transform cache TTL and size controls

## Release Process

- build application artifact
- validate API and event contracts locally
  - OpenAPI: `python3 -m pip install pyyaml openapi-spec-validator && python3 -c 'import yaml, openapi_spec_validator; openapi_spec_validator.validate_spec(yaml.safe_load(open("contracts/openapi.yaml")))'`
  - AsyncAPI: `npx @asyncapi/cli validate contracts/asyncapi.yaml`
- run automated tests
- apply database migrations with `app migrate up`
- deploy API (`app serve`)
- deploy background workers (`app worker`)
- deploy scheduled jobs if separated (`app scheduler`)
- validate runtime dependencies with `app doctor`
- verify health and readiness endpoints

## Migration Policy

- migrations must be explicit and reversible where practical
- run migrations before enabling new traffic-dependent features
- do not mix destructive schema changes with unverified code deploys

## Rollback Strategy

- support rolling back the app independently of non-breaking schema changes
- keep feature flags for high-risk launches when possible
- preserve audit trails during rollback

## Post-Deploy Checks

- health endpoints pass
- database connectivity works
- Redis connectivity works
- event publishing and consumers are healthy
- critical auth flows succeed in smoke tests
