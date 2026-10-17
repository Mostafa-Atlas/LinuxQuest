# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
FROM golang:1.22-alpine AS build
WORKDIR /src

# Copy everything, then resolve dependencies. `go mod tidy` also creates go.sum
# on a fresh checkout that does not have one yet. Commit go.sum for
# reproducible, checksum-verified builds.
COPY . .
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/linuxquest ./cmd/linuxquest

# Pre-create the data directory so the named volume inherits non-root ownership.
RUN mkdir -p /out/data

# ---- runtime stage ---------------------------------------------------------
# distroless "nonroot": no shell, no package manager, UID/GID 65532.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/linuxquest /usr/local/bin/linuxquest
COPY --from=build --chown=65532:65532 /out/data /data

ENV LINUXQUEST_HOST=0.0.0.0 \
    LINUXQUEST_PORT=2222 \
    LINUXQUEST_HOST_KEY=/data/ssh_host_ed25519 \
    LINUXQUEST_DB=/data/linuxquest.db \
    LINUXQUEST_WEB_PORT=8080

USER 65532:65532
VOLUME ["/data"]
EXPOSE 2222 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/linuxquest", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/linuxquest"]
