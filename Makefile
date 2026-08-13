.PHONY: fmt test test-race vet build clean release dist-%

# Version metadata injected via -ldflags. Override on the command line, e.g.
#   make build VERSION=v0.1.0 COMMIT=$(git rev-parse --short HEAD)
VERSION ?= development
COMMIT  ?=
BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)

# Target platforms for the release target. windows uses .exe suffix.
TARGETS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	windows/arm64

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/ai-code-provenance ./cmd/ai-prov
	go build -ldflags "$(LDFLAGS)" -o bin/ai-code-provenance-mcp ./cmd/ai-prov-mcp

# release cross-compiles both binaries for every TARGETS platform into dist/.
release: clean
	mkdir -p dist
	@set -e; for target in $(TARGETS); do \
		goos=$${target%/*}; \
		goarch=$${target#*/}; \
		ext=""; \
		if [ $$goos = windows ]; then ext=".exe"; fi; \
		echo "building $$goos/$$goarch"; \
		GOOS=$$goos GOARCH=$$goarch go build -ldflags "$(LDFLAGS)" \
			-o dist/ai-prov-$$goos-$$goarch$$ext ./cmd/ai-prov; \
		GOOS=$$goos GOARCH=$$goarch go build -ldflags "$(LDFLAGS)" \
			-o dist/ai-prov-mcp-$$goos-$$goarch$$ext ./cmd/ai-prov-mcp; \
	done
	@ls -1 dist/

clean:
	rm -rf bin dist coverage.out
