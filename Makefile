.PHONY: build test lint contracts routes run infra-up infra-down infra-up-messaging infra-down-messaging migrate-up

build:
	go build -o app ./cmd

test:
	go test -count=1 ./cmd/... ./internal/...

lint:
	go vet ./cmd/... ./internal/...
	test -z "$$(gofmt -l ./cmd ./internal)"

contracts:
	npm run contracts:validate

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
