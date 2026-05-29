# syntax=docker/dockerfile:1

# ── Stage 1: Build Tailwind CSS ───────────────────────────────────────────────
# Debian slim has glibc — required by the Tailwind standalone binary.
# Alpine uses musl and cannot execute the binary even if it downloads correctly.
FROM debian:bookworm-slim AS css-builder

# BUILDARCH reflects the machine running this stage (the builder).
# TARGETARCH reflects what the Go binary will run on — different when cross-compiling.
# The Tailwind binary must match the builder, not the target.
ARG BUILDARCH

RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Download the correct standalone Tailwind CLI binary for the build architecture.
# Must happen before WORKDIR so the path is /usr/local/bin, not /build/usr/local/bin.
RUN set -eux; \
    case "${BUILDARCH}" in \
      amd64) TW_ARCH="x64"  ;; \
      arm64) TW_ARCH="arm64" ;; \
      *)     echo "Unsupported arch: ${BUILDARCH}" && exit 1 ;; \
    esac; \
    curl -fsSL \
      "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.1.7/tailwindcss-linux-${TW_ARCH}" \
      -o /usr/local/bin/tailwindcss && \
    chmod +x /usr/local/bin/tailwindcss

WORKDIR /build

RUN tailwindcss --version

COPY tailwind.config.js .
COPY web/static/css/tailwind.src ./web/static/css/tailwind.src
COPY web/templates                   ./web/templates
COPY web/static/js                   ./web/static/js

RUN tailwindcss \
      --input ./web/static/css/tailwind.src \
      --output ./web/static/css/tailwind.css

# ── Stage 2: Build ────────────────────────────────────────────────────────────
FROM golang:1.26.3-alpine AS builder

WORKDIR /app

# Download and verify dependencies before copying source so this layer is
# cached as long as go.mod/go.sum are unchanged — the most expensive step.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the full source tree (controlled by .dockerignore).
COPY . .

# Overwrite the placeholder/missing tailwind.css with the compiled output
# from the css-builder stage so it gets embedded into the binary.
COPY --from=css-builder /build/web/static/css/tailwind.css ./web/static/css/tailwind.css

# Build a statically linked, stripped binary.
#
# CGO_ENABLED=0   — pure Go, no libc dependency, binary runs on scratch/alpine
# -ldflags -s -w  — strip symbol table and DWARF; reduces binary size ~30 %
# -trimpath       — remove local build paths from the binary (reproducibility + security)
# TARGETOS/ARCH   — populated automatically by `docker buildx build --platform …`
#                   so the same Dockerfile produces amd64 and arm64 images
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o knowledge-base \
      .

# ── Stage 3: Runtime ──────────────────────────────────────────────────────────
# Pinned to a specific minor version — no surprise updates between deploys.
FROM alpine:3.23.4

# ca-certificates — required for TLS connections to Ollama and PostgreSQL.
# tzdata          — keeps log timestamps correct if TZ is set in the pod spec.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Non-root user. Most Kubernetes clusters enforce this via PodSecurity or OPA.
RUN addgroup -S kb && adduser -S kb -G kb
USER kb

# Copy only what the binary needs at runtime.
COPY --from=builder --chown=kb:kb /app/knowledge-base .

EXPOSE 8080

# ENTRYPOINT instead of CMD: makes the container a proper executable.
# If you need to pass flags later, add them as CMD ["--flag"].
ENTRYPOINT ["./knowledge-base"]