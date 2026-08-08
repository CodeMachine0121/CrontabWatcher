.PHONY: start build test vet check docker-build docker-up docker-down docker-logs mocks

BINARY := bin/cronwatch
PACKAGE := ./cmd/cronwatch

start:
	go run $(PACKAGE) serve

build:
	go build -o $(BINARY) $(PACKAGE)

test:
	go test ./...

vet:
	go vet ./...

# commit 前跑這個（沒有 pre-commit hook，靠人工）
check: build vet test

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# 需先安裝 mockery CLI，設定讀 .mockery.yaml
mocks:
	mockery
