FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
COPY cmd ./cmd
COPY configs ./configs
COPY internal ./internal
COPY migrations ./migrations
COPY third_party ./third_party

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/devinit ./cmd/devinit

FROM alpine:3.22

WORKDIR /app

RUN addgroup -S gateway && \
    adduser -S -G gateway -H -D gateway && \
    mkdir -p /app/configs && \
    chmod 0555 /app /app/configs

COPY --from=builder --chmod=0555 /out/gateway /app/gateway
COPY --from=builder --chmod=0555 /out/devinit /app/devinit
COPY --chmod=0444 configs/config.yaml /app/configs/config.yaml

EXPOSE 8080

USER gateway

CMD ["/app/gateway"]
