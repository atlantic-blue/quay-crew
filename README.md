# Quay Crew

A self hosted, open source personal agent hub. You command a crew of AI agent sessions from any
channel (a CLI, a chat app, a scheduler), they do the work in sandboxes and report back, and every
action is auditable and observable. Bring your own model. Run it on your own machine or in your own
cloud from the same build.

The name is the picture of the system: a crew you command at the quay where every channel docks.

> Status: early. The architecture and the delivery plan are set (see
> [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)); the code is being built milestone by milestone,
> tracked in issues.

## What it is

Quay Crew is a set of small services that together let you drive AI coding and assistant work from
wherever you are:

- **Channels** take input and send output: a CLI, chat apps, a scheduler.
- A durable **event log** (Kafka) carries messages between services so every component is
  independent and replaceable.
- A **control plane** routes work and manages workspaces.
- **Agent sessions** run the model and execute tools inside sandboxes.
- A **workspaceion** builds a read model from the event log that an **admin dashboard** reads.

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
make up        # bring up the whole stack (alias: make start)
```

Then open the dashboard, create a workspace, and add that workspace's channel credentials. The system
starts serving it. See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full picture.

Common targets:

```sh
make up               # start everything (Redpanda, collector, services)
make up-observability # also start Grafana, Loki, Tempo, Prometheus
make down             # stop everything
make proto            # regenerate code from the protobuf contracts
make test             # run the tests
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
- **Dashboard and workspaceion:** the read model and the admin dashboard.
- **Cloud parity:** managed backends behind the same interfaces, deployed through CI.
- **Differentiators (optional):** a reviewed learning loop, a scheduler, and voice input.

## Prior art

Quay Crew learns from OpenClaw (a self hosted gateway with files on disk), Hermes Agent (an agent
loop with a learning loop, a scheduler, and persistent memory), and remote control features that turn
a phone into a window onto a local session. The comparison and what Quay Crew borrows or rejects from
each is in the docs.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
