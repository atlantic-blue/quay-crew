# Quay Crew. `make up` (or `make start`) brings the whole stack up in Docker.
# Pass PROJECT=<name> for a fully isolated stack, for example: make up PROJECT=demo

PROJECT ?=
COMPOSE_PROJECT := quaycrew$(if $(PROJECT),-$(PROJECT),)
GOBIN := $(shell go env GOPATH)/bin

# QUAY_HOME is where a crew keeps what belongs to it on this machine. It is deliberately outside this
# checkout: a crew that is installed rather than cloned has no checkout to put configuration in, and
# configuration that lives in one cannot be given to anybody. Compose is told the path rather than
# left to find a file next to its own compose file.
QUAY_HOME ?= $(HOME)/.quay
ENV_FILE ?= $(QUAY_HOME)/env

COMPOSE := docker compose -p $(COMPOSE_PROJECT) --env-file $(ENV_FILE) -f deploy/docker-compose.yml

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

# SANDBOX_PATTERN matches a session's sandbox container and nothing else. The prefix alone is not
# enough: the compose project is also called quaycrew, so its own services are quaycrew-postgres-1 and
# friends, and a reap by prefix would take the whole stack with it. So this matches the exact shape of
# a sandbox name, and the reap below additionally skips anything compose owns.
SANDBOX_PATTERN := ^quaycrew-[0-9a-f]{24}$$

# The default sandbox image: a container with the Claude Code CLI, built locally with `make
# sandbox-image`. Point QC_SANDBOX_IMAGE at this and set QC_MODEL=claude-code to run real tasks.
SANDBOX_IMAGE := quaycrew-sandbox-claude:local

.PHONY: up start upgrade up-observability down drain logs ps proto build install test features lint fmt tidy sandbox-image image rebuild config home-check env-check hooks help

# print-<name> is what a variable expands to. The tests that check where configuration lives read it
# through this, so they see what make actually computes rather than a pattern matched over the text.
print-%:
	@echo "$($*)"

## config: create the crew's directory and its configuration file, if they are not there yet
#
# Compose is given the path, so the file has to exist before any compose command runs. Seeding it
# rather than refusing means a first `make up` works, and the operator edits a file that already says
# what each key is for.
#
# The data directory is made here too, and made first, because docker creates a missing bind mount
# source itself and creates it as root. That would leave the crew's own directory owned by root, and
# the next `quay use` unable to write the address you are working in into it.
config:
	@mkdir -p "$(QUAY_HOME)/data"
	@if [ ! -f "$(ENV_FILE)" ]; then \
		mkdir -p "$(dir $(ENV_FILE))"; \
		cp deploy/env.example "$(ENV_FILE)"; \
		echo "wrote $(ENV_FILE) from deploy/env.example. Edit it to say which model and image to run."; \
	fi

## home-check: refuse to start a crew whose data is still in the layout from before ~/.quay
#
# The stack mounts $(QUAY_HOME)/data. A crew made before the move has its tokens, its sealing key and
# every conversation under ~/.quaycrew/data, so starting would mount an empty directory, mint a new
# token, and look exactly like a crew that had lost everything. The tool refuses for the same reason;
# this is the same refusal on the path that does not go through the tool.
home-check:
	@if [ -d "$(HOME)/.quaycrew/data" ] && [ ! -d "$(QUAY_HOME)/data" ]; then \
		echo "refusing: this crew's data is still at $(HOME)/.quaycrew/data, and the stack now mounts"; \
		echo "          $(QUAY_HOME)/data. Starting would come up empty on a new token. Move it, once:"; \
		echo ""; \
		echo "  mkdir -p $(QUAY_HOME)"; \
		echo "  mv $(HOME)/.quaycrew/data $(QUAY_HOME)/data"; \
		exit 1; \
	fi

## up: start the core stack (Redpanda, OpenTelemetry collector, services)
up: home-check config
	$(COMPOSE) up --build -d

## start: alias for up
start: up

## rebuild: build everything this machine runs: the tool, the hooks and the sandbox image
#
# One command, because these three go together and remembering the third is not a job for a person.
# The tool is what you type, the hooks are what every session runs under, and the image is what a
# session is. Leave one behind and the crew is running a mix of two builds, which looks like a bug in
# whichever part you happen to be reading.
#
# `make upgrade` runs this after fetching. Run it directly while working on a branch, where upgrade
# refuses because it would build a half finished checkout into the stack.
rebuild: install hooks sandbox-image

## sandbox-image: build the Claude Code sandbox image (tag quaycrew-sandbox-claude:local)
sandbox-image:
	docker build --build-arg QC_VERSION=$(VERSION) -f deploy/sandbox/claude.Dockerfile -t $(SANDBOX_IMAGE) .

## image: alias for sandbox-image
image: sandbox-image

## env-check: name the configuration in deploy/env.example that your configuration file does not have
#
# An upgrade adds configuration, and nobody's configuration grows with it. Compose fills a key that is
# not there with an empty string, so the feature it turns on is simply off and nothing says why: a
# driver whose crew had no address spent an evening reporting that the control plane was refusing
# connections, while the control plane was up the whole time.
env-check:
	@if [ ! -f "$(ENV_FILE)" ]; then \
		echo "note: no $(ENV_FILE), so the stack comes up with the defaults in the compose file."; \
		echo "      run make config to write one from deploy/env.example, and keep your model and"; \
		echo "      image across upgrades."; \
		exit 0; \
	fi; \
	missing=""; \
	for key in $$(grep -oE '^[A-Z][A-Z0-9_]*=' deploy/env.example | tr -d '='); do \
		grep -qE "^$$key=" "$(ENV_FILE)" || missing="$$missing $$key"; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "note: $(ENV_FILE) does not set:$$missing"; \
		echo "      deploy/env.example gained these after your copy was made, so the stack comes up with"; \
		echo "      them empty and whatever they task on is off. Compare the two and add what you want."; \
	fi

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
	@echo "building the tool first, so the sessions are put down by the build that knows how. It is"
	@echo "built again below with everything else, which costs nothing and keeps one list of what an"
	@echo "upgrade builds."
	@$(MAKE) --no-print-directory install
	@$(MAKE) --no-print-directory drain
	@echo "rebuilding the tool, the hooks and the sandbox image. Sessions run whatever the image holds,"
	@echo "so leaving it behind means upgrading the tool and the stack while every conversation keeps"
	@echo "the build from before."
	@$(MAKE) --no-print-directory rebuild
	@$(MAKE) --no-print-directory home-check
	@$(MAKE) --no-print-directory config
	@$(MAKE) --no-print-directory env-check
	@echo "clearing whatever sandboxes are left. Draining took the ones the crew knows about; these"
	@echo "are the containers it has forgotten, and their names would block those sessions from"
	@echo "starting again."
	@docker ps -a --format '{{.Names}}|{{.Label "com.docker.compose.project"}}' \
		| awk -F'|' '$$2 == "" { print $$1 }' \
		| grep -E '$(SANDBOX_PATTERN)' \
		| xargs -r docker rm -f >/dev/null 2>&1 || true
	@echo "rebuilding and restarting the stack. Secrets are held in memory, so set the model token again afterwards."
	$(COMPOSE) up --build -d

## up-observability: retired alias for up, which now starts everything
#
# Kept because it is in fingers, in scripts and in notes. It does what it always did, and says why it
# is no longer a separate command, rather than becoming an unknown target somebody has to go and read
# the Makefile to explain.
up-observability: up
	@echo "note: up-observability is now the same as make up. Grafana, Loki, Tempo and Prometheus"
	@echo "      start with the rest of the stack, so there is no second command to remember."

## down: stop and remove everything
down: config
	$(COMPOSE) down

## drain: put every live session down, so nothing loses a task when the containers go
#
# `make upgrade` removes sandboxes by name from the daemon. A container removed that way takes the
# task in flight with it, which the operator reads as "model: run exited: exit status 137, and it said
# nothing about why" against a conversation they were watching. Going through the crew stops each
# session first, so the row says stopped and the sandbox is closed rather than ripped out.
#
# A task still working refuses this. FORCE=1 drains over it, and says whose task went.
#
# A crew that is not up has nothing to drain and does not stop the upgrade: the tool says what it
# could not reach and the sweep below still clears the containers.
drain:
	@if ! command -v quay >/dev/null 2>&1; then \
		echo "note: quay is not on your PATH, so the sessions cannot be put down cleanly."; \
		echo "      Whatever takes their containers will end any task still working."; \
		exit 0; \
	fi; \
	quay drain $(if $(FORCE),anyway,) || { \
		echo; \
		echo "refusing: a session is still working. Wait for it, or upgrade over it with:"; \
		echo "    make upgrade FORCE=1"; \
		exit 1; \
	}

## logs: follow all service logs
logs: config
	$(COMPOSE) logs -f

## ps: show running services
ps: config
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

## hooks: build the entry point of every hook this build ships
#
# A hook reaches a sandbox as files and the runtime runs one of them by path, so the entry point has
# to exist before anything reads the directory. It is a build artifact rather than something in the
# history: one committed binary runs on one processor type, and this image is built on both arm and
# amd machines.
#
# Each hook is its own module, so this loops rather than building the crew's own packages. Static,
# because the result is mounted into whatever image a session runs.
hooks:
	@for dir in $$(find hooks -maxdepth 2 -name go.mod -exec dirname {} \;); do \
		CGO_ENABLED=0 go build -C "$$dir" -o bin/hook . || exit 1; \
		echo "built $$dir/bin/hook"; \
	done

## test: run the tests
#
# The hooks are separate modules, so `go test ./...` does not reach them and they are run by name.
test: hooks
	go test ./...
	@for dir in $$(find hooks -maxdepth 2 -name go.mod -exec dirname {} \;); do \
		go test -C "$$dir" -count=1 ./... || exit 1; \
	done

## features: run the behaviour specifications and print what the product does
features: hooks
	go test ./features/... -v -count=1

## lint: run buf and golangci-lint (generated code is not linted)
lint:
	buf lint
	golangci-lint run ./internal/... ./cmd/... ./features/...
	@for dir in $$(find hooks -maxdepth 2 -name go.mod -exec dirname {} \;); do \
		(cd "$$dir" && golangci-lint run ./...) || exit 1; \
	done

## fmt: format the Go sources (excluding generated code)
fmt:
	@gofmt -w $$(find . -name '*.go' -not -path './gen/*')

## tidy: tidy the module
tidy:
	go mod tidy

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
