# Quay Crew. `make up` (or `make start`) brings the whole stack up in Docker.
# Pass PROJECT=<name> for a fully isolated stack, for example: make up PROJECT=demo

PROJECT ?=
COMPOSE_PROJECT := quaycrew$(if $(PROJECT),-$(PROJECT),)
COMPOSE := docker compose -p $(COMPOSE_PROJECT) -f deploy/docker-compose.yml
GOBIN := $(shell go env GOPATH)/bin

.PHONY: up start up-observability down logs ps proto build test lint fmt tidy help

## up: start the core stack (Redpanda, OpenTelemetry collector, services)
up:
	$(COMPOSE) up --build -d

## start: alias for up
start: up

## up-observability: also start Grafana, Loki, Tempo, Prometheus
up-observability:
	$(COMPOSE) --profile observability up --build -d

## down: stop and remove everything
down:
	$(COMPOSE) --profile observability down

## logs: follow all service logs
logs:
	$(COMPOSE) logs -f

## ps: show running services
ps:
	$(COMPOSE) ps

## proto: regenerate Go code from the protobuf contracts
proto:
	PATH="$(PATH):$(GOBIN)" buf generate

## build: build all Go packages
build:
	go build ./...

## test: run the tests
test:
	go test ./...

## lint: run buf and golangci-lint (generated code is not linted)
lint:
	buf lint
	golangci-lint run ./internal/... ./cmd/...

## fmt: format the Go sources (excluding generated code)
fmt:
	@gofmt -w $$(find . -name '*.go' -not -path './gen/*')

## tidy: tidy the module
tidy:
	go mod tidy

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
