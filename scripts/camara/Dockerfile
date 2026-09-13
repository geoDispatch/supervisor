# DEVELOPMENT TOOL image: mock CAMARA server with synthetic fixtures.
# Build context: the supervisor directory.
FROM golang:1.22-alpine AS builder

# Set by BuildKit for the target platform; plain `docker build` leaves it
# empty, hence the amd64 fallback.
ARG TARGETARCH

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# The whole package directory: fixtures.json is embedded with go:embed.
COPY internal/models ./internal/models
COPY scripts/camara ./scripts/camara

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /mock_camara ./scripts/camara

FROM alpine:3.19

RUN apk add --no-cache ca-certificates
COPY --from=builder /mock_camara ./mock_camara
EXPOSE 8081
ENTRYPOINT ["./mock_camara"]
