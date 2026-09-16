.PHONY: build binary install test fmt clean conformance

BIN := bin/arno-mcp

# VERSION is stamped into the binary so a running server can say what it is.
# Falls back to "dev" outside a git checkout rather than failing the build.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# build stays `go build ./...` — it is what arno's own check(build) discovers
# and runs, so it has to compile everything quickly rather than produce an
# artifact. Use `make binary` for that.
build:
	go build ./...

binary:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/arno-mcp

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/arno-mcp

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

clean:
	rm -rf bin

# conformance builds an image containing every language server arno supports
# and runs the conformance suite inside it. Slow and large on purpose: it is
# the only way to prove the semantic path against real servers, none of which
# are installed on a typical developer machine.
conformance:
	docker build -f test/conformance/Dockerfile -t arno-conformance .
	docker run --rm arno-conformance
