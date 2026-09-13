# DEVELOPMENT TOOL image: rule-based mock AI agent (earthquake only).
# Build context: the supervisor directory.
FROM golang:1.22-alpine AS builder

# Set by BuildKit for the target platform; plain `docker build` leaves it
# empty, hence the amd64 fallback.
ARG TARGETARCH

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY internal/models ./internal/models
COPY scripts/agent ./scripts/agent

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /mock_agent ./scripts/agent

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /mock_agent ./mock_agent
EXPOSE 8082
ENTRYPOINT ["./mock_agent"]
