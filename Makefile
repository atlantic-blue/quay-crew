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

# UPGRADE_BRANCH is the branch `make upgrade` is willing to build the stack from. It exists because a
# stack built from somebody's half finished branch is a stack nobody can reason about.
UPGRADE_BRANCH ?= main

# VERSION is what a built binary reports for itself: the commit it came from, marked dirty when the
# checkout has uncommitted changes, because a build from an edited tree is not that commit.
VERSION := $(shell git rev-parse --short HEAD 2>/dev/null)$(shell git diff --quiet 2>/dev/null || echo -dirty)

# The default sandbox image: a container with the Claude Code CLI, built locally with `make
# sandbox-image`. Point QC_SANDBOX_IMAGE at this and set QC_MODEL=claude-code to run real turns.
SANDBOX_IMAGE := quaycrew-sandbox-claude:local

.PHONY: up start upgrade up-observability down logs ps proto build install test features lint fmt tidy sandbox-image help

## up: start the core stack (Redpanda, OpenTelemetry collector, services)
up:
	$(COMPOSE) up --build -d

## start: alias for up
start: up

## sandbox-image: build the Claude Code sandbox image (tag quaycrew-sandbox-claude:local)
sandbox-image:
	docker build -f deploy/sandbox/claude.Dockerfile -t $(SANDBOX_IMAGE) .

## upgrade: fetch the latest, rebuild the tool and the stack, and restart it
upgrade:
	@branch="$$(git rev-parse --abbrev-ref HEAD)"; \
	if [ "$$branch" != "$(UPGRADE_BRANCH)" ]; then \
		echo "refusing: you are on $$branch, not $(UPGRADE_BRANCH). Upgrading would rebuild the stack from a branch."; \
		exit 1; \
	fi; \
	if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "refusing: this checkout has uncommitted changes, and upgrading would build them into the stack."; \
		exit 1; \
	fi; \
	before="$$(git rev-parse --short HEAD)"; \
	git fetch origin || { echo "refusing: could not reach origin, so this would build whatever you already had."; exit 1; }; \
	git merge --ff-only "origin/$(UPGRADE_BRANCH)" || { \
		echo "refusing: cannot fast forward onto origin/$(UPGRADE_BRANCH), so this is not the newest build."; \
		exit 1; \
	}; \
	after="$$(git rev-parse --short HEAD)"; \
	if [ "$$before" = "$$after" ]; then \
		echo "already on the newest build ($$after)"; \
	else \
		echo "moved from $$before to $$after"; \
	fi
	@$(MAKE) --no-print-directory install
	@echo "rebuilding and restarting the stack. Secrets are held in memory, so set the model token again afterwards."
	$(COMPOSE) up --build -d

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
	go build -ldflags "-X main.version=$(VERSION)" -o "$$dir/quay" ./cmd/quay; \
	echo "installed quay to $$dir/quay, built from $(VERSION)"; \
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
