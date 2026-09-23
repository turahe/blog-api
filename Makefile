.PHONY: build test lint contracts contracts-docs routes run infra-up infra-down infra-up-messaging infra-down-messaging migrate-up

REDOCLY_IMAGE ?= redocly/cli:2.54.2

build:
	go build -o app ./cmd

test:
	go test -count=1 ./cmd/... ./internal/...

lint:
	go vet ./cmd/... ./internal/...
	test -z "$$(gofmt -l ./cmd ./internal)"

# Lint + bundle OpenAPI with the official Redocly Docker image (no local Node required).
contracts:
	docker run --rm -v "$(CURDIR):/spec" -w /spec $(REDOCLY_IMAGE) lint contracts/openapi.yaml
	docker run --rm -v "$(CURDIR):/spec" -w /spec $(REDOCLY_IMAGE) bundle contracts/openapi.yaml -o contracts/openapi.bundle.yaml
	docker run --rm -v "$(CURDIR):/spec" -w /spec $(REDOCLY_IMAGE) bundle contracts/openapi.yaml --dereferenced -o contracts/openapi.bundle.deref.yaml

contracts-docs:
	mkdir -p dist
	docker run --rm -v "$(CURDIR):/spec" -w /spec $(REDOCLY_IMAGE) build-docs contracts/openapi.yaml -o dist/openapi.html

# Optional: reproducible image build that exports bundles via BuildKit --output.
contracts-image:
	DOCKER_BUILDKIT=1 docker build -f contracts/Dockerfile --ignorefile contracts/.dockerignore --target bundle -o contracts/ .

routes:
	python scripts/contracts/generate_go_routes.py
	gofmt -w internal/adapters/inbound/http/v1/routes_gen.go

run:
	go run ./cmd serve

infra-up:
	docker compose up -d postgres redis rustfs

infra-down:
	docker compose down

infra-up-messaging:
	docker compose --profile messaging up -d kafka rabbitmq

infra-down-messaging:
	docker compose stop kafka rabbitmq
	docker compose rm -f kafka rabbitmq

migrate-up:
	go run ./cmd migrate up
