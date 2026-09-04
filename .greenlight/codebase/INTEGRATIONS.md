# INTEGRATIONS

What Quay Krewe's control plane talks to, and how a session reaches the control plane in turn.

## Control plane to session transport

The control plane exposes one gRPC service, `quaycrew.v1.ControlPlaneService`, defined in
`proto/quaycrew/v1/controlplane.proto` and compiled into `gen/quaycrew/v1/controlplane.pb.go` and
`controlplane_grpc.pb.go` by `buf` (`buf.gen.yaml`, plugins `protoc-gen-go` and `protoc-gen-go-grpc`).
Two companion proto files carry event shapes rather than RPCs: `events.proto` (`ExecEvent`,
`SessionEvent`, the audit record) and `channel.proto` (`InboundMessage`/`OutboundMessage`, meant for a
channel such as a Telegram bot, per its own comment; the `gateway` binary that would wire a channel to
this is still a skeleton, per STACK.md).

**Server side:** `cmd/controlplane/main.go` builds a `grpc.NewServer(...)` and registers the service
with `quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)`, listening on `QC_GRPC_ADDR`
(compose sets `:50051`, published to the host as `127.0.0.1:50051` only, loopback). Every call is
gated by `internal/auth`: the client must present a bearer token
(`authorization: Bearer <token>` gRPC metadata, see `internal/auth/auth.go`), checked with a constant
time comparison (`crypto/subtle`). The token is minted on first start into
`<data directory>/system.token`, and a separate, narrower `driver.token` is minted for the one session
allowed to drive the system (the file the system refuses to let a driver session grant itself).

**Client side:** every `krewe` subcommand (`cmd/krewe/*.go`) is a gRPC client of the same service,
dialing `QC_SANDBOX_CONTROL_PLANE` (inside a sandbox) or the loopback port (from the operator's own
machine), reading the token from `system.token` on disk or from the `QC_TOKEN` environment variable
when running remotely.

**Inside a session's sandbox**, the session reaches the same gRPC endpoint over the network described
below, not over a mounted socket: `QC_SANDBOX_CONTROL_PLANE` (default `controlplane:50051`) is baked
into the container's environment at creation (`internal/sandbox/docker.go`, `runArgs`), and the
container is only ever put on the session network so the address resolves and nothing else does.

## What runs an agent session

**Provider:** Docker, by default (`internal/sandbox/docker.go`, `DockerProvider`), selected by
`QC_SANDBOX` (`docker` or `local`, see STACK.md). The control plane creates a session's container by
shelling out to the `docker` command line client (`docker run --detach --name <session id> ...`), not
through a Go Docker SDK. A container carrying the session's name is adopted rather than recreated
(`adopt`/`Existing` in `docker.go`), so a control plane that lost its own memory of a session can still
reach the same container.

**What a session's container is given at creation** (`DockerProvider.runArgs`):
- **Image:** `QC_SANDBOX_IMAGE` (default `krewe-sandbox-claude:local` when built with
  `make sandbox-image` from `deploy/sandbox/claude.Dockerfile`; compose falls back to `alpine:3.22`
  with nothing installed when unset).
- **Resource limits:** `--cpu-shares` derived from a per session processor request
  (`internal/capacity`), and `--memory`/`--memory-swap` set equal to `QC_SANDBOX_MEMORY` so a session
  cannot use swap to exceed its stated limit.
- **Network:** exactly one, decided at creation and never changed later. An ordinary session joins
  `QC_SESSION_NETWORK` (default `quaycrew_sessions`), where the control plane is the only thing
  reachable. The one driver session instead joins `QC_SANDBOX_NETWORK` when the operator set one,
  which additionally carries Postgres, Redpanda and the observability stack.
- **Mounts:** a `tmpfs` at a fixed path (`secretsMount()`) for file projected secrets, owned by the
  sandbox's own user and mode `0700`; the workspace's conversation store and the project's files
  (`Storage.Prepare`, `internal/sandbox/storage.go`, bind mounted from the host so a container can be
  replaced without losing the conversation); any extra `Mounts` configured on the provider; and, for
  the driver only, `QC_DRIVER_MOUNTS` (host paths, comma separated, `host:container[:ro]`) so it has
  something on the host to act on.
- **Environment:** every `cfg.Env` entry the control plane composes for the session (workspace secrets
  projected as `Env`, `QC_SANDBOX_CONTROL_PLANE`, the session's own token, git author identity if
  `QC_GIT_AUTHOR_NAME`/`QC_GIT_AUTHOR_EMAIL` are set).
- **Command:** `sleep infinity` — the container just stays up; the actual model runtime is started per
  exec through `docker exec -i ... <argv>` (`Exec` in `docker.go`), and `krewe attach` opens a `tmux`
  session called `AttachedSessionName` inside the container for an operator to type into.

**Presence detection** reads the container's own state rather than a stamp the system keeps fresh:
`Attached` asks `tmux list-clients` inside the container; `RuntimeRunning` reads every process's
`cmdline` out of `/proc` inside the container (`processTable` in `docker.go`) and matches the model
runtime's binary name by base name.

**`local` provider** (`internal/sandbox/local.go`) runs the same exec on the host with no container,
described in its own comment as a stopgap rather than an isolation mechanism.

## Model backend

Selected by `QC_MODEL` (`internal/model`, referenced from `cmd/controlplane/main.go`): `claude-code`
runs the real Claude Code command line tool inside the sandbox against the operator's own
subscription (no separate API key configured in this repository; the credential is whatever the
sandbox image and the operator's environment already carry), and `echo` is a fake backend used by
continuous integration and local development, which has no subscription. `QC_CLAUDE_MODEL` picks the
specific model name (default `claude-opus-5`), passed through rather than left to the tool's own
default so an upgrade of the command line tool cannot silently change which model a session runs.

## Persistent state (`internal/store`, `internal/store/migrations`)

**Backend:** Postgres 17, or an in memory store when `QC_DATABASE_URL` is unset (data lost on
restart). See STACK.md for the schema outline and the migration list. Both implementations satisfy
one interface (`internal/store/store.go`) and are proven against a shared conformance suite in
`internal/store/storetest/`.

**Connection:** `jackc/pgx/v5/pgxpool`, one pool per process (`internal/store/postgres.go`,
`pgxpool.ParseConfig` then `pgxpool.NewWithConfig`), exposed for callers that need the raw pool through
`Postgres.Pool()`. No separate query builder or ORM; hand written SQL.

**Abstracted behind an interface:** yes, `store.Store` (or equivalent name in `store.go`), so the
control plane depends on the interface and the concrete backend is chosen once at startup
(`cmd/controlplane/main.go`).

## Secrets

**Store:** `internal/secrets`, an interface (`Store`) with an in memory backend for development and a
Postgres backend (`internal/secrets/postgres.go`) for the composed stack. Values are never written
to the store in the clear: `internal/secrets/seal.go` seals every value with AES (a 32 byte key,
`crypto/aes` + `crypto/cipher`, random nonce per value, stored ahead of the ciphertext) before it
reaches Postgres, so a database dump alone does not disclose a secret.

**Sealing key:** minted on first start alongside the system token, kept on the host next to
`system.token` (per `internal/auth/auth.go`'s comment on `TokenFile`), not in the database.

**Reaching a sandbox:** a secret is projected either as an environment variable (`Projection: Env`,
the default) or as a file under the fixed `tmpfs` mount described above (`Projection: File`), decided
per secret when it is set (`SetSecretRequest.projection` in the proto), never both. Set at
`workspace` scope (that workspace's sessions only) or `system` scope (every workspace, including ones
created later).

**Never returned by the API:** `ListSecretsRequest`/`Response` carries only `SecretRef` (name,
projection, timestamps), never a value; the only reader of a value is the control plane itself, at the
moment an exec needs it (comment on `SecretRef` in `controlplane.proto`).

## Skills and hooks (capability plugins)

**Skills** (`internal/skill`, `skill/*` directories, `Skill`/`ImportSkillRequest` in the proto): a
directory (brief for the model, required binaries, named secrets, an optional setup script) imported
whole over gRPC (`ImportSkillRequest.files`, because the control plane runs in a container and a path
on the operator's machine means nothing to it), pinned to a version, attached to a workspace or to the
whole system. Shipped skills under `skills/` in this repository (`aws`, `browser`, `deploy-identity`,
`git`, `github`, `jira`, `linear`, `outbound`, `proving`, `ste`, `terraform`) are copied into the
`runtime-docker` image at `/skills` and seeded into an empty catalogue on first start
(`QC_SEED_SKILLS_DIR`).

**Hooks** (`internal/hook`, `hooks/*` directories): the same import and versioning mechanism, but a
hook is a runtime constraint the model never reads (checked when a bound event fires, for example
`PreToolUse`), not a capability the model is told about. Shipped hooks: `deploy-identity-gate`,
`merge-gate`, `process-gate`, `prompt-analyser`, `prose-gate`, `test-gate`, each its own Go module
built statically into the sandbox image at `/hooks` (`Dockerfile`, the `for dir in $(find hooks ...)`
loop).

## Observability (outbound integrations)

All configured through `internal/telemetry` and `QC_OTEL_ENDPOINT` (default `otel-collector:4317`),
authenticated implicitly by network reachability inside the compose stack (no token/API key):
- **Traces and metrics:** OTLP over gRPC to `otel-collector` (`otel/exporters/otlp/otlptrace/otlptracegrpc`,
  `otlpmetric/otlpmetricgrpc`), which fans them out to **Tempo** (traces) and **Prometheus**
  (`prometheus.yaml` scrape config) for storage, rendered in **Grafana**
  (`deploy/grafana/datasources.yaml`, anonymous admin access enabled for local development,
  `GF_AUTH_ANONYMOUS_ENABLED=true`).
- **Logs:** OTLP log export (`otlploggrpc`, `otelslog` bridge) to the same collector, which forwards to
  **Loki**. Every log line still also goes to stdout (`internal/logging`), stated in `cmd/gateway/main.go`
  as the signal that still works when the collector does not.

## Declared, unused integration

**Redpanda / Kafka** (`redpandadata/redpanda:v24.2.7` in `deploy/docker-compose.yml`, `QC_KAFKA_SEEDS`
in the compose environment): documented as "the audit export's broker", meant to carry a second copy
of every exec on a `<workspace>.execs` topic (see `ExecEvent` in `events.proto` and the compose file's
own comment). No Go code in this repository publishes to it: `twmb/franz-go` and
`twmb/franz-go/pkg/kadm` are direct dependencies in `go.mod` with no matching import anywhere under
`internal/`, `cmd/`, `hooks/` or `features/`. The system runs and pays for the broker today without
using it; the durable record of every exec is Postgres alone.

## External binaries the control plane's container needs at runtime

- **`docker`** command line client (static, copied from `docker:28-cli` into the `runtime-docker`
  image) — the whole sandbox mechanism shells out to it.
- **`/var/run/docker.sock`** — bind mounted from the host, giving the control plane's container
  root equivalent access to the host (documented and deliberate, per the compose file's comment
  pointing at `docs/ARCHITECTURE.md`).
- Inside a session's own container, at exec time: `tmux` (for `krewe attach` and presence detection),
  `git` (commits need `QC_GIT_AUTHOR_NAME`/`QC_GIT_AUTHOR_EMAIL` or every commit inside a sandbox
  fails), and whichever model runtime binary `QC_MODEL` selects.
