**A job ends by stating one outcome from a fixed set, so done is a word rather than a reading.** Jobs
on one acceptance run reported "done", "complete", "the pull request is open" and "I could not finish
because the credential expired". All four settled the same way, because the system read the prose to
decide the job was over. A job that could not do its work and a job that did it read identically to
anything downstream, so the operator opened each one to tell them apart and nothing could be counted.

So every session doing a job is told to end its answer with one line carrying `Outcome:` and one of
four words: `proved` (done, and something the session ran proves it), `unproved` (done, and nothing
proves it), `blocked` (it cannot be done, and the reason is under the line) or `decide` (a person has
to decide). The system reads the word off the answer rather than believing a report of it, the same
way it already reads the address of a pull request. The prose stays underneath as the explanation.

`krewe job list --outcome blocked` narrows a listing, `krewe job show` prints the word above the
reason, and the console carries an `outcome` column beside the phase. A flow's choice node branches on
`result.outcome`, which arrives beside `result.reply` rather than inside it, and a choice waiting for a
word the system does not hand out is refused at import. The four words live in one place,
`internal/job/outcome.go`, so the tool, the flow schema and the console cannot drift apart.

**What this costs, and what it does not do.** A job whose answer states no outcome stops rather than
settling, and it is not asked again: the line was in the task it has just answered. What the session
said stays on the row, so the work is still readable. Nothing independent has to agree with the word,
which is [#536](https://github.com/atlantic-blue/quay-crew/issues/536) and not this, and the outcome
moves no phase: a job that ends `blocked` is `done` carrying `blocked`, so continuing a job still
applies to one that failed and to nothing else.
