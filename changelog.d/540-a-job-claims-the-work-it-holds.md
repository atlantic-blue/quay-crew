**A job says which piece of work it is doing, and a second job cannot take the same one.** Twice in
one run two sessions picked up the same issue and built it under different names. Neither was in the
other's way in the filesystem, because each one already had its own working copy. They were in each
other's way over the work itself, and nothing anywhere said who was doing what. The first anybody
knew was two pull requests conflicting on files both of them had created, and the two designs
disagreed in small places, which is the part that cost the most: putting them back together by hand
was more work than either one would have been alone.

So a declaration may claim one: `krewe job create --claim atlantic-blue/quay-krewe#540`. An issue, a
branch, or a name two people would both use for the same thing. A second job claiming it is refused
while the first still holds it, and the refusal names that job, what it is, and how old its claim is,
so the answer to "who has this" is in the refusal rather than in a search. `krewe job list` carries a
column of what is claimed, and `krewe job show` says it, because a record of intent nobody can see is
not read before somebody starts.

It is not a lock on a file, and nothing stops a session editing anything. It is the record both
sessions would have read before starting.

A claim ends three ways: the job finishes, somebody stops it, or nothing moves the job for two hours.
The third one is why this is worth anything. A claim that never runs out passes every test about
claiming and then stops all work on that issue the first time a container dies. Two hours is chosen
rather than measured: what it has to outlast is the longest gap between two movements of a job that
is alive, and a running job moves its row on every tick of its controller's lease. The measurement
that would replace the number is the distribution of that gap, which nothing takes yet.

The check runs inside the transaction that writes the row, under a lock taken on the claim, because a
check made before the write is one that two callers declaring at the same moment both pass. Both
stores are held to it by the same conformance suite, and the scenarios are in
[features/claims.feature](features/claims.feature).

What it does not do: the claim is scoped to a workspace, nothing outside the system reads it, and
the console's jobs view does not show it yet.
