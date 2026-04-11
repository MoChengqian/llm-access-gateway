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

RUN addgroup -S gateway && adduser -S -G gateway -H -D gateway

COPY --from=builder --chown=gateway:gateway /out/gateway /app/gateway
COPY --from=builder --chown=gateway:gateway /out/devinit /app/devinit
COPY --chown=gateway:gateway configs /app/configs

EXPOSE 8080

USER gateway

CMD ["/app/gateway"]
