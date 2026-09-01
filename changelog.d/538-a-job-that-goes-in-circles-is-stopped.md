**A job that goes in circles is stopped and escalated, rather than left to spend the rest of its
budget.** On the acceptance run of 30 August 2026 a session that could not get a check green tried
the same shape of fix several times and gave the same reasoning each time. Nothing compared what a
session produced against what it had just produced, so from outside a session going nowhere and a
session working hard were one picture: a phase word and a growing bill. The operator was the loop
detector, and only where he happened to read the transcript.

Every attempt at a step now goes on the record with what it produced and how like the earlier
attempts at that step it was, measured on the runs of three words they share. Three attempts the
system cannot tell apart stop the step, and the job escalates by the route it declared with
`krewe job create --escalate`: `ask` puts the question to the operator, carrying what each attempt
said, and `role:<name>` hands the job to another role in a conversation of its own, carrying the same
thing so the new one does not make those attempts again. Empty is asking. A job escalates once, and a
second loop stops it, because escalating twice is the system going round the same loop with more
steps in it. `krewe job show` says which step it went in circles on and what it escalated to.

`--escalate model:<name>` is refused, and the refusal says to import a role that runs on that model
and escalate to it instead. A role declares a model and nothing reads it, the runner taking one model
for the whole system, so that route would read as a decision that had been taken and change nothing.
[#354](https://github.com/atlantic-blue/quay-crew/issues/354) owns closing that gap.

**The threshold is provisional, and the direction of its error is deliberate.** A detector that fires
on real progress stops work that was going to finish, so it sits an order of magnitude above anything
different work scores. Measured on the 304 paragraphs of this changelog over sixty words: across the
46,056 pairs of different paragraphs the median is 0 and the ninety ninth percentile is 0.024, while a
paragraph held against itself with every number in it changed scores at least 0.654. The measurement
runs on every build, in `internal/job/loopcalibration_test.go`. What it does not catch is an attempt
reworded from scratch, which scores like different work: this finds a session saying the same thing
again rather than one thinking the same thing again. Every attempt records its similarity whether or
not it looped, so the number can be measured on attempts after fifty jobs have run rather than on
prose.
