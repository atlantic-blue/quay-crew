**A job that failed is continued rather than declared again, so the second attempt does not pay for
the first.** On the acceptance run of 29 August 2026 the container runtime went down and took six
jobs with it, a credential ran out sixty seconds into another, and a session was stopped while its
pull request was already open. None of those failures was about the work. The only way back was to
dispatch the same brief a second time, and the second attempt read the same issue, cut the same
worktree and made the same discoveries, so one slice came back as two branches under two names.
Twice.

So a session says what it finishes as it finishes it: `krewe job step "read the issue"`, one line per
step. The lines are rows, so they outlive the container, the controller and the night in between.
Every session is asked for them beside its brief, because a brief that forgets produces a job that
can only ever start again from nothing.

`krewe job resume <job>` continues a job that failed. It keeps its session, so the working directory,
the branch and the pull request are where the attempt left them, and the next task carries the
finished steps, the failure and the moment it stopped rather than the brief. The session is asked to
fetch the branch its work is based on and say what moved while it was stopped, because it may have
moved.

A step that names a pull request against the job's repository puts that address on the job as well.
A job that failed after opening one said so nowhere else: no answer landed, so the address the answer
would have carried was never read.

`krewe job refuse <job> "<reason>"` is the other answer, and it is the one that protects the
operator. A failure that was the work being wrong must not be offered a second attempt, so refusing
ends the job as stopped, carrying both what the operator decided and what it failed with, and a
stopped job is never continued. Which of the two a failure gets is a person's decision: `krewe job
show` on a failure prints both commands, and nothing a role grants lets a session continue its own
job.

Continuing applies only to a job that failed, in the same statement that moves it, so continuing
twice leaves one attempt rather than two tasks against one conversation. Recording the same step
twice leaves one step, because the record is the set of what is finished rather than a log of what
was said.

**What this costs, and what it does not do.** The system cannot check the base. Nothing here runs
git, so what moved under the work is stated as an expectation in front of the session and read back
in its answer, the way a pull request already is. A job that recorded no steps is continued anyway,
and told to look before it repeats itself, which is better than nothing and is not a record. A job an
operator stopped is not continued at all: stopping is the deliberate end, and that is what makes
refusing mean anything. And nothing continues a job on its own, deliberately: a failure that repeats
itself without a person looking is how six jobs become twelve.
