.PHONY: run test vet fmt docker-build compose-up compose-down observability-demo-up observability-demo-down observability-demo-config observability-demo-prepull observability-demo-verify k8s-production-render k8s-production-hpa-render k8s-production-local-check k8s-production-server-dry-run loadtest smoke verify stage7-static stage7-runtime stage7-verify otlp-check observability-demo-check sonar-quality-gate-check

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

observability-demo-up:
	docker compose -f deployments/observability/docker-compose.yml up -d

observability-demo-down:
	docker compose -f deployments/observability/docker-compose.yml down

observability-demo-config:
	docker compose -f deployments/observability/docker-compose.yml config

observability-demo-prepull:
	./scripts/observability-demo-prepull.sh

observability-demo-verify:
	./scripts/observability-demo-verify.sh

k8s-production-render:
	kubectl kustomize deployments/k8s-overlays/production

k8s-production-hpa-render:
	kubectl kustomize deployments/k8s-overlays/production-hpa

k8s-production-local-check:
	./scripts/k8s-production-cluster-check.sh local all

k8s-production-server-dry-run:
	./scripts/k8s-production-cluster-check.sh server-dry-run all

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

otlp-check:
	./scripts/otlp-export-check.sh

observability-demo-check:
	./scripts/observability-demo-check.sh

sonar-quality-gate-check:
	./scripts/sonar-quality-gate-check.rb
