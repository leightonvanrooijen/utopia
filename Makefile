.PHONY: build test health clean help fmt

GOBIN := $(shell go env GOPATH)/bin

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the utopia binary
	go build -o utopia ./cmd/utopia

test: ## Run all tests
	go test ./...

health: ## Run staticcheck on entire project
	$(GOBIN)/staticcheck ./...

clean: ## Remove built artifacts
	rm -f utopia

fmt: ## Format all Go files
	go fmt ./...
	@[ -f $(GOBIN)/goimports ] && $(GOBIN)/goimports -w . || true
