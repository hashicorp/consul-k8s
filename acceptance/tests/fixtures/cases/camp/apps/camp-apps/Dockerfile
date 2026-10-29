# syntax=docker/dockerfile:1

# Builds the CAMP apps into one image: ai-app, plus the vendored MCP gateway
# (consul-mcp-gateway, which stamps the x-mcp-* headers and is launched via
# `consul connect ai-agent`). Also bundles the local Consul CLI and Envoy so
# the ai-app Connect sidecar Envoy can run *inside this same container*.
#
# NOTE: binaries/consul and binaries/consul-mcp-gateway are precompiled
# linux/arm64 ELF binaries checked into camp-apps/binaries. Build this image
# for the host (arm64) so they run natively.

# ---- Go build stage (ai-app only) ----
FROM golang:1.24-alpine AS build

WORKDIR /src

# Prime the module cache. The BuildKit cache mount persists the Go module cache
# across builds so `go mod download` never re-fetches from the network.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# ai-app is self-contained (single package); copy only it so the checked-in
# binaries/ are not pulled into the build stage.
COPY ai-app ./ai-app
RUN --mount=type=cache,target=/root/.cache/go \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ai-app ./ai-app

# ---- Binary source for the in-container mesh sidecar ----
# Envoy needs glibc (no musl/alpine build exists).
FROM envoyproxy/envoy:v1.38.3 AS envoy

# ---- Runtime stage (glibc, so Envoy can run) ----
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      wget \
      net-tools \
      iproute2 \
      iptables \
      dnsutils \
      procps \
      iputils-ping \
      gosu \
      python3 \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --create-home --shell /usr/sbin/nologin camp \
 && useradd --uid 5995  --create-home --shell /usr/sbin/nologin envoy

# Local Consul CLI (with `connect ai-agent`, execs Envoy + launches the MCP
# gateway) + Envoy binary for the in-container sidecar.
COPY --from=envoy /usr/local/bin/envoy /usr/local/bin/envoy

WORKDIR /app
COPY --from=build /out/ai-app      /app/ai-app
# Precompiled MCP gateway binary, launched by `consul connect ai-agent`.
# Vendored Python HITL approver (sam-kit HITLWatcher on :16101) + its package.
COPY hitl                          /app/hitl
COPY entrypoint.sh                 /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# NOTE: the container starts as root so entrypoint.sh can apply the transparent
# proxy iptables redirect (consul connect redirect-traffic, needs NET_ADMIN),
# then drops privileges: the apps run as `camp`, the sidecar Envoy as `envoy`
# (uid 5995, excluded from the redirect).

# consul-mcp-gateway ext_proc gRPC :21101 | HITL HTTP :16101 | ai-app :8080 |
# ai-app sidecar Envoy :21000
EXPOSE 21101 16101 8080 21000

ENTRYPOINT ["/app/entrypoint.sh"]
