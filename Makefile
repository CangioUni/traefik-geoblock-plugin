.PHONY: test lint format check build clean help

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Build variables
BINARY_NAME=geoblock
VERSION?=0.1.0

all: test

## help: Display this help message
help:
	@echo "Available targets:"
	@echo "  test      - Run unit tests"
	@echo "  lint      - Run linter (requires golangci-lint)"
	@echo "  format    - Format Go code"
	@echo "  check     - Check Go code formatting"
	@echo "  build     - Build the plugin (verification only)"
	@echo "  clean     - Clean build artifacts"
	@echo "  tag       - Create a git tag for the version"

## test: Run unit tests
test:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

## lint: Run linter
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed. Installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	}
	golangci-lint run

## format: Format Go code
format:
	$(GOFMT) -w .

## check: Check if Go code is formatted correctly
check:
	@if [ -n "$$($(GOFMT) -l .)" ]; then \
		echo "Go code is not formatted. Run 'make format'"; \
		$(GOFMT) -l .; \
		exit 1; \
	fi

## build: Verify the plugin builds correctly
build:
	$(GOMOD) tidy
	$(GOMOD) verify
	$(GOBUILD) -v ./...

## clean: Clean build artifacts
clean:
	$(GOCLEAN)
	rm -f coverage.out

## tag: Create a git tag for the version
tag:
	@echo "Creating tag v$(VERSION)"
	git tag -a v$(VERSION) -m "Release version $(VERSION)"
	@echo "Tag created. Push with: git push origin v$(VERSION)"

## coverage: Display test coverage
coverage: test
	$(GOCMD) tool cover -html=coverage.out
