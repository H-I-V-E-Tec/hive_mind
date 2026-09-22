# --- Build Stage ---
FROM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS builder

# Install system dependencies needed for building
RUN apk add --no-cache git ca-certificates build-base

# Set the working directory inside the container
WORKDIR /app

# Copy dependency files first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the Go app as a highly optimized static binary
# Tree-sitter requires CGO; build against musl for the Alpine runtime.
ARG HIVE_VERSION=development
ARG HIVE_SOURCE_REVISION=unknown
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w -X main.Version=${HIVE_VERSION} -X main.SourceRevision=${HIVE_SOURCE_REVISION}" -o hive-mind .

# --- Runtime Stage ---
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# Add CA certificates for secure connections (essential for HTTPS calls to Qdrant Cloud or external APIs)
RUN apk add --no-cache ca-certificates tzdata

# The MCP process has no reason to run with administrative privileges.
RUN addgroup -S hive && adduser -S -D -H -u 10001 -G hive hive
RUN mkdir -p /var/lib/hive/audit && chown -R hive:hive /var/lib/hive && chmod 0700 /var/lib/hive/audit

# Set the working directory
WORKDIR /app

# Copy the pre-compiled binary from the builder stage
COPY --from=builder /app/hive-mind /app/hive-mind

USER hive
ENV HIVE_AUDIT_DIR=/var/lib/hive/audit

# Define the entrypoint to run the server
ENTRYPOINT ["/app/hive-mind"]
