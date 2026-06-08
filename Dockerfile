FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ai-gateway ./cmd/server

FROM alpine:3.20
WORKDIR /app
RUN adduser -D -u 10001 appuser && mkdir -p /data && chown -R appuser:appuser /data
COPY --from=build /out/ai-gateway /app/ai-gateway
COPY openapi.yaml /app/openapi.yaml
COPY gateway-config.json /app/gateway-config.json
USER appuser
EXPOSE 8080
ENV DB_PATH=/data/gateway.db
ENV CONFIG_PATH=/app/gateway-config.json
ENV PORT=8080
ENV GIN_MODE=release
CMD ["/app/ai-gateway"]
