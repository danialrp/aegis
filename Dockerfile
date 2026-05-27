# syntax=docker/dockerfile:1.7
#
# Aegis controller image. Multi-stage:
#   1. node — builds the Vite SPA into web/dist/
#   2. go-builder — cross-builds the linux/{amd64,arm64} agent binaries
#      so they can be embedded, then builds the controller with
#      -tags=embedagent
#   3. runtime — distroless static, just the controller binary + the
#      embedded SPA + CA storage volume
#
# Result: a self-contained controller that needs only Postgres on the
# other end. No node, no go, no shell on the runtime image.

FROM node:22-alpine AS web-builder
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.25-alpine AS go-builder
RUN apk add --no-cache git make
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# SPA produced by the previous stage lands inside the embed path.
COPY --from=web-builder /web/dist ./web/dist

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=docker
ARG COMMIT=unknown

ENV CGO_ENABLED=0
ENV GOFLAGS=-trimpath

# Cross-compile agent for both supported target arches so the embed
# carries both — the host Aegis manages may be either.
RUN mkdir -p internal/agentbinary/bin/linux-amd64 internal/agentbinary/bin/linux-arm64 && \
    GOOS=linux GOARCH=amd64 go build \
        -ldflags "-s -w -X github.com/danialrp/aegis/internal/version.Version=${VERSION} -X github.com/danialrp/aegis/internal/version.Commit=${COMMIT}" \
        -o internal/agentbinary/bin/linux-amd64/aegis-agent ./cmd/agent && \
    GOOS=linux GOARCH=arm64 go build \
        -ldflags "-s -w -X github.com/danialrp/aegis/internal/version.Version=${VERSION} -X github.com/danialrp/aegis/internal/version.Commit=${COMMIT}" \
        -o internal/agentbinary/bin/linux-arm64/aegis-agent ./cmd/agent

# Controller for whatever the image arch is.
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -tags=embedagent \
    -ldflags "-s -w -X github.com/danialrp/aegis/internal/version.Version=${VERSION} -X github.com/danialrp/aegis/internal/version.Commit=${COMMIT}" \
    -o /out/aegis-controller ./cmd/controller

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=go-builder /out/aegis-controller /usr/local/bin/aegis-controller
EXPOSE 8080 8443
VOLUME ["/var/lib/aegis/ca"]
# We intentionally run as root inside the container — a dedicated
# in-container user buys nothing here (the controller is the only
# process). Isolation comes from running the container itself with
# user-namespace remapping or a non-default Docker security profile,
# both of which are operator concerns.
ENTRYPOINT ["/usr/local/bin/aegis-controller"]
