# Web3 Wallet API — common developer tasks.
# Run `make help` for the list.

BINARY      := server
PKG         := ./...
SWAG        := $(shell go env GOPATH)/bin/swag
DOCKER_IMG  := web3-wallet-api:latest

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API locally
	go run ./cmd/server

.PHONY: build
build: ## Build the binary into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(BINARY) ./cmd/server

.PHONY: test
test: ## Run all tests
	go test -race $(PKG)

.PHONY: cover
cover: ## Run tests with a coverage summary
	go test -cover $(PKG)

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	go mod tidy

.PHONY: swag
swag: ## Regenerate Swagger docs from annotations
	$(SWAG) init -g internal/api/router.go -o docs --parseDependency --parseInternal

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -t $(DOCKER_IMG) .

.PHONY: docker-up
docker-up: ## Start the stack with docker compose
	docker compose up --build

.PHONY: clean
clean: ## Remove build artifacts and local keystore data
	rm -rf bin data
