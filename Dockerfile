# syntax=docker/dockerfile:1

# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Download and verify dependencies before copying source so this layer is
# cached as long as go.mod/go.sum are unchanged — the most expensive step.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the full source tree (controlled by .dockerignore).
COPY . .

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

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
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
COPY --chown=kb:kb index.html .

EXPOSE 8080

# ENTRYPOINT instead of CMD: makes the container a proper executable.
# If you need to pass flags later, add them as CMD ["--flag"].
ENTRYPOINT ["./knowledge-base"]