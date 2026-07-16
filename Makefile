# observability-mcp — dev shortcuts
BINARY      := observability-mcp
PKG         := github.com/truepace-io-oss/observability-mcp-server
VERSION     ?= dev
LDFLAGS     := -s -w -X main.version=$(VERSION)

.PHONY: build test test-e2e lint helm-lint run tidy fmt

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

## Unit tests (no external backend needed; uses httptest fakes)
test:
	go test ./internal/... -race -count=1

## End-to-end: full MCP server over HTTP against fake backends
test-e2e:
	go test ./test/e2e/... -race -count=1 -v

lint:
	go vet ./...
	gofmt -l -d .

fmt:
	gofmt -w .

helm-lint:
	helm lint deploy/helm/observability-mcp

run:
	go run . --config examples/config.yaml

tidy:
	go mod tidy
