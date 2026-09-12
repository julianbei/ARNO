# A machine with every language server jade supports, so the conformance
# suite can prove the semantic path against the real thing rather than a fake.
#
# No developer machine has all eight installed, and jade's own CI machine
# certainly does not. That gap is exactly where the interesting bugs live: a
# server whose startup needs an option jade does not send, one that answers
# rename in the DocumentChanges shape rather than Changes, one that reports
# positions jade converts wrongly. None of those are reachable without the
# server present, and all of them ship silently otherwise.
#
# The image is large and slow to build. That is the price of the answer being
# real, and it is built rarely — the fixtures and the suite change far more
# often than the toolchains do.

FROM golang:1.24-bookworm

ENV DEBIAN_FRONTEND=noninteractive
ENV PATH="/root/go/bin:/root/.cargo/bin:/root/.local/bin:/opt/jdtls/bin:/root/.local/share/coursier/bin:${PATH}"

# Base toolchains. build-essential is required regardless of language: jade
# itself needs cgo for tree-sitter.
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        git \
        unzip \
        python3 \
        python3-pip \
        python3-venv \
        ruby-full \
        openjdk-17-jdk \
        maven \
        nodejs \
        npm \
    && rm -rf /var/lib/apt/lists/*

# --- Go: gopls -------------------------------------------------------------
RUN go install golang.org/x/tools/gopls@latest

# --- TypeScript / JavaScript ----------------------------------------------
RUN npm install -g typescript typescript-language-server

# --- Python: pyright -------------------------------------------------------
# The npm distribution is used rather than pip's because it ships the
# `pyright-langserver` stdio entry point jade launches.
RUN npm install -g pyright

# --- Ruby ------------------------------------------------------------------
# ruby-lsp is jade's first choice; solargraph is its configured alternative,
# so installing both exercises the alternative-resolution path too.
RUN gem install --no-document ruby-lsp solargraph

# --- Rust: rust-analyzer ---------------------------------------------------
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
        | sh -s -- -y --profile minimal --default-toolchain stable \
    && rustup component add rust-analyzer \
    # rustup exposes rust-analyzer through a proxy shim that is not on PATH
    # under the name jade looks for, so link the real binary. The shim being
    # present while the binary is missing is precisely the failure seen on the
    # development machine, where `rust-analyzer --version` reported
    # "Unknown binary in official toolchain".
    && ln -sf "$(rustup which rust-analyzer)" /usr/local/bin/rust-analyzer

# --- Java: jdtls -----------------------------------------------------------
RUN mkdir -p /opt/jdtls \
    && curl -fsSL https://download.eclipse.org/jdtls/milestones/1.40.0/jdt-language-server-1.40.0-202409261450.tar.gz \
        | tar xz -C /opt/jdtls

# --- Scala: metals ---------------------------------------------------------
# Installed through coursier, which is how metals is actually distributed.
RUN curl -fsSL "https://github.com/coursier/launchers/raw/master/cs-x86_64-pc-linux.gz" \
        | gzip -d > /usr/local/bin/cs \
    && chmod +x /usr/local/bin/cs \
    && cs install metals --install-dir /usr/local/bin || true

WORKDIR /jade
COPY . .

# Build once at image build time so a test run is not also a compile.
RUN go build -o /usr/local/bin/jade-mcp ./cmd/jade-mcp
ENV JADE_MCP_BINARY=/usr/local/bin/jade-mcp

# Warm the module cache for the fixture repos that need one, so the first
# language server to start is not also downloading a toolchain.
RUN cd test/conformance/repos/rust && cargo fetch || true

CMD ["go", "test", "./test/conformance/", "-v", "-timeout", "30m"]
