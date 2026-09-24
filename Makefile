.PHONY: test lint routes-check swagger dev-keys infra-up infra-down infra-up-messaging infra-down-messaging docker-up docker-down docker-build docker-logs docker-seed docker-migrate

MODULE := github.com/turahe/blog-api

# Every target runs in Docker; only Docker (with Compose) is required on the host.
# Go module and build caches live in named volumes so repeat runs stay fast.
GO_IMAGE ?= golang:1.26.5-alpine
GOLANGCI_LINT_IMAGE ?= golangci/golangci-lint:v2.14.0
SWAG_VERSION ?= v1.16.6
# Containers run as root; files they write are chowned back to the host user.
HOST_UID_GID := $(shell id -u):$(shell id -g)
# Keep root-run git (VCS stamping, --new-from-rev) from rewriting .git/index as root.
DOCKER_GIT_ENV = -e GIT_OPTIONAL_LOCKS=0 -e GOFLAGS=-buildvcs=false
DOCKER_GO = docker run --rm $(DOCKER_GIT_ENV) \
	-v "$(CURDIR)":/app -w /app \
	-v blog-api-go-mod-cache:/go/pkg/mod \
	-v blog-api-go-build-cache:/root/.cache/go-build

test:
	$(DOCKER_GO) $(GO_IMAGE) go test -count=1 ./cmd/... ./internal/... ./docs/...

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

# Dev-only RS256 key pair for configs/dev. The container runs as distroless nonroot (uid 65532),
# so the bind-mounted keys must be world-readable; never reuse these keys outside local dev.
dev-keys:
	@mkdir -p configs/dev
	@docker run --rm -v "$(CURDIR)/configs/dev":/keys -w /keys alpine:3.20 sh -c '\
		if [ ! -f jwt-rsa-private.pem ] || [ ! -f jwt-rsa-public.pem ]; then \
			apk add --no-cache -q openssl && \
			{ [ -f jwt-rsa-private.pem ] || openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out jwt-rsa-private.pem; } && \
			openssl pkey -in jwt-rsa-private.pem -pubout -out jwt-rsa-public.pem; \
		fi && chmod 644 jwt-rsa-private.pem jwt-rsa-public.pem && \
		chown $(HOST_UID_GID) jwt-rsa-private.pem jwt-rsa-public.pem'

# Full local stack: postgres, redis, rustfs, migrate, api (http://localhost:8080).
docker-up: dev-keys
	docker compose up -d --build api

docker-down:
	docker compose down

docker-build:
	docker compose build api

docker-logs:
	docker compose logs -f api

docker-migrate: dev-keys
	docker compose run --rm migrate

docker-seed: dev-keys
	docker compose run --rm --no-deps api seed

# Infra only: postgres, redis, rustfs (for running the API from an IDE or debugger).
infra-up:
	docker compose up -d postgres redis rustfs

infra-down:
	docker compose down

infra-up-messaging:
	docker compose --profile messaging up -d kafka rabbitmq

infra-down-messaging:
	docker compose stop kafka rabbitmq
	docker compose rm -f kafka rabbitmq
