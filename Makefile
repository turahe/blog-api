.PHONY: test test-race test-integration test-brokers asyncapi-validate coverage lint routes-check swagger dev-keys infra-up infra-down infra-up-messaging infra-down-messaging docker-up docker-down docker-build docker-logs docker-seed docker-migrate trivy

MODULE := github.com/turahe/blog-api
TEST_PKGS := ./cmd/... ./internal/... ./docs/...

# Every target runs in Docker; only Docker (with Compose) is required on the host.
# Go module and build caches live in named volumes so repeat runs stay fast.
GO_IMAGE ?= golang:1.27.1-alpine
# -race needs cgo and a C toolchain; the Debian image ships gcc, alpine does not.
GO_RACE_IMAGE ?= golang:1.27.1
GOLANGCI_LINT_IMAGE ?= golangci/golangci-lint:v2.14.0
SWAG_VERSION ?= v1.16.6
ASYNCAPI_CLI_VERSION ?= 6.2.0
# Containers run as root; files they write are chowned back to the host user.
HOST_UID_GID := $(shell id -u):$(shell id -g)
# Keep root-run git (VCS stamping, --new-from-rev) from rewriting .git/index as root.
DOCKER_GIT_ENV = -e GIT_OPTIONAL_LOCKS=0 -e GOFLAGS=-buildvcs=false
DOCKER_GO = docker run --rm $(DOCKER_GIT_ENV) \
	-v "$(CURDIR)":/app -w /app \
	-v blog-api-go-mod-cache:/go/pkg/mod \
	-v blog-api-go-build-cache:/root/.cache/go-build

test:
	$(DOCKER_GO) $(GO_IMAGE) go test -count=1 $(TEST_PKGS)

test-race:
	$(DOCKER_GO) -e CGO_ENABLED=1 $(GO_RACE_IMAGE) go test -race -shuffle=on -count=1 $(TEST_PKGS)

# Writes coverage.out and coverage.html (open in a browser), and prints total coverage.
coverage:
	$(DOCKER_GO) -e CGO_ENABLED=1 $(GO_RACE_IMAGE) sh -c '\
		go test -race -shuffle=on -count=1 -covermode=atomic -coverprofile=coverage.out $(TEST_PKGS) && \
		go tool cover -html=coverage.out -o coverage.html && \
		go tool cover -func=coverage.out | tail -n 1; \
		status=$$?; chown $(HOST_UID_GID) coverage.out coverage.html 2>/dev/null; exit $$status'

# Repository tests against the Compose Postgres (run `make infra-up` first), in a
# throwaway database that is recreated on every run.
TEST_DB ?= blog_api_test
PG_ENV = docker compose exec -T postgres printenv

test-integration:
	docker compose exec -T postgres sh -c 'dropdb -U "$$POSTGRES_USER" --if-exists $(TEST_DB) && createdb -U "$$POSTGRES_USER" $(TEST_DB)'
	$(DOCKER_GO) --network container:$$(docker compose ps -q postgres) \
		-e TEST_DATABASE_URL="host=127.0.0.1 port=5432 sslmode=disable dbname=$(TEST_DB) user=$$($(PG_ENV) POSTGRES_USER) password=$$($(PG_ENV) POSTGRES_PASSWORD)" \
		$(GO_IMAGE) go test -count=1 ./internal/adapters/outbound/persistence/...

# Broker round-trip and dead-letter tests against the Compose Kafka and RabbitMQ (run
# `make infra-up-messaging` first). Each run uses its own topics and deletes them afterwards.
RABBIT_ENV = docker compose exec -T rabbitmq printenv

test-brokers:
	$(DOCKER_GO) --network host \
		-e TEST_KAFKA_BROKERS=127.0.0.1:9092 \
		-e TEST_RABBITMQ_URL="amqp://$$($(RABBIT_ENV) RABBITMQ_DEFAULT_USER):$$($(RABBIT_ENV) RABBITMQ_DEFAULT_PASS)@127.0.0.1:5672/" \
		$(GO_IMAGE) go test -count=1 -run TestBroker ./internal/platform/messaging/...

# Validate the event contract; warnings are printed, schema errors fail.
asyncapi-validate:
	docker run --rm -v "$(CURDIR)":/app:ro -w /app node:22-alpine \
		npx --yes @asyncapi/cli@$(ASYNCAPI_CLI_VERSION) validate docs/architecture/asyncapi.yaml --fail-severity error

# Compile + smoke-test the Gin route registration.
routes-check:
	$(DOCKER_GO) $(GO_IMAGE) go test -count=1 ./internal/adapters/inbound/routes/...

# golangci-lint, configured by .golangci.yml (includes govet, gofumpt, goimports).
# Extra flags via LINT_ARGS, e.g. `make lint LINT_ARGS=--new-from-rev=HEAD` or `LINT_ARGS=--fix`.
LINT_ARGS ?=

lint:
	docker run --rm $(DOCKER_GIT_ENV) \
		-v "$(CURDIR)":/app -w /app \
		-v blog-api-golangci-cache:/root/.cache \
		-v blog-api-go-mod-cache:/go/pkg/mod \
		$(GOLANGCI_LINT_IMAGE) golangci-lint run $(LINT_ARGS) ./...

# Regenerate OpenAPI (docs/) from swag annotations.
swagger:
	$(DOCKER_GO) $(GO_IMAGE) sh -c '\
		go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) fmt -g main.go && \
		go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init -g main.go -o docs --parseDependency --parseInternal && \
		chown -R $(HOST_UID_GID) docs'

# Dev-only ES256 (P-256) key pair for configs/dev. The container runs as distroless nonroot (uid 65532),
# so the bind-mounted keys must be world-readable; never reuse these keys outside local dev.
dev-keys:
	@mkdir -p configs/dev
	@docker run --rm -v "$(CURDIR)/configs/dev":/keys -w /keys alpine:3.20 sh -c '\
		if [ ! -f jwt-es256-private.pem ] || [ ! -f jwt-es256-public.pem ]; then \
			apk add --no-cache -q openssl && \
			{ [ -f jwt-es256-private.pem ] || openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out jwt-es256-private.pem; } && \
			openssl pkey -in jwt-es256-private.pem -pubout -out jwt-es256-public.pem; \
		fi && chmod 644 jwt-es256-private.pem jwt-es256-public.pem && \
		chown $(HOST_UID_GID) jwt-es256-private.pem jwt-es256-public.pem'

# Full local stack: postgres, redis, rustfs, migrate, api (http://localhost:8080).
docker-up: dev-keys
	docker compose up -d --build api

docker-down:
	docker compose down

docker-build:
	docker compose build api

trivy:
	docker build -t blog-api:local .
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
		aquasec/trivy:0.36.0 image \
		--severity HIGH,CRITICAL \
		--exit-code 1 \
		blog-api:local

docker-logs:
	docker compose logs -f api

docker-migrate: dev-keys
	docker compose run --rm migrate

docker-seed: dev-keys
	docker compose run --rm --no-deps api seed

# Infra only: postgres, redis, rustfs (for running the API from an IDE or debugger).
infra-up:
	docker compose up -d postgres redis rustfs mailpit

infra-down:
	docker compose down

infra-up-messaging:
	docker compose --profile messaging up -d kafka rabbitmq

infra-down-messaging:
	docker compose stop kafka rabbitmq
	docker compose rm -f kafka rabbitmq
