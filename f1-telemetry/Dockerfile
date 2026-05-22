# Two-stage build. Produces a small image containing both binaries; the
# docker-compose file decides which one to run via `command:`.

FROM golang:1.22-alpine AS build
WORKDIR /src

# Cache deps separately for fast incremental builds.
COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .

RUN CGO_ENABLED=0 go build -o /out/server    ./cmd/server
RUN CGO_ENABLED=0 go build -o /out/simulator ./cmd/simulator

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/server    /app/server
COPY --from=build /out/simulator /app/simulator
USER nonroot:nonroot
# Default to running the server; docker-compose overrides per-service.
CMD ["/app/server"]
