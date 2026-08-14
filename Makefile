.PHONY: build test vet run docker-build

build:
	go build -o bin/bot ./cmd/bot

test:
	go test ./...

vet:
	go vet ./...

run:
	go run ./cmd/bot

# Funciona com `docker` ou `podman` -- troque a engine na chamada:
#   make docker-build ENGINE=podman
ENGINE ?= docker
docker-build:
	$(ENGINE) build -f deploy/Dockerfile -t xmpp-translate-bot:local .
