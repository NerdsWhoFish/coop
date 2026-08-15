BIN := coopd
PKG := github.com/nerdswhofish/coop
OUT := bin/$(BIN)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

DEV_DB_CONTAINER := coop-postgres
DEV_DB_DSN       := postgres://coop:coop@localhost:5433/coop?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the server binary into bin/
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT) ./cmd/$(BIN)

.PHONY: install
install: ## Build and install into ~/bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(HOME)/bin/$(BIN) ./cmd/$(BIN)

.PHONY: run
run: ## Run the server against the dev database
	COOP_DATABASE_DSN="$(DEV_DB_DSN)" go run ./cmd/$(BIN) serve

.PHONY: migrate
migrate: ## Apply migrations to the dev database
	COOP_DATABASE_DSN="$(DEV_DB_DSN)" go run ./cmd/$(BIN) migrate

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration on the dev database
	COOP_DATABASE_DSN="$(DEV_DB_DSN)" go run ./cmd/$(BIN) migrate-down

.PHONY: test
test: ## Run unit tests
	go test -race -shuffle=on ./...

.PHONY: test-integration
test-integration: ## Run tests that need the dev database
	COOP_TEST_DATABASE_DSN="$(DEV_DB_DSN)" go test -race -tags=integration ./...

.PHONY: cover
cover: ## Run tests and open a coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: lint
lint: ## Run go vet and staticcheck
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 \
		&& staticcheck ./... \
		|| echo "staticcheck not installed, skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"

.PHONY: fmt
fmt: ## Format the tree
	go fmt ./...

.PHONY: tidy
tidy: ## Tidy the module
	go mod tidy

.PHONY: dev-db
dev-db: ## Start a local Postgres for development
	docker run -d --rm \
		--name $(DEV_DB_CONTAINER) \
		-e POSTGRES_USER=coop \
		-e POSTGRES_PASSWORD=coop \
		-e POSTGRES_DB=coop \
		-p 5433:5432 \
		postgres:16-alpine
	@echo "waiting for postgres..."
	@until docker exec $(DEV_DB_CONTAINER) pg_isready -U coop >/dev/null 2>&1; do sleep 1; done
	@echo "ready at $(DEV_DB_DSN)"

.PHONY: dev-db-stop
dev-db-stop: ## Stop the local Postgres
	-docker stop $(DEV_DB_CONTAINER)

.PHONY: dev-key
dev-key: ## Generate an encryption key for local config
	@openssl rand -base64 32

.PHONY: clean
clean: ## Remove build output
	rm -rf bin coverage.out
