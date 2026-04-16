MODULE = $(shell go list -m)
VERSION ?= $(shell git describe --tags --always --dirty --match=v* 2> /dev/null || echo "0.0.0")
GOBIN ?= $$(go env GOPATH)/bin
GOLINT := golangci-lint
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(shell date -u +%FT%TZ)"

.PHONY: default
default: build

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the grc binary
	go build $(LDFLAGS) -o bin/grc ./cmd/grc

.PHONY: install
install: ## Install grc to GOBIN
	go install $(LDFLAGS) ./cmd/grc

.PHONY: test
test: ## Run all tests
	go test -v -race ./...

.PHONY: coverage
coverage: ## Generate coverage report
	go test -coverprofile=cover.out -covermode=atomic -coverpkg=./... ./...
	go tool cover -func=cover.out

.PHONY: coverage-html
coverage-html: coverage ## Open coverage report in browser
	go tool cover -html=cover.out -o cover.html

.PHONY: install-go-test-coverage
install-go-test-coverage:
	go install github.com/vladopajic/go-test-coverage/v2@latest

.PHONY: check-coverage
check-coverage: install-go-test-coverage ## Check coverage thresholds
	go test ./... -coverprofile=./cover.out -covermode=atomic -coverpkg=./...
	${GOBIN}/go-test-coverage --config=./.testcoverage.yml

.PHONY: lint
lint: ## Run golangci-lint
	@if command -v $(GOLINT) > /dev/null 2>&1; then \
		$(GOLINT) run ./...; \
	else \
		echo "golangci-lint not installed. Run: make tools"; \
	fi

.PHONY: fmt
fmt: ## Format code
	go fmt ./...
	@if command -v goimports > /dev/null 2>&1; then \
		goimports -w -local $(MODULE) .; \
	fi

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Tidy module dependencies
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	go clean
	rm -f bin/grc cover.out cover.html

.PHONY: tools
tools: ## Install development tools
	go install github.com/golangci-lint/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/vladopajic/go-test-coverage/v2@latest

.PHONY: setup
setup: tools ## Set up development environment
	@echo "Development tools installed."
