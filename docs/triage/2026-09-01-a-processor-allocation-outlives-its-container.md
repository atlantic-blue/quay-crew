# A processor allocation outlives its container, 1 September 2026

A job could not get a sandbox on 1 September 2026. The refusal read: "there is not enough processor
for this job's sandbox: it asks for 100%, 0% of 1200% is unallocated". Five sandbox containers
existed. The load average was about 2.4 on fourteen processors.

The system counted twelve sandboxes. Seven of the twelve allocations belonged to no container.

This is the reading behind [#622](https://github.com/atlantic-blue/quay-krewe/issues/622). The code
was read against `main` at `f1dc9a0`. The operator measured the figures above on his own machine on
1 September 2026. Nobody drove a live control plane for this reading. Nobody inspected a container
for it. So the part below about the code is a code read, and it is not a reproduction.

## What the operator measured

Five sandbox containers existed. The container runtime reported fourteen processors, which is what
the host has. `krewe limits` showed a request of `1536 MiB, 100%`. That is one whole processor for
each sandbox.

The operator inspected every sandbox container. Each one reported `NanoCpus` of zero and a processor
quota of zero.

Sessions by status: 2 running, 2 awake, 4 idle, 27 reclaimed, 1 reclaimed stale, 152 stopped. Eight
rows sat in a status that implies a container. Five containers existed.

Job `31a6d96d` finished its work and opened a pull request. Its reviewer could not start. The job
ended as stopped, with the reason that nothing independent passed the work. The pull request stayed
open and nobody reviewed it.

## Where the ceiling comes from

`nodeFrom` reads the processor count from the last sample. It turns that count into a share of one
processor, so fourteen processors is 1400 per cent (`internal/controlplane/capacity.go:204`). The
system holds back a floor of 200 per cent for its own containers
(`internal/capacity/measured.go:64`). It holds back the measured figure where that is larger.
`Allocatable` is the difference, which is 1200 per cent (`internal/capacity/capacity.go:104`).

Twelve holds of 100 per cent fill it exactly. The ceiling is correct arithmetic over a total that is
wrong.

## Where an allocation is taken

The controller reserves room before it claims a job. The key is `<project>/<handle>`
(`internal/job/controller.go:613`, `internal/controlplane/capacity.go:39`). A reservation runs out
after ten minutes when no container appears (`internal/capacity/ledger.go:16`).

The control plane places the sandbox under the same key, with the session identifier. It does this at
the moment it creates the container (`internal/controlplane/server.go:494`,
`internal/controlplane/capacity.go:83`).

A placed hold carries no expiry time. `TestAPlacedSandboxNeverRunsOut` states that as a rule
(`internal/capacity/ledger_test.go:107`). `expire` drops only the holds with no session
(`internal/capacity/ledger.go:173`).

## Where it is given back

Each ending below returns the allocation.

A dispatch that failed releases the key. A job stopped for a refused role releases it too
(`internal/job/controller.go:633`, `:640`, `:685`).

A setup that failed after the container was made releases the session
(`internal/controlplane/server.go:530`).

A session stopped, drained, restarted, archived or deleted goes through `closeSandbox`. That call
releases the room first (`internal/controlplane/server.go:860`).

A session reclaimed after its idle time goes through the same call
(`internal/controlplane/lifecycle.go:56`). So the reclaim path is not the leak.

A stray container is reaped at startup (`internal/controlplane/server.go:898`).

A restart repairs the whole ledger. `SeedCapacity` counts the containers that survived the process
and starts from them (`cmd/controlplane/main.go:255`).

## Where it is not given back

A container can go away without the control plane doing it. A crash, an out of memory kill, a
`docker rm` by hand, a runtime restart, or `make upgrade`, which removes sandboxes by name. The hold
stays. It has no expiry, and nothing compares the ledger against the containers that exist.

`ReapStrays` and `SeedCapacity` each run one time, on the way up (`cmd/controlplane/main.go:250` and
`:255`). After that the ledger moves only when the control plane itself acts.

The drift goes one way. The total goes up and never comes down, until the process restarts.

## Why the stall guard did not free it

Issue 575 shipped a guard for a system that sits with work held and nothing running
(`internal/job/stall.go`). It reads a pair: nothing moves, and jobs wait. At the measured moment two
sessions ran, so the pair did not match. The guard correctly did nothing.

So the guard covers the case where every session is idle. It does not cover a ledger that counts
containers which are not there.

## What the row says, and what the daemon says

Eight session rows implied a container and five containers existed. So three rows were already wrong
about themselves. The row is not a safe source for what holds the machine. The sample is. It lists
one entry for each sandbox container with its session identifier
(`internal/headroom/headroom.go:167`), and the system takes it on its own timer.

A total derived from that list alone has a cost, and the cost is the reason the reservation exists. A
container appears seconds after the system admits the job that asked for it. So nine jobs read one
empty sample and all of them fit. That is the incident in issue 466.

So the reservation stays tracked and short lived. The placement becomes derived.

Two guards go with it. A sample that failed, or one that is stale, must reconcile nothing. Otherwise
a daemon that will not answer reads as an empty machine. A container younger than one sampling
interval must never be dropped.

## The request is not a limit, and the message says it is

`runArgs` sets `--cpu-shares` from the request (`internal/sandbox/docker.go:366`). That is a relative
weight. It binds only when the machine is contended. It is not `--cpus`, so `NanoCpus` and the
processor quota stay at zero. That is what the inspection found. The measurement and the code agree,
because a share is not a limit.

So 1200 per cent is a count of twelve sandboxes in the units of a processor share. A person reads "0%
of 1200% is unallocated" on a machine at a load average of 2.4. He reads it as a statement about
processors.

Issue 477 makes the request a real limit, and it is open. That change makes more work stop rather
than less. Issue 622 takes the other road: the accounting says what it counts.

## What a person could see at the time

The refusal names a percentage. The count of sandboxes and the keys that hold them go to a log line
(`internal/controlplane/capacity.go:33`). A person who reads a held job never sees that line.

No command prints the ledger. `krewe room` prints the sample, which is the containers. It never
prints what the system believes it placed. So a person cannot make the one comparison that finds this
in a second.
