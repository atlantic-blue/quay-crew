# Structure

Go 1.25 module `github.com/atlantic-blue/quay-krewe`. Buf generated protobuf/gRPC code, one
gRPC service, Docker sandboxed sessions, Postgres store, BDD scenarios as the acceptance layer.

## Top level tree

```
.
├── cmd/            entry points, one directory per binary
│   ├── controlplane/  the gRPC server (spine service)
│   ├── krewe/          the CLI and terminal console (what an operator types)
│   ├── gateway/        service skeleton, channels/event log wiring lands here later
│   ├── promises/       repo tool: refuses a behavioural diff with no features/ scenario
│   └── quay/            entry point for the sandbox image's own tooling
├── internal/        everything not meant to be imported outside this module, one package per concern
├── proto/quaycrew/v1/  the API contract (source of truth: events, controlplane, channel .proto files)
├── gen/quaycrew/v1/    buf generated Go from proto/ (never hand edited)
├── features/         Gherkin scenarios (*.feature) + Go step definitions (*_steps_test.go), the
│                      acceptance/BDD layer; "anything not in here does not exist" (README.md:67)
├── hooks/           first party hooks, one directory per hook, each its own Go module
├── skills/           first party skills, one directory per skill (SKILL.md + skill.yaml, no code)
├── deploy/           docker-compose, observability config (Grafana/Loki/Prometheus/Tempo/otel), and
│                      a large set of *_test.go files that test the deployment itself
├── docs/ (implied by references in code comments; not present in this worktree's tree above)
├── Dockerfile        the sandbox image sessions run in
├── Makefile          `make install`, `make test`, `make features`, `make sandbox-image`, etc.
└── buf.yaml / buf.gen.yaml   protobuf generation config
```

## `internal/` packages (grouping pattern)

Grouped by concern/domain, not by technical layer. Each package's doc comment states why it exists as
its own package (a recurring convention in this codebase: see `internal/session/session.go:1-13` for
an explicit "why this boundary" comment). The full list:

| Package | What it holds |
|---|---|
| `controlplane` | the gRPC server implementation: `server.go` (2,327 lines, the RPC handlers) plus one file per cross cutting concern split out of it (`capability.go`, `hooks_render.go`, `dispatchwaits.go`, `describe.go`, `secretfiles.go`, `signing.go`, `presence.go`, `seed.go`, `seed_hooks.go`, `stopexec.go`, `sessionevents.go`, `events.go`, `health.go`, `work.go`, `locate.go`, `deny.go`, `lifecycle.go`, `conversation.go`) |
| `sandbox` | the `Sandbox`/`Provider` interfaces and the Docker, local and in memory implementations; `Storage` (host directory layout, section 3/4 of ARCHITECTURE.md); mount/spec types; usage and context spend accounting |
| `store` | the `Store` interface, its Postgres and in memory implementations, and `migrations/` (61 numbered SQL migration pairs, `.up.sql`/`.down.sql`) |
| `model` | the `Runner` interface and the Claude Code CLI adapter (`claudecode.go`), an echo/fake runner, permission mode types, redaction of secrets from error text |
| `skill` | reads a skill's directory (`skill.yaml` + `SKILL.md` + optional `bin/setup`) into a `Skill` struct; shared by first party and imported skills |
| `hook` | the hook equivalent of `skill`: reads a hook's directory, renders the settings file that binds hook events to entry points |
| `workspace` | address parsing (`workspace/project/session`, `path.go`), resolving an address against the control plane (`resolve.go`), and reading what the operator typed as a session (`session.go`) — this is CLI side, not the store's `Store` interface |
| `session` | one small package: a single derived fact (`LastMoved`) shared between the store's sort order and the display layer, cut out specifically to avoid a store-to-display dependency (see the package doc) |
| `console` | the terminal UI: screens, the command bar, the project/session catalogue tree, panels |
| `repository` | repository address parsing/validation (`owner/name`), shared between a project's repository field and a former job's (see ARCHITECTURE.md section 5) |
| `deploy` | `DeployTarget` parsing/validation (account, region, identity) |
| `auth` | token env var names and auth plumbing for a driver session reaching the control plane |
| `secrets` | the secrets backend interface and projection (`env` vs `file`) |
| `name` | shared naming rules (e.g. the retired "crew" word, `RefuseRetired`) |
| `display` | short id formatting, identifier heuristics (`LooksLikeIdentifier`) used by the CLI |
| `telemetry` | OpenTelemetry wiring: traces, metrics, `Traceparent` propagation into a sandbox's env |
| `logging` | structured logging setup |
| `capacity` | CPU/processor unit conversions for sandbox resource limits |
| `contextsize` / `contextspend` | measuring how full a conversation's context window is, and where its characters went (`ContextSpend`, `ContextWindow` on `Session`) |
| `statusline` | renders the CLI status line |
| `panel` | a display primitive shared by console screens |
| `manual` | the operator manual / help text surface |
| `origin` | where a request originated (channel vs CLI vs driver), used for auth/attribution |
| `promise` | the model shared by `cmd/promises` (the "every behavioural diff needs a scenario" check) |

## Naming conventions

- **Packages**: single lower case word, by domain (`sandbox`, `skill`, `hook`, `workspace`), never
  `utils` or `helpers` or `common`.
- **Functions**: verbs describing behaviour, often in full sentences as doc comments rather than
  compressed names — e.g. `capabilityOf`, `withoutUnusable`, `renderTo`, `startSandbox`,
  `notASessionOnItsOwn`. Doc comments are prose, frequently explaining *why* a shape was chosen and
  citing the defect that motivated it (a strong, consistent house style across the whole codebase; see
  almost any file above).
- **Files**: one file per cohesive concern inside a package, named after that concern
  (`hooks_render.go`, `dispatchwaits.go`, `capability.go`), not after a type.
- **Types**: exported Go convention, `PascalCase` for exported (`Sandbox`, `Provider`, `Storage`),
  `camelCase` for unexported (`capability`, `contextLevel`, `dockerSandbox`).
- **Proto/generated names**: `PascalCase` messages and fields become `GetFieldName()` accessors on the
  Go side (`session.GetWorkspace()`, `req.GetProject()`); handler code consistently uses the getters
  rather than direct field access, which makes a nil pointer on an unset message safe to call through.
- **Migrations**: `NNNN_description.up.sql` / `.down.sql`, sequential, every one reversible; several
  carry long prose comments explaining the reasoning for the schema change (the same "why" convention
  as the Go doc comments).
- **Environment variables the sandbox reads**: `QC_` prefix for the system's own configuration
  (`QC_SESSION_ID`, `QC_GRPC_ADDR`), reserved so a workspace secret cannot pose as one
  (`internal/controlplane/server.go:1893`, `systemOwnPrefix`).

## Import pattern

All internal imports are absolute, rooted at the module path
(`github.com/atlantic-blue/quay-krewe/internal/...`), never relative. Generated protobuf code is
imported as `quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"` throughout, with that
alias used consistently across every file that touches the wire types.

## Entry points

- `cmd/controlplane/main.go` — the gRPC server; wires the store, sandbox provider and model runner from
  configuration/environment and serves `ControlPlaneService`.
- `cmd/krewe/main.go` — the CLI; each subcommand is its own file (`exec.go`, `attach.go`, `stop.go`,
  `context.go`, `skills.go`, `hooks.go`, `drain.go`, `label.go`, `mode.go`, `target.go`, `place.go`,
  `delete.go`, `read.go`, `where.go`, `panel.go` for the console, etc.), dispatched from `main.go`. With
  no arguments it opens the console (`README.md:62`).
- `cmd/gateway/main.go` — service skeleton for channel/event log wiring; not yet load bearing.
- `cmd/promises/main.go` — repository local CI tool, not part of the deployed system.
- `Dockerfile` / `make sandbox-image` — builds the image every session's container runs, stamped with a
  build label (`sandbox.BuildLabel`) read back by `GetInfo`.

## Config file locations

- `deploy/env.example` — the environment variables an operator sets (`make config` copies this to a
  real `.env`).
- `deploy/docker-compose.yml` — the whole stack: control plane, Postgres, the observability sidecars
  (Grafana, Loki, Prometheus, Tempo, otel collector), the session network.
- `buf.yaml` / `buf.gen.yaml` — protobuf lint/generation config.
- `.golangci.yml` — linter config.
- Per skill/hook `skill.yaml` / `hook.yaml` — the manifest read by `internal/skill` / `internal/hook`
  at import time.

## Where tests live relative to source

Two tiers, both inside the module, neither in a separate `test/` tree:

1. **Go unit/integration tests**, `*_test.go`, co-located in the same package/directory as the code
   they test (standard Go convention: `internal/controlplane/server_test.go` beside `server.go`,
   `internal/sandbox/docker_test.go` beside `docker.go`, and so on). `deploy/*_test.go` tests the
   deployed stack itself (compose config, browser smoke tests, signing, upgrade behaviour).
2. **BDD/acceptance scenarios**, top level `features/`: one `<name>.feature` (Gherkin) paired with one
   `<name>_steps_test.go` (the Go step definitions) per behaviour area (`dispatching`, `context`,
   `skills`, `hooks`, `sessions`, `workspaces`, `projects`, `mergegate`, `processgate`,
   `deployidentity`, and about 45 more). `cmd/promises` enforces that a behavioural diff carries a
   matching scenario here. `features/suite_test.go` and `features/catalog.go` wire the runner.

Per the hard constraint on this survey: none of `go test`, `make test`, or `make features` were run to
produce this document; everything above is read from source, not from a passing suite.
