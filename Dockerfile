# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/memory-button-api ./cmd/api

FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata ffmpeg su-exec
COPY --from=build /out/memory-button-api /app/memory-button-api
COPY migrations /app/migrations
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh && mkdir -p /data/storage && chown -R app:app /data/storage
EXPOSE 8080
ENV HTTP_ADDR=:8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
