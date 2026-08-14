# Quay Crew

A self hosted, open source personal agent hub. You command a crew of AI agent sessions from any
channel (a CLI, a chat app, a scheduler), they do the work in sandboxes and report back, and every
action is auditable and observable. Bring your own model. Run it on your own machine or in your own
cloud from the same build.

The name is the picture of the system: a crew you command at the quay where every channel docks.

> Status: early. Sessions work end to end from the command line. Chat channels, the dashboard and the
> telemetry stack do not exist yet. [`CHANGELOG.md`](CHANGELOG.md) is the honest list of what has
> landed, and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) is where the whole plan lives.

## What works today

Everything below runs. Each one is written out as scenarios in [`features/`](features/), which you can
run with `make features`, or read from the binary itself with `quay features`.

- **A session is a conversation in its own container.** It starts on the first turn, is reused for
  every turn after it, and runs the Claude Code command line tool on your subscription, so a turn costs
  no API credit.
- **You address the crew by path.** `quay use me/house-bills`, then `quay dispatch "..."`. Creating a
  workspace or a project moves you into it. An address typed on a command applies to that command only,
  and a thread is the third level, so standing in one continues that conversation.
- **A conversation survives its container.** The model's own store and the project's files are mounted
  in from the host, so replacing a sandbox does not destroy the conversation.
- **Each level carries context.** A workspace and a project own a directory the sandbox mounts, and the
  model reads `CLAUDE.md` from both. Giving a project context is writing a file into it.
- **Workspaces and sessions survive a restart**, in Postgres, so the thread you were in yesterday is
  still there.
- **You can get inside the conversation.** `quay attach <session>` puts you in it with its history;
  shelling in with `s` shows you the room instead.
- **`quay` with no arguments opens a console**, in the shape of k9s: `:` to switch resource, `/` to
  filter, enter to drill in, `s` to shell in, `x` to stop a session, `?` for every key. It opens with
  a status block naming the build, the crew you are connected to, where you are standing and what a
  turn there would run in, so you can see which one you are about to act on.
- **Secrets are per workspace**, held by a secrets backend and injected into the session's sandbox. The
  event log records a reference, never a value.

## What it is

Quay Crew is a set of small services that together let you drive AI coding and assistant work from
wherever you are:

- **Channels** take input and send output: a CLI, chat apps, a scheduler.
- A durable **event log** (Kafka) carries messages between services so every component is
  independent and replaceable.
- A **control plane** routes work and manages workspaces.
- **Agent sessions** run the model and execute tools inside sandboxes.
- Turn history is written to the store in the same breath as each turn; the event log is an optional audit **export** for a second consumer, such as an **admin dashboard**.

It ships with no data of any kind. You create **workspaces** at runtime through the dashboard or the
control plane API, and each workspace is isolated from the others.

## Principles

- **Open source and self hosted.** Everything runs on infrastructure you control.
- **No baked in data or secrets.** The code has no knowledge of any workspace. Workspaces and their
  credentials are created at runtime and stored outside the repository.
- **Independent components.** Services talk over an event log and typed APIs, never by reaching into
  each other, so any one can be deployed, scaled, or replaced on its own.
- **Fully auditable and observable.** Structured logs and an audit stream, distributed traces, and
  metrics (including token spend and cost, plus CPU, memory, and GPU) through OpenTelemetry.
- **Reviewed changes only.** An agent can propose a new skill or memory, but nothing self applies.
- **Bring your own model.** Point it at the provider you use.

## Stack

- **Go**, one monorepo, a service per component.
- **Kafka** as the async backbone, run locally with **Redpanda** (Kafka compatible, lightweight), and
  managed Kafka or Redpanda in the cloud. Client: `franz-go`.
- **gRPC** and **protobuf** for synchronous APIs and shared contracts.
- **OpenTelemetry** into **Grafana**, **Loki**, **Tempo**, and **Prometheus**.
- **Docker** for every component, wired by a compose file. The same images deploy to the cloud.

## Quick start

```sh
make config                                                   # say which model and image to run, in ~/.quay/env
make sandbox-image                                            # the image a session runs in
make up                                                       # bring the stack up
make install                                                  # build quay and install it over the copy you run

quay workspace create me
quay project create house-bills
quay secret set CLAUDE_CODE_OAUTH_TOKEN <from `claude setup-token`>
quay dispatch "say pong"
```

The stack reads `~/.quay/env` on every command, so the model and image you chose survive a restart
and an upgrade. It sits outside this checkout, because a crew that was installed rather than cloned has no
checkout to keep configuration in. Creating something moves you into it, so nothing above says where twice, and `quay use` tells
you where you are. [`docs/SANDBOX.md`](docs/SANDBOX.md) has the long version, including what runs without a
subscription. See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full picture.

Two of the services in that stack are worth knowing on their own.
[`docs/DATABASE.md`](docs/DATABASE.md) is Postgres: why a session survives a restart, how to shell in
with psql, and what each table means. [`docs/EVENTS.md`](docs/EVENTS.md) is the event log: what it is
for, how to inspect it with `rpk`, and why it is empty today.
[`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md) is the third: which signals are real, which are
wired but carry nothing, and what to read when something goes wrong.

Common targets:

```sh
make up               # start everything (Redpanda, collector, services)
make up-observability # also start Grafana, Loki, Tempo, Prometheus
make down             # stop everything
make proto            # regenerate code from the protobuf contracts
make install          # build quay from this checkout, over whatever quay your shell runs
make upgrade          # fetch the latest, rebuild the tool and the stack, restart it
make test             # run the tests
make features         # print what the product does, scenario by scenario
make lint             # run the linters
```

## Workspaces and isolation

A workspace is the unit of isolation, created at runtime. Everything is namespaced by workspace id: the
event log topics, the consumer groups, the stored state, and the agent workspace. Workspaces can share
one running stack (logical isolation) or run as fully separate stacks when you want hard isolation.

## Secrets

Secrets are never stored in the repository. You set a workspace's credentials through the dashboard or
the API; they go straight to a pluggable secrets backend (an encrypted local store for development,
a managed secrets service in the cloud). The event log records only a reference, never the value, and
logs redact them.

## Roadmap

Built spine first, so a usable thing exists early and the rest widens it. Full detail per slice is in
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md); each slice is an issue.

- **Spine:** channel contract, event log, control plane with the sessions controller, thread engine,
  and a CLI channel end to end.
- **First remote channel:** a chat channel, inbound and gated outbound.
- **Controllers, sessions, sandbox:** the rest of the controllers, parallel sessions, a durable
  session store, and sandbox tiers with permission tiers.
- **Dashboard:** the admin dashboard, reading the export.
- **Cloud parity:** managed backends behind the same interfaces, deployed through CI.
- **Differentiators (optional):** a reviewed learning loop, a scheduler, and voice input.

## Prior art

Quay Crew learns from OpenClaw (a self hosted gateway with files on disk), Hermes Agent (an agent
loop with a learning loop, a scheduler, and persistent memory), and remote control features that turn
a phone into a window onto a local session. The comparison and what Quay Crew borrows or rejects from
each is in the docs.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
