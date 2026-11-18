.PHONY: run build test lint generate

run:
	go run ./cmd/stockportfolio

build:
	go build -o bin/stockportfolio ./cmd/stockportfolio

test:
	go test ./...

lint:
	golangci-lint run

generate:
	sqlc generate
