.PHONY: help run build test fmt docker-build migrate

GOCACHE ?= /tmp/go-build
GOMODCACHE ?= /tmp/go-mod
IMAGE_NAME ?= ghcr.io/justarandombaddev/admin-portal
IMAGE_TAG ?= dev

help:
	@echo "run          Run admin panel locally"
	@echo "build        Build Go binaries"
	@echo "migrate      Apply embedded admin database migrations"
	@echo "test         Run Go tests"
	@echo "fmt          Format Go files"
	@echo "docker-build Build Docker image"

run:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/admin-panel

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o bin/admin-panel ./cmd/admin-panel
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o bin/adminctl ./cmd/adminctl

migrate:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/adminctl migrate

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

fmt:
	gofmt -w ./cmd ./internal

docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .
