# The event log

Quay System runs a Kafka compatible event log, served locally by Redpanda, which starts with the rest
of the stack. It is an audit export, not the road anything travels by: history lives in Postgres,
written in the same breath as each task, and a system with no broker loses nothing but the export.

Read the next section before you go looking for messages in it. The path one task takes, from the
moment it is dispatched to the records it leaves in the store and on the log, is in
[`TASKS.md`](TASKS.md).

## What state it is in today

**Every task is written to the store synchronously.** The dispatch path writes the redacted task
into the `tasks` table in the same breath as the task itself, on a context detached from the
request's, so a client hanging up cannot lose the record and a broker being down cannot either.
`quay task list <session>` and `l` on a session in the console read that table.

**When an export is configured, the control plane also publishes each task to the log**, on
`<workspace>.tasks`, keyed by session so one session's events stay in order on one partition. A
task that failed is exported too, because that is the one somebody comes looking for.

**The payload is redacted before it is written anywhere.** The record carries the prompt, the reply
and the failure, and what an operator pastes into a conversation can be a credential, so all three
go through the system's redactor first: every value the workspace keeps sealed is matched exactly and
replaced with the secret's name, the driver's token is matched for a driver session, and anything
shaped like a subscription token is caught even when the system never held the value. A value the
system could not know about, a password typed in that was never sealed as a secret, is stored as
typed: the log and the `tasks` table hold what was said minus what the system can recognise, not a
guarantee that nothing sensitive was ever said.

**Nothing consumes it.** There is no projection any more: history does not travel through the log,
so nothing has to read it back. The log exists for a second consumer that is not built yet, a
dashboard, a data pipeline, another machine, and until one lands an empty consumer group list is
the expected state, not a fault.

**No chat channel publishes either.** The gateway (`cmd/gateway/main.go`) is still a service skeleton
that boots telemetry and waits, so the inbound stream a channel would write to does not exist. That
work is #9 and #10, and it is blocked on a bot token rather than on code.

Exporting never fails a task. The task already happened and the store already holds it, so a broker
that is unreachable is logged and the export record is dropped: the log is the audit copy, and the
store is the source of truth.

If `QC_KAFKA_SEEDS` is not set, history is kept in the store and nothing is exported, and the
status block says so rather than leaving an empty column to read as fine.

## What is on the log

Two record kinds exist, on two streams.

**`<workspace>.tasks` carries a `TaskEvent`**, one per task, whether the task worked or not. The key
is the session identifier. The value is protobuf, so it reads as binary in a terminal, and it is
defined in [`../proto/quaycrew/v1/events.proto`](../proto/quaycrew/v1/events.proto), which is the
only place the shape is written down. It carries ten fields:

- `id` is the task's own identifier, and the same value as the row in the `tasks` table. That is how
  a record on the log joins back to the store, with no lookup and no guessing.
- `session`, `workspace`, `project` and `handle` say where the task ran. They are copied into every
  record on purpose, so a consumer knows what it is reading without asking the database.
- `prompt`, `reply`, `status` and `failure` are what happened, redacted as described above.
- `occurred_at` is when the task finished.

The key is the session rather than the task, and that is a deliberate choice. A broker keeps order
inside a partition and nowhere else, and it picks the partition from the key, so keying by session is
what makes one session's records arrive in the order they happened. Two sessions have no order
between them. [`TASKS.md`](TASKS.md) follows one of these records from the moment a task is
dispatched.

### What a task record says happened

A `TaskEvent` has no `kind` field. One message is published, at four moments, and the only thing
that tells them apart is `status`, which is `idle` or `failed`:

- **A task worked.** `idle`, with `prompt` and `reply` filled in and `failure` empty.
- **The model did not finish.** `failed`, with `prompt` filled in, `reply` empty, and `failure`
  saying which: a refusal, a crash, a deadline the caller set, or a caller that went away.
- **The sandbox could not be made.** `failed`, with `failure` naming the sandbox and what the daemon
  said.
- **The system restarted while the task was running.** `failed`, with no `prompt` at all, because the
  system is settling a row it found on the way up rather than reporting a task it ran.

So a consumer of this stream reads `status`, then `failure`, and there is nothing else to branch on.
The stream below is the one with a kind on every record, and it is the one to subscribe to when what
you want is to know what the system is doing.

**`<workspace>.sessions` carries a `SessionEvent`**, one every time something happens to a session.
This is the stream a consumer subscribes to, and the one a workflow trigger will match on, because
here the kind is a field rather than something to work out. It is keyed by session for the same
reason the tasks stream is.

The kinds, and each is something that happened rather than a state the session is in:

- `session.created`, the session exists
- `session.started`, work began in it, and the detail is what was asked
- `session.completed`, the job landed, and the detail is what came back
- `session.errored`, the job did not land, and the detail is why
- `session.halted`, an operator stopped the task with `quay stop`, and the detail is their reason.
  The session survives, so this is not `session.stopped`: it keeps its container and its
  conversation, and the next dispatch continues it
- `session.stopped`, it was put down and its container with it
- `session.reclaimed`, the system took its container back on its own. Nothing else went, so a task
  sent to it builds a fresh container over the same conversation. See section 11 of
  `docs/ORCHESTRATION.md`
- `session.archived` and `session.restored`
- `session.deleted`, with its project or its workspace

`idle` and `running` are not kinds. They are what the session's row says now, which is the fold of
these, and a consumer handed a state learns nothing about what changed. The listing keeps showing the
state; the log carries the change.

The detail is one short line, and it goes through the same redactor a task does before it is written
anywhere, because what came back and what failed can both carry something the operator pasted. The
whole of what was said stays in the task record.

**`<workspace>.job` carries a `JobEvent`**, one per movement of a job, keyed by the job
identifier so one job's records stay in order on one partition. A consumer rebuilding a
tree depends on that. The row goes into `job_events` in the same transaction as the change it
describes, and the export follows the commit, so a system with no broker keeps the whole history and
loses only the copy.

The kinds split in two, and the split is the useful part. A dashboard counting jobs must never break
because the system changed how it leases, and a dashboard counting leases has taken a dependency it was
told not to take.

The contract, which another service may depend on:

- `job.declared`, when somebody wrote the job down. The detail is the title.
- `job.started`, when a controller sent the brief as a task. The detail names the attempt and the
  session.
- `job.answered`, when the answer landed and the claim held. The detail is what it spent and where.
- `job.failed`, when the model did not finish or the sandbox could not be made. The detail is the
  reason.
- `job.stopped`, when a person stopped it, or a limit did, or its claim did not hold.
- `job.asked`, when it put a question to a person. The detail is the question.
- `job.looped`, when three attempts at one step were too alike to tell apart. The detail says the
  step, how alike they were, and what the job escalated to. It is written whether the job then asks
  a person, is handed to another role, or stops, because the loop is the thing that happened and
  where it went is what the job declared.
- `job.held`, when the machine had no room for its sandbox. The job stays pending, so it is not a
  movement, and it is written once per reason rather than once per tick.
- `job.unstuck`, when nothing at all was running while this job waited for room, so the system took
  back the container that had been idle longest and freed the room itself. The detail names how many
  jobs were waiting, which container went, and how long that container had been idle. The job is
  pending before it and pending after it: what changed is the machine.
- `job.told`, when that person answered it. The detail is the answer. The pair is the record of every
  decision a run stopped for, so somebody reading it afterwards learns what was chosen without
  opening a container that is long gone.

A flow run is carried by a job, so a run's own records are on this stream too, against that
job. They are the four kinds
[issue 349](https://github.com/atlantic-blue/quay-crew/issues/349) named, and they are a contract:

- `flow.run.started`, when the run was declared. The detail names the run, the graph and the version
  it pinned.
- `flow.run.asked`, when the run put a question to a person. The detail is the node and the question.
- `flow.run.finished`, when the run reached the end of its graph. The detail is the node, how many
  movements it took and what it spent.
- `flow.run.stopped`, when the run was halted instead: a limit, a refusal, or a person. The detail is
  the reason.

One more, added by [issue 520](https://github.com/atlantic-blue/quay-crew/issues/520):

- `flow.product.replaced`, when the operator, shown the first thing a person can open, answered with
  the sentence they wanted instead. The detail is the new sentence, so the tree says what the rest of
  the work was done against rather than only what it started against.

One history rather than two, which is why they are here and not on a stream of their own. Every step
the run took is a job under the same one, so its `job.*` records are beside these.

Internal, which nothing outside should depend on:

- `job.claimed`, when a controller took the row. The detail names the holder and until when.
- `job.released`, when a hold ran out and another controller took over. The detail names the
  previous holder and the phase it was found in.

Every record carries `trace_id`, the trace the whole tree belongs to. It is minted at the root and
inherited unchanged by every descendant, so one identifier joins a job, its children, the
tasks they ran and the spans around them. `TaskEvent` carries the same field, and so does the `tasks`
row, which is what closes
[issue 346](https://github.com/atlantic-blue/quay-crew/issues/346): before it, the durable record of
what the system did joined to neither the trace nor the log lines, and weeks later the logs are gone
and the row is all that is left.

Each `detail` goes through the system's redactor before it is written or exported, the same way a task
does, because what a caller types into a title or a reason can be a credential.

Two more streams are designed and not built:

- **`<workspace>.inbound`** is a message arriving from a chat channel, written by the gateway, which
  is still a skeleton. [#9](https://github.com/atlantic-blue/quay-crew/issues/9)
- **Outbound delivery** is a reply going back out through a channel, gated so nothing is sent
  without the operator's intent. [#10](https://github.com/atlantic-blue/quay-crew/issues/10)

```mermaid
flowchart LR
    CP["control plane"] -->|"TaskEvent"| TASKS[["workspace.tasks"]]
    TASKS -.->|"nothing reads it yet"| SECOND["a second consumer, when one exists"]
    CP -->|"SessionEvent"| SESSIONS[["workspace.sessions"]]
    SESSIONS -.->|"nothing reads it yet"| SECOND
    CP -->|"JobEvent"| JOB[["workspace.job"]]
    JOB -.->|"nothing reads it yet"| SECOND
    GW["gateway, a skeleton"] -. "InboundMessage, issue 9" .-> IN[["workspace.inbound"]]
    CP -. "OutboundMessage, issue 10" .-> OUT["a chat channel"]
```

A solid line is built. A dotted line is not.

## Why an event log at all

The synchronous path does not need one. Today `quay` speaks gRPC to the control plane, the control
plane writes to Postgres and runs a task in a sandbox, and a reply comes back down the same
connection. Adding a broker to that would be architecture for its own sake.

The log earns its place at the point where the system stops being one operator at one terminal:

- **Channels.** A message arriving from a chat channel is not a request waiting on a response. It
  arrives, it is durable, and something picks it up. If the control plane is restarting when your
  message lands, the message waits rather than vanishing.
- **Decoupling.** Each service publishes and subscribes on its own, which is what makes a second
  channel additive rather than a change to the first one.
- **A second consumer.** A dashboard, a data pipeline or another machine reads the export without
  touching the system's database. That consumer does not exist yet, which is exactly why the export
  is optional.

Automation graphs are deliberately not on this list: flows are a Postgres state machine, with their
transitions appended in the same transaction as the state they describe, and the log only ever gets
a copy. The decision and its reasons are in `docs/ARCHITECTURE.md`.

The intended shape, none of which is wired yet:

```mermaid
flowchart LR
    channel["chat channel"] --> gateway
    gateway -->|"publish workspace.inbound"| log[("event log")]
    log -->|consume| controlplane["control plane"]
    controlplane -->|"write tasks"| store[("Postgres")]
    controlplane -->|"export workspace.tasks"| log
    store --> console["console"]
    log -->|consume| second["a second consumer, when one exists"]
```

## How it runs

`make up` starts it, like everything else. It used to sit behind an `export` profile, on the argument
that a system with no second consumer needs no broker; the cost of that was an export nobody had
switched on and a second command to know about. From `deploy/docker-compose.yml`:

- image `redpandadata/redpanda:v24.2.7`, started in `dev-container` mode with a single core
- two listeners: `redpanda:9092` inside the compose network, and `localhost:19092` published to your
  machine, so a tool on the host can reach the same broker
- a healthcheck that waits for the cluster to report healthy
- data in the named volume `redpanda-data`

The control plane exports when `QC_KAFKA_SEEDS` is set, and the compose file now defaults it to
`redpanda:9092`. Set it to empty in your own configuration to turn the export off, which loses
nothing: the store is the source of truth and the log only ever carried a copy.

## Inspect it

`rpk` ships inside the Redpanda image, so there is nothing to install:

```
docker exec -it quaycrew-redpanda-1 rpk topic list      what streams exist
docker exec -it quaycrew-redpanda-1 rpk group list      who is reading, and how far behind
docker exec -it quaycrew-redpanda-1 rpk cluster health  is the broker itself well
docker exec -it quaycrew-redpanda-1 rpk cluster info    brokers and addresses
```

One task through `quay task --dispatch` creates the topic and puts a record on it, so
`rpk topic list` shows `<workspace>.tasks`. The topic is created by the publisher on first use
rather than provisioned ahead of time, because a workspace's stream is named after a workspace
nobody knew about yet. `rpk group list` still prints no groups, because nothing reads.

This is how you watch tasks go by, live:

```
docker exec -it quaycrew-redpanda-1 rpk topic consume demo.tasks
```

The value is a protobuf `TaskEvent`, so the payload reads as binary in the terminal. What is legible
without decoding is the key, which is the session identifier, and the topic it landed on.

Add `--num 5` to take a few and stop, or `--offset start` to read a topic from the beginning rather
than tailing it.

## How topics are named

`messaging.Topic(workspace, stream)` joins a workspace and a logical stream with a dot, so
`Topic("demo", "inbound")` gives `demo.inbound`. Workspaces are namespaced on one cluster, which is
what keeps two workspaces on the same broker from reading each other's traffic. Names may not
contain a dot, a slash or whitespace, so the separator stays unambiguous.

## Prove the broker works

The plumbing is easy to verify without any Quay System code being involved. This creates a throwaway
topic, writes one message, reads it back and cleans up:

```
docker exec -it quaycrew-redpanda-1 rpk topic create scratch
docker exec -it quaycrew-redpanda-1 sh -c 'echo hello | rpk topic produce scratch'
docker exec -it quaycrew-redpanda-1 rpk topic consume scratch --num 1
docker exec -it quaycrew-redpanda-1 rpk topic delete scratch
```

If that round trips, the broker is fine and any missing messages are missing because nothing is
sending them.

## Testing the client

`internal/messaging/kafka_integration_test.go` starts a real Redpanda with testcontainers and
publishes and consumes against it, behind the `integration` build tag:

```
go test -tags=integration -count=1 ./internal/messaging/
```

Continuous integration runs the same command across the repository. `-count=1` matters: a cached
pass and a real one are indistinguishable otherwise.

## What would switch it on

In rough order, each an open issue:

- **First chat channel inbound (#9).** The gateway consumes from a channel and publishes to
  `workspace.inbound`. Blocked on a bot token rather than on code.
- **Gated outbound delivery (#10)** and **a second channel (#11).** Replies going back out, and the
  proof that a second channel is additive.
- **Read surface for the console (#45).** Tasks, sandboxes, streams and metrics, which is what the
  console needs before it can show more than the store.
- **A second consumer.** The first real reader of the export, whichever lands first: a dashboard, a
  pipeline, another machine.

Until a consumer lands, the export accumulates tasks that nothing reads when it is on at all. That
is the right order to build it in, and it is written down here so an empty consumer group list does
not read as a fault.

---

The `rpk` commands above are the ones to run; the empty listings were captured from a running stack
on 4 August 2026, before the publisher existed. That a task reaches a real broker, creates its topic,
arrives keyed by session and decodes back into the event that was sent is proved by
`internal/controlplane/events_integration_test.go` against a real Redpanda, not by a screenshot.
Reproducing the listing yourself needs the stack up and at least one task dispatched.
