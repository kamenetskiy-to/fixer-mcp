FROM ubuntu:24.04

ARG GO_VERSION=1.25.4

LABEL fixer-mcp.smoke="true"

ENV DEBIAN_FRONTEND=noninteractive
ENV PATH="/usr/local/go/bin:${PATH}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        make \
        nodejs \
        npm \
        python3 \
        sqlite3 \
    && rm -rf /var/lib/apt/lists/*

RUN set -eu; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) go_arch="amd64" ;; \
        arm64) go_arch="arm64" ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz" -o /tmp/go.tgz; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -f /tmp/go.tgz; \
    go version

WORKDIR /workspace/self_orchestration
COPY . .

RUN chmod +x docker/fixer-smoke.sh tests/fixer_mcp_stdio_smoke.py

CMD ["docker/fixer-smoke.sh"]
