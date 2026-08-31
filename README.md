# Quay System

A self hosted, open source personal agent hub. You command agent sessions from a command line
tool. Each session works in its own container and reports back, and every action it takes is on
the record. It is for one person who wants agents working on their own machine, on their own model
subscription, with nothing sent to a service they do not run.

The name is the picture. Every channel docks at the quay, and behind it one system holds what every
workspace shares.

It is early. [`CHANGELOG.md`](CHANGELOG.md) is the list of what has landed, and
[`features/`](features/) says what the system does today, as scenarios you can run. A change that
has landed and is waiting for a release is a file of its own in
[`changelog.d/`](changelog.d/README.md), which is how two changes written at once stay out of each
other's way.

## The words

Eleven resources, and everything the system holds is one of them.

**Workspaces.** The outer grouping. It holds projects, and the secrets, roles and skills they share.

**Projects.** Inside a workspace. A repository, its context, and the sessions working in it.

**Jobs.** What you want. A row the system keeps, so the intent outlives the terminal that asked for
it. A controller runs it. Close the laptop and it carries on.

**Sessions.** The workers. A container holding one conversation and its history.

**Tasks.** One thing you said to a session, and the reply. Ephemeral: a task is written when it
starts and nothing survives the process going down.

**Flows.** An automation graph the system runs: dispatch, choice, ask, wait and trigger nodes, joined
by edges.

**Roles.** A named way of working: a brief, the model it runs on, and the material it may receive.

**Skills.** What the system can do. Imported, then attached to a workspace so its sessions hold them.

**Hooks.** Constraints a session runs under, checked when it acts. A hook reaches a sandbox when the
sandbox is built, so a session already running is not under a new one.

**Secrets.** Credentials a workspace holds. Values never printed, and a mounted secret reaches a
session as a file rather than through its environment.

**Context.** What the system, the workspace and the project know, as memory files. It is a resource
with levels, not a setting.

Beside them are the **limits**, which are settings rather than resources: per workspace, how deep the
job tree may go, how many run at once, what a tree may spend, and how long an unused session is kept.

A job is a Kubernetes Job: declared intent, run to completion, watched by a controller, with a
disposable container underneath. A session is deliberately not a Pod. A Pod is disposable and
interchangeable with its replacement, and a session's whole value is the history it holds.

## Quick start

You need Docker and a Claude subscription.

```sh
make install
```

That is the whole first run, and running it again is safe. It writes the configuration, builds the
tool, the hooks and the sandbox image, and starts the system.

It cannot mint your model credential. So it ends by printing these four commands, which you type:

```sh
krewe workspace create me
krewe project create house-bills
krewe secret set CLAUDE_CODE_OAUTH_TOKEN <from `claude setup-token`>
krewe task "say pong"
```

Then run `krewe` with no arguments to open the console. `make help` lists every other target.

## Where to read next

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): the whole picture, the stack, the principles and
  the plan.
- [`docs/ORCHESTRATION.md`](docs/ORCHESTRATION.md): a job as a record the system keeps, the controller,
  the lease and the capability model.
- [`docs/TASKS.md`](docs/TASKS.md): one task from dispatch to the records it leaves, and the words
  that get used for each other.
- [`docs/WORKSPACE.md`](docs/WORKSPACE.md): one workspace from nothing to working.
- [`docs/SANDBOX.md`](docs/SANDBOX.md): the sandbox, and what runs without a subscription.
- [`docs/ROLES.md`](docs/ROLES.md) and [`docs/SKILLS.md`](docs/SKILLS.md): what a role is, what a
  skill is, and how each one reaches a session.
- [`docs/DATABASE.md`](docs/DATABASE.md), [`docs/EVENTS.md`](docs/EVENTS.md) and
  [`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md): the store, the event log and the signals.
- [`docs/CONTEXT-SPEND.md`](docs/CONTEXT-SPEND.md): where a session's context actually goes, measured,
  and the run to repeat it. Read it before proposing a change to how a session reads code.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
