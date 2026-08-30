**A job the crew cannot give a sandbox goes back to pending, and runs later.** One job came back
failed saying the sandbox could not be created, after the crew waited two minutes for a container and
gave up. Nothing was wrong with that job. Eight sandboxes were starting and the machine was busy. The
work was lost, and the only sign of it was one word in a listing that somebody has to read on
purpose.

A machine with no room is a moment, not a verdict. The controller now puts the job back to `pending`,
writes why it is waiting on the row, says so in the log, and starts it again on a later tick. The
record says the job was given up, and never that it failed, so an operator reading the history is
never told the job was wrong.

The controller tells the two apart by the crew's own words, which are now one constant that the
control plane writes and the controller reads. `RequeueJob` is a new movement in the store: it
applies only where the controller putting the job back still holds the lease, in the same statement,
so two controllers cannot both move one row.

This is the other half of admission. Asking the machine for room before starting a job stops the
ninth job being admitted at all; this is what happens when a job is admitted and the sandbox still
does not come, which a reading of a machine cannot rule out. The room is reserved under the job's own
handle, so a retry re-reserves the same room rather than taking a second helping.

**A detached dispatch now writes its task down before it answers.** It used to write it inside the
goroutine it spawned. A caller reading the history straight afterwards saw the task before this one,
and so did a controller sending a job's task again: it would answer for this attempt with the last
attempt's failure and pay for a second task. Every double in the unit tier already wrote the record
first, so the real path now matches the thing that stands in for it.

Two things the issue asks for are not here. The start budget is unchanged, because any number picked
now is a guess and nothing has measured what a sandbox costs to create under load. And `failed` is
reserved for the job being wrong for this one cause, rather than across the whole vocabulary an
operator reads.

There is no ceiling on how often a job is put back. Each attempt costs one start, and the crew starts
one sandbox at a time, so a job that keeps being turned away asks again about as often as a start
takes rather than on every tick.
