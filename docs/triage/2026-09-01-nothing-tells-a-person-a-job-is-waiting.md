# Nothing tells a person that a job is waiting, 1 September 2026

Four jobs stopped for a person on the afternoon of 1 September 2026. Each one put a plan up for
approval. Each one entered the phase `asking`. Nothing reached the person who had to approve them.

The jobs were `f71415ba`, `fe7bfea7`, `d77b5719` and `d7b3a3a6`. The oldest waited more than one
hour. He found out because he asked what the state was.

This is the reading behind [#614](https://github.com/atlantic-blue/quay-krewe/issues/614). The
question was not whether a person can find a waiting job. It is whether the system tells him without
him going to look. The code was read against `main` at `e53cc5f`. Nobody drove a live control plane,
so nothing here was settled by observation of a running system.

## What the system does at the transition

Three things, and no more.

The row changes. `internal/controlplane/asking.go:46` calls `store.AskJob`, which writes the phase
and the question, clears what the job was last told, and clears the hold
(`internal/store/memory_job.go:159`, `internal/store/postgres_job.go:301`).

One record is written. The same call writes `job.asked` in the transaction that moves the row.

The record is offered to the event log. `ExportJob` publishes it to the `<workspace>.job` topic after
the transaction (`internal/controlplane/jobevents.go:55`).

Nothing reads that topic. A search of `internal` and `cmd` for a consumer of the job stream finds
the log client itself and nothing else. A system with no broker configured loses the export, and
loses nothing else, because nothing acts on it.

The second road into the phase behaves the same way. A job that goes in circles takes the route it
declared, and the default route writes the same phase (`internal/job/controller.go:1694`). A flow at
an ask node writes the same phase and question onto the job that carries the run
(`internal/flow/engine.go:545`). So one telling covers all three roads into `asking`.

## Everything that can answer the question is pull

The briefing at `internal/web/briefing.go` answers what needs the operator first, and it is a page on
this machine. `krewe job list --phase asking` is a command he must type. The console draws the phase
to whoever looks at it, and reloads every three seconds (`internal/console/model.go:20`).

Each one is correct. Each one waits for a person to open it. The cost of that is the time between the
stop and the next time somebody looks. On this afternoon it was one hour.

The reading in the brief was right. The pieces that answer "what needs a person" exist, and nothing
reaches the person.

## What already exists

Issue 547 and pull request 578 merged on 31 August 2026. The front door answers what needs you, what
is blocked and what landed.

Issue 548 and pull request 583 merged on 31 August 2026. The briefing says what the system does.

Issue 549 closed on 1 September 2026. The row now holds what the forge last said about a pull
request. `jobColumns` in `internal/store/postgres_job.go:16` carries `pull_request_status`,
`pull_request_checks`, `pull_request_check`, `pull_request_review`, `pull_request_read_at` and
`pull_request_failed`. A timer reads them back (`internal/controlplane/job.go:674`). So a red check
is answerable today, and it was not answerable when 548 was written.

Issue 550 closed on 1 September 2026. It is a decision, and `docs/ARCHITECTURE.md` holds it under
"Decided 31 August 2026". The front door stays on this machine. Another device is reached through a
chat channel, which is issue 9 and issue 10.

## The decision in 550 still holds

The decision names three things a wider front door needs. The system holds none of them. They are a
credential for each device, a way to withdraw one device, and a rule about encryption on the path.
`internal/web/web.go` refuses every address that is not this machine, and `features/web.feature`
holds that refusal as a scenario.

So the work in #614 stays on this machine. It does not widen the bind. It sends no message anywhere.
A design that reaches a phone needs issue 9 and issue 10 first, and it needs the owner's word before
it is switched on.

## Where it can reach him on this machine

Each of these already exists. Each cost is what the build adds to it.

The console. It reloads every three seconds and is already in front of him when it is open. One bell
and one line in the header. It reaches a terminal tab that is not in front, which is the only real
push on this list. It says nothing when the console is closed.

Any `krewe` command. One line above the output when something waits. The next shell he opens tells
him, so he does not have to know where to look. It costs one read on each command, and it reaches him
only when he types something.

The status line under an attached conversation. `internal/statusline` draws it on every redraw, and
it is the one place always in front of the person who types. It costs one more field on a line that
exists. It reaches only the session he is attached to.

The terminal that waits on `krewe task`. The reply already prints there. A dispatched job has no such
terminal, and the four jobs of this afternoon were dispatched.

A file that his shell reads at login. It writes outside this repository, onto his machine, and the
delay before he opens a shell has no limit. Rejected.

Any address off this machine. Refused by the decision of 31 August 2026.

## The limit on a wait

A job that waited one hour is not the same as a job that stopped one second ago, and the telling
should say so. The limit belongs on `krewe limits`, beside the lease, the reclaim and the archive
times. That command already declares what a workspace permits, and how long it keeps things.

Start it at 15 minutes. That number is a guess. The measurement that replaces it is the median time
from `job.asked` to `job.told` over one week of real jobs, which the record already holds.

## What proves it worked

The measure is the time between the stop and the person knowing. The record holds the first half
today, because `job.asked` carries the moment. It holds nothing about the second half, so any number
for it is an estimate.

So the first surface that names a waiting job records `job.raised`, with the surface that carried
it. The gap between the two events then reads back rather than being estimated. The cost is one write
from a read surface, one time for each waiting job, and not one for each poll.

## One trap for whoever builds it

A new column on the jobs table must go into `jobColumns` and into `scanJob`. Miss either one and
Postgres reads zero while the memory store passes every test.
