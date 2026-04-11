.PHONY: run test vet fmt docker-build compose-up compose-down loadtest smoke verify stage7-static stage7-runtime stage7-verify

run:
	go run ./cmd/gateway

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal third_party

docker-build:
	docker build -t llm-access-gateway:latest .

compose-up:
	docker compose -f deployments/docker/docker-compose.yml up -d --build

compose-down:
	docker compose -f deployments/docker/docker-compose.yml down

loadtest:
	go run ./cmd/loadtest -auth-key lag-local-dev-key

smoke:
	./scripts/gateway-smoke-check.sh

verify:
	./scripts/stage7-verify.sh runtime

stage7-static:
	./scripts/stage7-verify.sh static

stage7-runtime:
	./scripts/stage7-verify.sh runtime

stage7-verify:
	./scripts/stage7-verify.sh all
