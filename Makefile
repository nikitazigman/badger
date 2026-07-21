.DEFAULT_GOAL := help

.PHONY: help build run test fixtures-bbolt

BINARY := bin/badger
MAIN := ./cmd/badger

help: ## Show available make commands
	@echo "Available commands:"
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the main binary
	@mkdir -p $(dir $(BINARY))
	go build -o $(BINARY) $(MAIN)

run: ## Run the main program (pass ARGS='...')
	go run $(MAIN) $(ARGS)

fixtures-bbolt: ## Generate bbolt fixture databases
	go run fixtures/bbolt/generate.go

test: fixtures-bbolt ## Run all tests
	go test -v ./...
