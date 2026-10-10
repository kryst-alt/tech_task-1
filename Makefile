# testcontainers talks to Podman through its Docker-compatible socket.
DOCKER_HOST ?= unix://$(XDG_RUNTIME_DIR)/podman/podman.sock
export DOCKER_HOST
export TESTCONTAINERS_RYUK_DISABLED ?= true

.PHONY: run test generate db-up db-down

run:
	set -a && . ./.env && set +a && go run ./cmd/server

test:
	go test ./...

generate:
	go tool sqlc generate

db-up:
	podman compose up -d db

db-down:
	podman compose down
