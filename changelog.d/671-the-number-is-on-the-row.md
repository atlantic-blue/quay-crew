**A worker is asked for its run, and never for the number its row already holds.** The test stage
asked each worker for a `Requirement: <n>` line and matched the report by it. The build stage asked
for `Vertical: <n>` the same way. That number is on the run: the stage wrote it when it made the run,
so the question could only be answered again or answered wrong.

It was answered wrong. On 3 September 2026 eleven test workers ran across jobs `a171e9c4` and
`6d808f05`, and seven were refused for that line. The stage closed neither job and put a report
format to a person twice. The refusal did not help, because the example in it was a literal: it said
to write `Requirement: 2` whatever requirement the run was for, so a worker on requirement 4 was
told something wrong and was right to be confused.

Both stages now read the number off the run and refuse nothing about it. A reply that names another
requirement is kept as a fault and said out loud in the record a person reads, under the requirement
the row holds:

    Fault 1: the run holding this requirement named requirement 2 in its report, and the row it ran
    under says 1

`Ran` and `Failing` stay as they are. `Ran` proves the suite executed something, because a suite that
finds nothing to run reports success just the same, and `Failing` proves the tests assert work
nothing has built yet. No example in a refusal fabricates a number now: each one names the line to
write rather than a count to copy.
