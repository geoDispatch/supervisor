FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY scripts/camara/mock_camara.go ./scripts/camara/mock_camara.go

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /mock_camara ./scripts/camara/mock_camara.go

FROM alpine:3.19

RUN apk add --no-cache ca-certificates
COPY --from=builder /mock_camara ./mock_camara
EXPOSE 8081
ENTRYPOINT ["./mock_camara"]