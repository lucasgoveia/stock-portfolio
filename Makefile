.PHONY: run dev build build-web test lint generate

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

generate:
	sqlc generate
