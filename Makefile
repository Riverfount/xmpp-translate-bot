.PHONY: build test vet run

build:
	go build -o bin/bot ./cmd/bot

test:
	go test ./...

vet:
	go vet ./...

run:
	go run ./cmd/bot
