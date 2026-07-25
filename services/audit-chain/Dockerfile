FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY pkg/ ./pkg/
COPY services/audit-chain/ ./services/audit-chain/

ENV GOWORK=off
WORKDIR /app/services/audit-chain
RUN go build -o /audit-chain ./cmd/audit

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /audit-chain /audit-chain

ENV PORT=8084

EXPOSE 8084

ENTRYPOINT ["/audit-chain"]
