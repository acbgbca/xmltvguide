# ── Build stage ──────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

# ca-certificates is needed at runtime for HTTPS; install here so we can
# copy just the cert bundle into the scratch image.
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy all source first so go mod tidy can resolve imports.
# tidy scans .go files, downloads dependencies, and writes go.mod + go.sum.
# This means Go does not need to be installed on the host machine.
#
# GOTOOLCHAIN=auto allows the Go toolchain to upgrade itself if a dependency
# requires a newer Go version than the base image provides.
#
# After the first successful build, commit the updated go.mod and go.sum
# back to the repository for reproducible builds.
COPY . .
ENV GOTOOLCHAIN=auto
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tvguide .

# ── Runtime stage ─────────────────────────────────────────────────
# scratch: zero OS overhead (~0 MB vs ~8 MB for alpine).
# Timezone data is embedded in the binary via `import _ "time/tzdata"`,
# so only the CA certificate bundle needs to be copied in.
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tvguide /tvguide

EXPOSE 8080

ENTRYPOINT ["/tvguide"]
