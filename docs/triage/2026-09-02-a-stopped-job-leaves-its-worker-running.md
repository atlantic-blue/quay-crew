# A stopped job leaves its worker running, 2 September 2026

Six jobs were declared for the same console work on 1 September 2026, over about one hour. Each new
instruction stopped the current job and declared a fresh one. Stopping a job did not stop the session
that job made.

Four pull requests reached issue [#608](https://github.com/atlantic-blue/quay-krewe/issues/608). Two
were correct and merged. Two duplicated each other, conflicted, and were closed. Then one session
whose job was stopped an hour before reopened both closed pull requests.

This is the reading behind [#621](https://github.com/atlantic-blue/quay-krewe/issues/621). The code
was read against `main` at `f1dc9a0`. Nobody drove a live control plane from this session, so nothing
here about a running system was settled by observation.

## What is measured, and what is reported

These times come from the GitHub API, read on 2 September 2026.

- Pull request 609 merged at 11:50:13Z.
- Pull request 611 merged at 12:34:46Z.
- Pull requests 610 and 612 were closed at 12:38:30Z and 12:38:32Z.
- Both were reopened at 12:52:44Z and 12:52:47Z, fourteen minutes after the close.
- Both were closed a second time at 12:55:43Z and 12:55:45Z.

Every one of those events carries the operator's own GitHub account as the actor, because a session
pushes with the workspace token. The record cannot tell a person from the worker that acted as him.

The session identifier `d6d8b80b` and the figure of 43 per cent of context come from the report of
the incident. This sandbox cannot read the control plane, so neither was measured here.

## What the system does when a job stops

Three things, and no more (`internal/controlplane/job.go:386`).

The row moves to `stopped`, with the reason. One record, `job.stopped`, is written and exported.
Then `RevokeJobCredentials` takes back every credential minted for that job
(`internal/controlplane/jobtoken.go:161`).

That credential is the one the session calls the control plane with. It is not the one that pushes.

Nothing halts the task. `StopTask` at `internal/controlplane/stoptask.go:127` is the call that halts one session's task.
It works. It cancels the context the model runs under. It answers only once the task landed. A search of `internal` and `cmd` finds no caller of it except the command
`krewe stop`.

## Why the halt alone is not the whole fix

The forge token is a workspace secret. It is written into the container once, at sandbox birth
(`internal/controlplane/secretfiles.go:33`), into a memory backed mount. It lives as long as the
container lives. No road takes it back.

So the damage in this incident was not the computation. It was a push, made after the decision to
stop. A token that outlives the decision is the mechanism. A halt of the task in flight does not remove
the token. The next dispatch to that session still holds it, and so does a person attached to it.

## The claim is already released, and that is the second half

`Holding` in `internal/job/claim.go` answers false the instant a job reaches a terminal phase. A
stop therefore frees the piece of work at once, while the worker that does it is alive and still pushes.

That is the two writers failure issue
[#540](https://github.com/atlantic-blue/quay-krewe/issues/540) exists to stop, reintroduced through
the stop. No job in this incident used a claim, so nothing was refused and nothing was recorded.

## The same hole on the other roads out

A job that fails leaves its session the same way: the controller writes the end and revokes the
credential (`internal/job/controller.go:513`), and halts no task. A job the gate stops, a job that
goes in circles and a job over the context ceiling all land through that call.

A halted flow stops the run from taking another step (`internal/controlplane/flows.go:132`). The task
that its carrier job runs finishes.

A refused declaration is the one case with no hole. The store refuses it before a job exists, so
there is no session to leave behind.

## Nothing reports the state

The sessions listing has no job column (`internal/display/session.go:30`). The job filter has no
session field. The link runs one way only, from the job row to the session it made. So a person who
reads a listing of live sessions cannot see which of them belongs to a job that is over.

That is why the recovery took a person: he had to find which session was still alive before he could
halt it.

## What the fix has to answer

The session survives the stop on purpose. `StopTask` keeps the conversation, the container and the
history, and that is right: a stop that loses the conversation is a stop nobody uses. `drain` and
`StopSession` are the calls that put a session down, and they are not this.

So the shape is narrow. The system halts the task. It takes the write to the repository away until
the session gets new work. It holds the claim until the worker is down. The listing says which
session is in this state.

## One trap for whoever builds it

The forge secret is written at sandbox birth and only at birth. `provision` runs when the container
is made or adopted, not on every task (`internal/controlplane/server.go:552`). A build that removes
the file at the stop must put it back when the session gets its next work. Without that, the session
cannot push again for the rest of its life, and nothing says why.
