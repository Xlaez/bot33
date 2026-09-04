.PHONY: tidy test build run-watcher run-api seed web web-build

tidy:
	go mod tidy

test:
	go test ./...

build:
	go build -o bin/watcher ./cmd/watcher
	go build -o bin/api ./cmd/api
	go build -o bin/meme-watcher ./cmd/meme-watcher

web:
	cd web && npm run dev

web-build:
	cd web && npm run build

run-watcher:
	go run ./cmd/watcher

run-meme:
	go run ./cmd/meme-watcher

run-api:
	go run ./cmd/api

seed:
	python3 scripts/research_seed_fast.py
