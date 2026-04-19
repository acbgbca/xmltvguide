# ── Build stage ──────────────────────────────────────────────────
FROM golang:1.26.2-alpine@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 AS builder

# ca-certificates is needed at runtime for HTTPS; install here so we can
# copy just the cert bundle into the scratch image.
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy module manifests first so this layer is cached until dependencies change.
COPY go.mod go.sum ./
RUN go mod download

# Now copy source and build.
COPY . .

# Stamp the service-worker cache name with the build version so that a new
# deployment always invalidates the PWA cache on the client.
ARG VERSION=dev
RUN sed -i "s/__CACHE_VERSION__/${VERSION}/" web/sw.js

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tvguide .

# Create an empty /tmp directory to carry into the scratch image.
# SQLite FTS5 segment operations require a writable temp directory even when
# PRAGMA temp_store=MEMORY is set; without /tmp the second+ refresh fails
# with SQLITE_IOERR_WRITE (6410). See GitHub issue #87.
RUN mkdir /scratch_tmp && chmod 1777 /scratch_tmp

# ── Runtime stage ─────────────────────────────────────────────────
# scratch: zero OS overhead (~0 MB vs ~8 MB for alpine).
# Timezone data is embedded in the binary via `import _ "time/tzdata"`,
# so only the CA certificate bundle needs to be copied in.
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tvguide /tvguide
COPY --from=builder /scratch_tmp /tmp

EXPOSE 8080

# Run as non-root (numeric UID required for scratch images which have no /etc/passwd).
# UID 65534 is the conventional "nobody" user on Linux.
USER 65534:65534

ENTRYPOINT ["/tvguide"]
