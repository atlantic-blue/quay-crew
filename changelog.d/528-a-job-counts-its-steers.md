**The score of a job is kept by the system now, not by a person with a markdown file.** A steer is one
moment the operator had to say something the system should have known, asked for, or refused on its
own. Mark one while it happens with `krewe steer "the workspace has no secrets"`, which lands on the
job in flight where you stand, or name the job with `krewe steer <job> "..."`.

`krewe job show` prints the count. `krewe steers <job>` reads the marks back in order, with the time
and the job each one landed on. `krewe steers` on its own lists every job where you stand against the
one before it, which is the question the number exists for: did this take fewer than last time.

A steer counts on the job it landed on and on every job above it, so the number on the job at the top
is the score of the whole tree however deep the session that needed telling was. The definition ships
with the tool, printed under every report, because a count whose definition drifts compares with
nothing.

A session cannot record one. The credential a job runs under reaches neither call, so the thing being
scored cannot write its own score.

The reason. The acceptance job of 29 August 2026 took thirteen steers across two days, each one
written out afterwards by the person watching and numbered by hand. Nothing in the system knew any of
them happened, so four issues came out of that job and nobody could say by how much the number moved.
See [#528](https://github.com/atlantic-blue/quay-crew/issues/528).
