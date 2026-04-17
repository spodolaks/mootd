FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mootd-api ./cmd/api

FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -g '' appuser

WORKDIR /app

COPY --from=builder /out/mootd-api /app/mootd-api

USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/mootd-api"]
