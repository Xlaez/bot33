.PHONY: tidy test build run-watcher run-api seed

tidy:
	go mod tidy

test:
	go test ./...

build:
	go build -o bin/watcher ./cmd/watcher
	go build -o bin/api ./cmd/api

run-watcher:
	go run ./cmd/watcher

run-api:
	go run ./cmd/api

seed:
	python3 scripts/research_seed_fast.py
