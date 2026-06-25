# syntax=docker/dockerfile:1

# ---- Build stage ------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache dependencies first for faster incremental builds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static, CGO-free binary so it runs on a distroless/scratch base.
# -trimpath and stripped symbols (-s -w) keep the image small and reproducible.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server

# ---- Runtime stage ----------------------------------------------------------
# distroless/static: no shell, no package manager — minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/server /app/server

# Persisted, encrypted keystore lives here; mount a volume to keep keys.
ENV KEYSTORE_DIR=/app/data/keystore
ENV PORT=8080
EXPOSE 8080

# Runs as the non-root user provided by the distroless image.
USER nonroot:nonroot

ENTRYPOINT ["/app/server"]
