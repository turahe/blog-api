.PHONY: build test lint routes-check swagger run infra-up infra-down infra-up-messaging infra-down-messaging migrate-up docker-up docker-down docker-build docker-logs docker-seed docker-migrate

MODULE := github.com/turahe/blog-api

build:
	go build -o app .

test:
	go test -count=1 ./cmd/... ./internal/... ./docs/...

lint:
	go vet . ./cmd/... ./internal/... ./docs/...
	test -z "$$(gofmt -l main.go cmd internal docs)"

# Compile + smoke-test the Gin route registration.
routes-check:
	go test -count=1 ./internal/adapters/inbound/routes/...

# Regenerate OpenAPI from swag annotations (requires: go install github.com/swaggo/swag/cmd/swag@latest).
swagger:
	swag fmt -g main.go
	swag init -g main.go -o docs --parseDependency --parseInternal

run:
	go run . serve

# Full local stack: postgres, redis, rustfs, migrate, api (http://localhost:8080).
docker-up:
	docker compose up -d --build api

docker-down:
	docker compose down

docker-build:
	docker compose build api

docker-logs:
	docker compose logs -f api

docker-migrate:
	docker compose run --rm migrate

docker-seed:
	docker compose run --rm --no-deps api seed

# Infra only (API runs on the host via `make run`).
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
	go run . migrate up
