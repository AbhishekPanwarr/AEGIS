FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY pkg/ ./pkg/
COPY services/containment-controller/ ./services/containment-controller/

ENV GOWORK=off
WORKDIR /app/services/containment-controller
RUN go build -o /containment-controller ./cmd/containment

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /containment-controller /containment-controller

ENV PORT=8083

EXPOSE 8083

ENTRYPOINT ["/containment-controller"]
