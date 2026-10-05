# Backend image: the API server, ingestion CLI and aggregator in one image;
# the container's command picks which runs (default: the API server).
#
#   docker build -t tft-platform-backend .
#
# Plain Dockerfile syntax (no BuildKit-only features) so it builds with the
# classic builder too.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Dependencies first, so code changes don't invalidate the module cache layer.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/ingestcli ./cmd/aggregator

FROM alpine:3.21
# CA certificates for HTTPS to the Riot API and CommunityDragon; wget (from
# busybox) for the health check.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 app
COPY --from=build /out/ /usr/local/bin/
COPY scripts/ingest-loop.sh /usr/local/bin/ingest-loop
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
CMD ["api"]
