GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN     := bin/sigame
PKG     := ./cmd/sigame

.PHONY: all build run test test-short vet fmt lint cross clean openapi web web-install web-dev web-check release

all: build

# --- web client (web/, React + Vite; embedded into the binary from web/dist) ---
web-install:
	cd web && npm ci

web: web-install
	cd web && npm run build

web-dev:
	cd web && npm run dev

web-check:
	cd web && npm run typecheck && npm run lint && npm test -- --run

# Full release build: web client first, then the Go binary that embeds it.
release: web build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

run:
	CGO_ENABLED=0 $(GO) run $(PKG)

test:
	$(GO) test -race -count=1 ./...

test-short:
	$(GO) test -short -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

lint: vet
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed (go install honnef.co/go/tools/cmd/staticcheck@latest)"

# Cross-compile static binaries for release.
cross: web
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/sigame-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/sigame-linux-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/sigame-darwin-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/sigame-darwin-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/sigame-windows-amd64.exe $(PKG)

# Dump the OpenAPI spec without starting the network listener.
openapi: build
	./$(BIN) --print-openapi > docs/openapi.json

clean:
	rm -rf bin dist coverage.out
