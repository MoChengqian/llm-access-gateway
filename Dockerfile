FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY configs ./configs
COPY internal ./internal
COPY migrations ./migrations
COPY third_party ./third_party

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/devinit ./cmd/devinit

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /out/gateway /app/gateway
COPY --from=builder /out/devinit /app/devinit
COPY configs /app/configs

EXPOSE 8080

CMD ["/app/gateway"]
