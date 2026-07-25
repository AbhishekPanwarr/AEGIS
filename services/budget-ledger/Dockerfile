FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY pkg/ ./pkg/
COPY services/budget-ledger/ ./services/budget-ledger/

ENV GOWORK=off
WORKDIR /app/services/budget-ledger
RUN go build -o /budget-ledger ./cmd/budget

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /budget-ledger /budget-ledger

ENV PORT=8082

EXPOSE 8082

ENTRYPOINT ["/budget-ledger"]
