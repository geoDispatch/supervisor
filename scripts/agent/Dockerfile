FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY scripts/agent/mock_agent.go ./scripts/agent/mock_agent.go

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /mock_agent ./scripts/agent/mock_agent.go


FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /mock_agent ./mock_agent
EXPOSE 5000
ENTRYPOINT ["./mock_agent"]