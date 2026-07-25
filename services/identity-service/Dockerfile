FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY pkg/ ./pkg/
COPY services/identity-service/ ./services/identity-service/

ENV GOWORK=off
WORKDIR /app/services/identity-service
RUN go build -o /identity-service ./cmd/identity

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /identity-service /identity-service

ENV PORT=8081
ENV SIGNING_KEY_PATH=/data/identity_signing_key

EXPOSE 8081

ENTRYPOINT ["/identity-service"]
