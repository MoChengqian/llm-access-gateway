.PHONY: run test fmt docker-build compose-up compose-down

run:
	go run ./cmd/gateway

test:
	go test ./...

fmt:
	gofmt -w cmd internal third_party

docker-build:
	docker build -t llm-access-gateway:latest .

compose-up:
	docker compose -f deployments/docker/docker-compose.yml up -d --build

compose-down:
	docker compose -f deployments/docker/docker-compose.yml down
