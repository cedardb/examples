# Two-stage build. Produces a small image containing both binaries; the
# docker-compose file decides which one to run via `command:`.

FROM golang:1.22-alpine AS build
WORKDIR /src

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

# Use CMD (not ENTRYPOINT) so docker-compose's per-service `command:` fully
# REPLACES this rather than being appended as argv to the default binary.
CMD ["/app/server"]
