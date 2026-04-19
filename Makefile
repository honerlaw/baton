# Developer shortcuts. Everything here is equivalent to typing the
# underlying command; CI uses the same invocations directly.

BINARY := baton
GOFUMPT_VERSION   := v0.7.0
GOIMPORTS_VERSION := v0.24.0
GOLANGCI_VERSION  := v1.64.5

.PHONY: all build test vet lint fmt fmt-check validate clean tools ci

all: build

build:
	go build -trimpath -o $(BINARY) ./cmd/baton

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# Format in place. Uses `go run` to pin versions without polluting go.mod.
fmt:
	go run mvdan.cc/gofumpt@$(GOFUMPT_VERSION) -w .
	go run golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION) -local github.com/honerlaw/baton -w .

# Non-mutating check — fails if anything would be reformatted.
fmt-check:
	@diff=$$(go run mvdan.cc/gofumpt@$(GOFUMPT_VERSION) -l .); \
	if [ -n "$$diff" ]; then echo "needs gofumpt:"; echo "$$diff"; exit 1; fi

validate: build
	./$(BINARY) validate internal/assets/workflows/default.yaml
	./$(BINARY) validate internal/assets/workflows/minimal.yaml
	./$(BINARY) validate internal/assets/workflows/iter-design.yaml

# One-shot for devs who want the CI check locally.
ci: vet test lint

# Install the lint tool locally for devs who don't have it yet.
tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VERSION)

clean:
	rm -f $(BINARY)
