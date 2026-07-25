FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY pkg/ ./pkg/
COPY services/mock-rails/ ./services/mock-rails/

ENV GOWORK=off
WORKDIR /app/services/mock-rails
RUN go build -o /mock-rails ./cmd/mockrails

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /mock-rails /mock-rails

ENV PORT=8090

EXPOSE 8090

ENTRYPOINT ["/mock-rails"]
