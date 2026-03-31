# ── Build stage ──────────────────────────────────────────────────
FROM golang:1.25.8-alpine AS builder

# ca-certificates is needed at runtime for HTTPS; install here so we can
# copy just the cert bundle into the scratch image.
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy module manifests first so this layer is cached until dependencies change.
COPY go.mod go.sum ./
RUN go mod download

# Now copy source and build.
COPY . .
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
