.PHONY: run test fmt

run:
	go run ./cmd/gateway

test:
	go test ./...

fmt:
	gofmt -w cmd internal third_party
