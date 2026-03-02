.PHONY: build build-dev test health clean help fmt version

GOBIN := $(shell go env GOPATH)/bin

# Version info - uses git tag if available, otherwise "dev"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# ldflags for version injection
LDFLAGS := -X github.com/leightonvanrooijen/utopia/internal/cli.Version=$(VERSION) \
           -X github.com/leightonvanrooijen/utopia/internal/cli.Commit=$(COMMIT) \
           -X github.com/leightonvanrooijen/utopia/internal/cli.BuildDate=$(BUILD_DATE)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the utopia binary with version info
	go build -ldflags "$(LDFLAGS)" -o utopia ./cmd/utopia

build-dev: ## Build without version info (faster for development)
	go build -o utopia ./cmd/utopia

version: ## Show version that would be embedded
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

test: ## Run all tests
	go test ./...

health: ## Run staticcheck on entire project
	$(GOBIN)/staticcheck ./...

clean: ## Remove built artifacts
	rm -f utopia

fmt: ## Format all Go files
	go fmt ./...
	@[ -f $(GOBIN)/goimports ] && $(GOBIN)/goimports -w . || true
