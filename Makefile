.PHONY: run dev build build-web test lint generate openapi migration migrate-check

run:
	go run ./cmd/stockportfolio

# Go server (:8080) and Vite dev server (:5173, proxies /api to Go) together.
dev:
	@trap 'kill 0' INT TERM EXIT; \
	go run ./cmd/stockportfolio & \
	pnpm --dir web dev & \
	wait

build-web:
	pnpm --dir web install --frozen-lockfile
	pnpm --dir web build

# Builds the web app first; Go embeds web/dist into the single binary.
build: build-web
	go build -o bin/stockportfolio ./cmd/stockportfolio

test:
	go test ./...

lint:
	golangci-lint run
	pnpm --dir web lint

generate: openapi
	sqlc generate

# OpenAPI document from the Go handler types, then the TS client types from it.
openapi:
	go run ./cmd/openapi -o api/openapi.json
	pnpm --dir web gen:api

# Schema changes: edit internal/store/schema, then `make migration name=<desc>`.
migration:
	atlas migrate diff $(name) --env local
	atlas migrate lint --env local --latest 1 || true
	sqlc generate

migrate-check:
	atlas migrate validate --env local
	atlas migrate diff check --env local | grep -q "synced with the desired state"
