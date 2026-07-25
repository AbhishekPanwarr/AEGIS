FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY pkg/ ./pkg/
COPY services/gateway/ ./services/gateway/

ENV GOWORK=off
WORKDIR /app/services/gateway
RUN go build -o /gateway ./cmd/gateway

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /gateway /gateway

ENV PORT=8080

EXPOSE 8080

ENTRYPOINT ["/gateway"]
