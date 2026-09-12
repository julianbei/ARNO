# Build jade-mcp for use inside another project's container.
#
# Jade is not a service — it is a stdio MCP server that an agent harness
# spawns as a child process. So this image exists to *produce the binary*,
# not to run as a long-lived container. The intended use is a build stage:
#
#   COPY --from=ghcr.io/julianbei/jade-mcp:v0.0.1 /jade-mcp /usr/local/bin/jade-mcp
#
# Running the image directly starts the server on stdio, which is only useful
# if you are attaching an MCP client to the container's stdin/stdout.

FROM golang:1.24-alpine AS build

# tree-sitter's Go binding is cgo, so a C toolchain is required — jade cannot
# be built with CGO_ENABLED=0. git is for the version stamp fallback.
RUN apk add --no-cache build-base git

WORKDIR /src

# Dependencies first, so a source-only change does not re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION is passed in by the release workflow (the git tag). It falls back to
# git describe for local builds, and to "dev" when neither is available, rather
# than failing — a binary that cannot say what it is still beats no binary.
#
# The binary is statically linked against musl so it can run in a scratch or
# distroless image despite needing cgo.
ARG VERSION
RUN VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}" && \
    CGO_ENABLED=1 go build -trimpath \
        -ldflags "-s -w -linkmode external -extldflags '-static' -X main.version=${VERSION}" \
        -o /jade-mcp ./cmd/jade-mcp && \
    /jade-mcp --version

# Runtime stage. Distroless static: jade shells out to git, gopls and the
# project's own build tools when they exist, and degrades with an explicit
# message when they do not — so a minimal image is a usable image, just with
# `references`/`rename` in their textual fallback and `changes`/`diff` off.
# Install git and gopls in your own image if you want the full surface.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /jade-mcp /jade-mcp

# JADE_WORKSPACE_ROOT is the repository jade inspects. Mount it here.
ENV JADE_WORKSPACE_ROOT=/workspace
WORKDIR /workspace

ENTRYPOINT ["/jade-mcp"]
