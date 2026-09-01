**The system does not sit with work held and nothing running.** Twenty five jobs were declared at
once against a workspace allowing eight running. Fifteen finished. Then nothing ran at all: five jobs
sat held saying a sandbox asks for 100 per cent of a processor and that 0 per cent of 1200 per cent
is unallocated, while twelve sandboxes sat idle, every one of them for an hour or more, holding the
whole processor allocation between them. The reclaim time was thirty minutes and not one container
came back. An operator drained thirty three sessions by hand to free a resource the reclaim was
already meant to free.

The controller now makes a fifth comparison, in the same loop as the other four. The first four make
reality match what was declared; this one asks whether they are working. Nothing running with
something held is a state the system reads in one query and it is always wrong, so it takes back the
container that has been idle longest, one per tick, and the next tick starts the work.

**It is the pair and never the pressure.** A full machine with jobs running on it is a healthy
machine, and taking a container back there takes it from a session that is about to get its next
task. Every guard a reclaim already had stays: a container an operator is typing into is never taken,
a system that cannot tell whether anybody is in one reads that as attached, and a session a live job
names is never touched. Nothing here stops a session that is doing work.

**Reclaim was starved, and the two rules are now two queries.** The controller read twenty settled
sessions per tick, ordered by how long ago each was touched. A reclaimed session stays settled, and
with no archive time set nothing ever moves it, so once twenty of those rows sat at the front the
batch was all of them and the reclaim never reached a container again. It now reads the sandboxes
and the sessions waiting to be filed separately, each in its own order, so neither can starve the
other.

**A job that is waiting says which of the two it is waiting for.** "There is not enough processor"
reads the same on a busy machine and on a stopped one, and only one of them means waiting is the
right thing to do, so a job the machine turns away while nothing at all is running says that too. It
is written once rather than on every tick. Where the system frees room for itself, a `job.unstuck`
record goes on the job it freed the room for, naming the container it took and how long that
container had been idle.

**What this does not change.** The room a sandbox reserves still ends with its container and at no
other moment, on both memory and processor together, which is what a scheduler does. The mismatch was
never the arithmetic: a pod ends and a session does not, so the answer is to take the container back.
Nothing here holds a sandbox to what it asked for, which is issue 477, and nothing here stops a
session because the machine is in danger, which is issue 478.
