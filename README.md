# Quay Krewe

A self hosted, open source personal agent hub. You command agent sessions from a command line
tool. Each session works in its own container and reports back, and every action it takes is on
the record. It is for one person who wants agents working on their own machine, on their own model
subscription, with nothing sent to a service they do not run.

The name is the picture. Every channel docks at the quay, and behind it one system holds what every
workspace shares. A krewe is the group that puts the work on, which is what the sessions are. The
command you type is `krewe`.

It is early. [`CHANGELOG.md`](CHANGELOG.md) is the list of what has landed, and
[`features/`](features/) says what the system does today, as scenarios you can run. A change that
has landed and is waiting for a release is a file of its own in
[`changelog.d/`](changelog.d/README.md), which is how two changes written at once stay out of each
other's way.

## The words

Eight resources, and everything the system holds is one of them.

**Workspaces.** The outer grouping. It holds projects, and the secrets and skills they share.

**Projects.** Inside a workspace. A repository, its context, and the sessions working in it.

**Sessions.** The workers. A container holding one conversation and its history.

**Tasks.** One thing you said to a session, and the reply. Ephemeral: a task is written when it
starts and nothing survives the process going down.

**Skills.** What the system can do. Imported, then attached to a workspace so its sessions hold them.

**Hooks.** Constraints a session runs under, checked when it acts. A hook reaches a sandbox when the
sandbox is built, so a session already running is not under a new one.

**Secrets.** Credentials a workspace holds. Values never printed, and a mounted secret reaches a
session as a file rather than through its environment.

**Context.** What the system, the workspace and the project know, as memory files. It is a resource
with levels, not a setting.

A session is deliberately not a Pod. A Pod is disposable and interchangeable with its replacement,
and a session's whole value is the history it holds.

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

The scenarios in [`features/`](features) are what this system does, written out one behaviour at a
time. [`CHANGELOG.md`](CHANGELOG.md) is what shipped and why. Anything not in one of those two does
not exist.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
