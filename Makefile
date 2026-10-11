# Uses Docker when installed, Podman otherwise. Override with: make up COMPOSE="podman compose"
HAVE_DOCKER := $(shell command -v docker 2>/dev/null)
COMPOSE ?= $(if $(HAVE_DOCKER),docker compose,podman compose)

# Without Docker, testcontainers reaches Podman through its Docker-compatible socket.
PODMAN_SOCK := $(XDG_RUNTIME_DIR)/podman/podman.sock
ifeq ($(HAVE_DOCKER),)
ifneq ($(wildcard $(PODMAN_SOCK)),)
export DOCKER_HOST ?= unix://$(PODMAN_SOCK)
export TESTCONTAINERS_RYUK_DISABLED ?= true
endif
endif

LOAD_ENV := set -a && . ./.env && set +a

.PHONY: help setup up down logs ps db-up db-down run-api run-sync test generate

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

setup: ## Create .env with generated secrets and import the Google client JSON
	@scripts/setup.sh

up: ## Build and start db, lead-api and sheets-sync in containers
	$(COMPOSE) up -d --build

down: ## Stop all containers (data is kept)
	$(COMPOSE) down

logs: ## Follow logs of both services
	$(COMPOSE) logs -f lead-api sheets-sync

ps: ## Show container status
	$(COMPOSE) ps

db-up: ## Start only Postgres, for running the services with go run
	$(COMPOSE) up -d db

db-down: ## Stop Postgres
	$(COMPOSE) down

run-api: ## Run lead-api locally (needs make db-up)
	$(LOAD_ENV) && go run ./services/lead-api

run-sync: ## Run sheets-sync locally (needs make db-up)
	$(LOAD_ENV) && go run ./services/sheets-sync

test: ## Run all tests (starts its own Postgres container)
	go test ./...

generate: ## Regenerate sqlc code after editing db/queries
	go tool sqlc generate
