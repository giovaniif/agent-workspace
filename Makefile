VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
VERSION_PKG := github.com/giovaniif/agent-workspace/internal/version
LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT)
GO_TEST_FLAGS ?= -p 2
GREMLINS ?= $(shell go env GOPATH)/bin/gremlins

.PHONY: build test lint bench mutate e2e dev web

build:
	go build -ldflags "$(LDFLAGS)" -o bin/agentws ./cmd/agentws

test:
	go test $(GO_TEST_FLAGS) ./...

lint:
	golangci-lint run ./...
	go run ./scripts/lint-comments .
	go run ./scripts/lint-agents .

bench:
	go test $(GO_TEST_FLAGS) -run '^$$' -bench . -benchtime 1s ./...

mutate:
	./scripts/mutate $(GREMLINS)

e2e:
	go test $(GO_TEST_FLAGS) ./test/e2e/...

dev:
	SEED=$(SEED) FAKES=$(FAKES) ./scripts/dev

web:
	cd web && npm ci --no-audit --no-fund --ignore-scripts
	find internal/serve/dist -mindepth 1 ! -name index.html -delete
	cd web && npm run build
