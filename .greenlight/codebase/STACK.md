# STACK

Quay Krewe is a self hosted agent hub: a control plane that runs agent sessions in sandboxes, a
terminal console, and a set of command line tools. Written in Go, deployed as a small fleet of
containers through Docker Compose.

## Language and runtime

**Language:** Go 1.25.0 (from `go.mod` line 3).
**Module:** `github.com/atlantic-blue/quay-krewe`.
**Build:** multi stage `Dockerfile`, `golang:1.25` build stage, `CGO_ENABLED=0 GOOS=linux go build`,
static binaries. Runtime images are `gcr.io/distroless/static-debian12:nonroot` (most services) or
the same base run as root (`runtime-docker`, the control plane, because it needs the Docker socket).

## Services (binaries)

| Binary | Path | Role |
|---|---|---|
| `controlplane` | `cmd/controlplane` | The spine gRPC service: workspaces, projects, sessions, skills, hooks, secrets |
| `gateway` | `cmd/gateway` | Service skeleton only today (telemetry + logging, waits for shutdown); channel wiring not yet built, per its own doc comment |
| `krewe` | `cmd/krewe` | The operator's command line client and Bubble Tea console |
| `quay` | `cmd/quay` | A second command line entry point (see `cmd/quay/main.go`) |
| `promises` | `cmd/promises` | Repository tool, not shipped to operators: reads a pull request diff and refuses a behaviour change that carries no `features/` scenario |

## Frameworks and key libraries (from `go.mod`)

**Terminal UI:** `charmbracelet/bubbletea` v1.3.10, `charmbracelet/lipgloss` v1.1.0,
`charmbracelet/x/ansi` v0.10.1, `charmbracelet/x/term` v0.2.1 - the `krewe` console (`internal/console`,
`internal/panel`, `internal/statusline`) is a Bubble Tea terminal application.

**RPC:** `google.golang.org/grpc` v1.82.0, `google.golang.org/protobuf` v1.36.11. Service contracts are
authored in `proto/quaycrew/v1/*.proto` and compiled with `buf` (`buf.yaml`, `buf.gen.yaml`) into
`gen/quaycrew/v1/*.pb.go`, using `protoc-gen-go` and `protoc-gen-go-grpc`. `make proto` runs it (see
Makefile `.PHONY` list).

**Database driver:** `jackc/pgx/v5` v5.10.0, used through `pgxpool` (`internal/store/postgres.go`). No
ORM; queries are written by hand against `pgxpool.Pool`.

**Observability:** the OpenTelemetry Go SDK, pinned to a matched set: `go.opentelemetry.io/otel`
v1.44.0, `otel/sdk` v1.44.0, `otel/trace` v1.44.0, `otel/metric` v1.44.0, `otel/log` v0.14.0 (logs are
still pre 1.0), plus OTLP gRPC exporters for traces, metrics and logs
(`otlptracegrpc`, `otlpmetricgrpc`, `otlploggrpc`), the `otelgrpc` gRPC instrumentation and
`otelslog` bridge. Wired in `internal/telemetry`.

**Testing:** `cucumber/godog` v0.16.0 (Gherkin/BDD) for the `features/` suite, plus the standard
library `testing` package for unit and integration tests, and `testcontainers-go` v0.43.0 with the
`postgres` and `redpanda` modules for integration tests that need a real database or broker.
`stretchr/testify` is present only as an indirect dependency of testcontainers.

**Other:** `google/uuid` v1.6.0 (identifiers), `mattn/go-isatty` (terminal detection), `gopkg.in/yaml.v3`
(skill/hook manifests, `skill.yaml`).

**Declared but not imported anywhere in `.go` source:** `twmb/franz-go` v1.21.5 and
`twmb/franz-go/pkg/kadm` v1.18.0 (Kafka client), and correspondingly
`testcontainers-go/modules/redpanda`. `go.mod` lists them as direct (non indirect) requires, but a
repository wide search of every `.go` file under `internal/`, `cmd/`, `hooks/`, `features/` and `deploy/`
finds no import of `kgo` or `kadm`. Redpanda itself runs in `deploy/docker-compose.yml` as "the audit
export's broker" and the proto comments describe an export ("also puts every exec on
`<workspace>.execs`"), but the code that would publish to it does not exist yet. Flagged here rather
than in CONCERNS.md because it is a stack fact (a dependency with no call site) rather than a defect.

## Database

**Postgres 17** (`postgres:17-alpine` in `deploy/docker-compose.yml`). Connected through
`jackc/pgx/v5/pgxpool`, config from `QC_DATABASE_URL`. Unset falls back to an in memory store
(`internal/store/memory.go`) that loses all state on restart, and `GetInfo` reports which one is live
(`store` field in `GetInfoResponse`, `proto/quaycrew/v1/controlplane.proto` lines 858 to 861).

**Migrations:** plain SQL files under `internal/store/migrations/`, driven by `internal/store/migrate.go`.
61 migration pairs as of this snapshot (`0001_init` through `0061_an_exec_is_called_an_exec`), each a
`.up.sql`/`.down.sql` pair, no migration framework or library, a hand rolled runner, forward and
reversible.

**Schema, in outline** (see the migrations for the authoritative field list):
- `projects` - renamed to `workspaces` in migration 0002, then `projects` reappears in 0003 as a new,
  narrower concept (a body of work inside a workspace). Soft deleted through `deleted_at`.
- `channels` - one row per (project, channel id) pair.
- `sessions` - one row per agent conversation: status, `model_session_id` (the handle back into the
  model's own conversation store), archival, permission mode, lifecycle timestamps. Migrations 0004,
  0005, 0008, 0026, 0032, 0039, 0050 extend this table (archiving, permission mode, driver sessions,
  session events, lifecycle, titles).
- `secrets` (0007) - sealed workspace and system credentials; see INTEGRATIONS.md.
- `turns` (0007) - per exec transcript accounting.
- `skills` (0009), extended through 0012, 0019, 0025 - imported capability manifests, pinned by
  version, plus which secrets and binaries they need.
- `hooks` (0021) - imported constraint manifests bound to runtime events.
- `flows`, `flow_limits`, `flow_due`, `flow_question`, `flow_schedules` (0014 to 0018) and later
  `work`/`job` tables (0028 onward, through 0061) - a scheduling and work subsystem that migration 0060
  ("remove jobs, flows and roles") mostly retired; the newest migrations (`0058_executions`,
  `0059_a_job_is_not_under_a_job`, `0061_an_exec_is_called_an_exec`) show the vocabulary settling on
  "exec" as the unit of work, replacing "job".
- `roles`, `role_verbs`, `role_may`, `role_origin` (0024, 0038, 0041, 0042) - a role and capability
  model, also retired by 0060.

The migration history is a readable log of the domain vocabulary changing under the system (projects to
workspaces, roles removed, jobs renamed to execs); read it in order rather than assuming the current
name for a concept matches an older migration's filename.

**Two store implementations, one contract:** `internal/store/postgres.go` and
`internal/store/memory.go` both satisfy `internal/store/store.go`'s interface, and both are run against
the same conformance suite in `internal/store/storetest/`, so a behaviour proven against one is proven
against the other (per the package doc comment in `store.go`).

## Sandbox execution

Two `sandbox.Provider` implementations, selected by `QC_SANDBOX` (`internal/sandbox/new.go`):
- **`docker`** (default) - `internal/sandbox/docker.go`. Each session gets its own long lived
  container, created with `docker run --detach`, execs run with `docker exec`. The control plane talks
  to the host's Docker daemon by shelling out to the `docker` command line client (not the Docker API
  client library), which is why the `runtime-docker` image stage installs the static `docker` binary
  from `docker:28-cli` and mounts `/var/run/docker.sock`.
- **`local`** - `internal/sandbox/local.go`. Runs directly on the host with no isolation, documented
  in its own package comment as "a stopgap, not a sandbox."

## Build and task running

**`Makefile`** is the primary task runner: `make up`/`make start` (compose up), `make install`
(first run: writes config, builds, brings the stack up), `make upgrade` (fetch, rebuild, drain, restart,
refuses on a dirty tree or a non `main` branch), `make rebuild` (tool + hooks + sandbox image),
`make sandbox-image`, `make proto` (buf generate), `make lint`, `make fmt`, `make tidy`, `make tool`
(build `krewe` and install it), `make drain`, `make env-check`. No separate bundler or frontend build;
the console is a terminal application compiled into the `krewe` binary.

**Linting:** `golangci-lint`, configured in `.golangci.yml` (`version: "2"`, `linters: default: standard`,
`gofmt` formatter enabled, `errcheck` excludes `fmt.Fprint*`).

## Testing setup

Three tiers, all Go, no separate coverage tool configured beyond `go test -cover`:
- Unit tests: `_test.go` files beside the code across every package.
- Integration tests: `_integration_test.go` files, several using `testcontainers-go` to start a real
  Postgres for `internal/store/postgres_integration_test.go` and its neighbours.
- BDD and acceptance: `features/*.feature` (Gherkin) with matching `*_steps_test.go`, run through
  `cucumber/godog`, driven by `features/suite_test.go`. `make features` runs this tier; `make promises`
  is a separate, purpose built gate that fails a pull request whose diff changes behaviour without
  adding or touching a feature file.
- Note for this session: `go test`, `make test` and `make features` were not run (excluded by the
  operator's constraint for this task); the above is read from source, not from a passing run.

## Deployment

**Platform:** Docker Compose (`deploy/docker-compose.yml`), self hosted, no cloud specific
infrastructure code found in this repository (no Terraform or CDK). `make up` brings up: `redpanda`,
`postgres`, `otel-collector`, `gateway`, `controlplane`, `grafana`, `loki`, `tempo`, `prometheus`.
`PROJECT=<name>` gives a fully isolated second stack (its own compose project name and session
network).

**Versioning:** the running build is named after the short revision id
(`git rev-parse --short HEAD`, marked `-dirty` on an uncommitted tree), stamped into binaries with
`-ldflags "-X main.version=..."` and into the sandbox image with the `com.quaycrew.build` Docker
label, so `GetInfo` can report a drift between the tool, the control plane and the sandbox image
build.

**Configuration:** `deploy/env.example` is the template written to `~/.krewe/env` (or `$KREWE_HOME/env`)
by `make config`/`make install`, read by Compose with `--env-file`. Every `QC_*` variable is
documented in place there; see INTEGRATIONS.md for the ones that name an external dependency.
