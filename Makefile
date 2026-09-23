.PHONY: build test lint routes-check run infra-up infra-down infra-up-messaging infra-down-messaging migrate-up

MODULE := github.com/turahe/blog-api

build:
	go build -o app .

test:
	go test -count=1 ./cmd/... ./internal/... ./contracts/...

lint:
	go vet . ./cmd/... ./internal/... ./contracts/...
	test -z "$$(gofmt -l main.go cmd internal contracts)"

# Compile + smoke-test the Gin route registration (no YAML reads).
routes-check:
	go test -count=1 ./internal/adapters/inbound/routes/...

run:
	go run . serve

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
