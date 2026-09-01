**A job that names a repository does not settle on its own answer.** Every failure of the acceptance
run reached the operator through one door: a session finished its work, it wrote an answer, and the
job settled on that answer. Where the work was wrong the answer said it was right, in good faith,
because the session had no way to know otherwise. The answer was the only evidence, and it was
written by the session being judged.

Two sessions that did not do the work now read it first. A **reviewer** reads the change against what
the job was asked for, which is its sentence, its title and its brief. A **tester** runs the
repository's own gates and is told to read their output rather than their exit status, because a
suite that ran nothing exits zero. Each answers with one line the system reads off the answer, the
way the address of a pull request is already read:

    Verdict: fail the change adds a column and no migration, so a fresh store cannot read it

They run in conversations of their own, named after the job, so a second opinion is never formed by
the session that formed the first. Neither holds a credential: what a session may call on the system
comes from the job it runs, and these run no job.

A fail is not the end of the run. It goes back to the session that did the work as its next task,
carrying the reason, and the job stays open, because the branch and the worktree are still there. It
goes back once. Every ask is a task somebody pays for, so a second fail ends the job with what the
gate said on the row, which is the bound the pull request ask already has. A gate that answers
without a verdict, or whose own task failed, ends the job too: a check that quietly passes when it
could not be run is the same false green as no check at all.

The gate is refusable rather than optional. `krewe job create --no-gate` declares a job that settles
on its own answer, the row records that it did, and `krewe job show` reads differently for the two:

    passed by the reviewer and the tester, in sessions that did not do the work
    declared with the gate off, so nothing independent read this work

It costs two more sandboxes per job, one at a time, after the work is done. A job that names no
repository produced no change, so nothing reads it and nothing is paid for.
