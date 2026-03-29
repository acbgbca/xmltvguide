# ── Build stage ──────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod ./
# No go.sum needed — stdlib only, no external dependencies
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tvguide .

# ── Runtime stage ─────────────────────────────────────────────────
FROM alpine:3.20

# ca-certificates: needed for HTTPS requests to fetch the XMLTV URL
# tzdata: needed so TZ env var correctly sets the server's local timezone
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/tvguide ./

EXPOSE 8080

ENTRYPOINT ["./tvguide"]
