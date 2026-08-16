# ── Build stage ──────────────────────────────────────────────────
FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

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

# Create an empty /data directory to carry into the runtime image.
# When Docker mounts an empty named volume on top of a directory that exists
# in the image, the volume inherits the image directory's ownership and mode.
# Without this, a fresh volume is created root-owned and the non-root runtime
# user (UID 65532) cannot write the SQLite DB or image cache. See issue #259.
RUN mkdir /scratch_data && chown 65532:65532 /scratch_data

# ── Runtime stage ─────────────────────────────────────────────────
# distroless static-debian12:nonroot — minimal (~2 MB) image that ships:
#   - /tmp as drwxrwxrwt (mode 1777) — writable by any UID, which fixes
#     issue #263 (Docker BuildKit's COPY --chmod does NOT apply to the
#     destination directory itself, only files inside, so the previous
#     scratch-based approach left /tmp at 0755 and broke for any user
#     not matching its owner UID)
#   - /etc/ssl/certs/ca-certificates.crt (no manual copy needed)
#   - /etc/passwd containing the `nonroot` user (UID/GID 65532)
# Timezone data is embedded in the binary via `import _ "time/tzdata"`.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a

COPY --from=builder /app/tvguide /tvguide
# --chown is required on the COPY because Docker's COPY does not preserve the
# source directory's ownership for the destination directory itself (only for
# its contents). Without this, a fresh /data volume inherits root:0755 and
# the non-root runtime user cannot write the SQLite DB. See issue #259.
COPY --from=builder --chown=65532:65532 /scratch_data /data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/tvguide", "--healthcheck"]

# Run as non-root. UID 65532 is the conventional "nonroot" user shipped by
# distroless. /tmp is mode 1777 in the base image so users overriding this
# via `docker run --user` or compose `user:` can still write temp files.
USER 65532:65532

ENTRYPOINT ["/tvguide"]
