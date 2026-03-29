# ── Build stage ──────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

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
FROM alpine:3.20

# ca-certificates: needed for HTTPS requests to fetch the XMLTV URL
# tzdata: needed so the TZ env var correctly sets the server's local timezone
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/tvguide ./

EXPOSE 8080

ENTRYPOINT ["./tvguide"]
