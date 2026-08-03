# Quay Crew. `make up` (or `make start`) brings the whole stack up in Docker.
# Pass PROJECT=<name> for a fully isolated stack, for example: make up PROJECT=demo

PROJECT ?=
COMPOSE_PROJECT := quaycrew$(if $(PROJECT),-$(PROJECT),)
COMPOSE := docker compose -p $(COMPOSE_PROJECT) -f deploy/docker-compose.yml
GOBIN := $(shell go env GOPATH)/bin

# BINDIR is where `make install` puts the quay binary. Left unset, it installs over whatever quay
# your shell already runs, and falls back to Go's bin directory when there is none, so installing
# always leaves you running the build you just made rather than an older copy earlier on your PATH.
BINDIR ?=

# The default sandbox image: a container with the Claude Code CLI, built locally with `make
# sandbox-image`. Point QC_SANDBOX_IMAGE at this and set QC_MODEL=claude-code to run real turns.
SANDBOX_IMAGE := quaycrew-sandbox-claude:local

.PHONY: up start up-observability down logs ps proto build install test features lint fmt tidy sandbox-image help

## up: start the core stack (Redpanda, OpenTelemetry collector, services)
up:
	$(COMPOSE) up --build -d

## start: alias for up
start: up

## sandbox-image: build the Claude Code sandbox image (tag quaycrew-sandbox-claude:local)
sandbox-image:
	docker build -f deploy/sandbox/claude.Dockerfile -t $(SANDBOX_IMAGE) .

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

## install: build the quay CLI from this checkout and install it over the copy your shell runs
install:
	@dir="$$(eval echo "$(BINDIR)")"; \
	if [ -z "$$dir" ]; then \
		existing="$$(command -v quay 2>/dev/null || true)"; \
		if [ -n "$$existing" ]; then dir="$$(dirname "$$existing")"; else dir="$(GOBIN)"; fi; \
	fi; \
	mkdir -p "$$dir"; \
	go build -o "$$dir/quay" ./cmd/quay; \
	echo "installed quay to $$dir/quay, built from $$(git rev-parse --short HEAD) $$(git log -1 --format=%cd --date=format:'%Y-%m-%d %H:%M')"; \
	found="$$(command -v quay 2>/dev/null || true)"; \
	if [ -z "$$found" ]; then \
		echo "note: $$dir is not on your PATH, so run $$dir/quay directly or add it"; \
	elif [ ! "$$found" -ef "$$dir/quay" ]; then \
		echo "warning: your shell still runs $$found, which is a different binary."; \
		echo "         install over that one with: make install BINDIR=$$(dirname "$$found")"; \
	fi

## test: run the tests
test:
	go test ./...

## features: run the behaviour specifications and print what the product does
features:
	go test ./features/... -v -count=1

## lint: run buf and golangci-lint (generated code is not linted)
lint:
	buf lint
	golangci-lint run ./internal/... ./cmd/... ./features/...

## fmt: format the Go sources (excluding generated code)
fmt:
	@gofmt -w $$(find . -name '*.go' -not -path './gen/*')

## tidy: tidy the module
tidy:
	go mod tidy

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
