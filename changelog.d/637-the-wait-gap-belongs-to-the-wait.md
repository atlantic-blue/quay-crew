**The wait gap belongs to the wait a person is in now.** `krewe job show` printed the gap between
`job.asked` and `job.raised` on every job. That is right for a job that is asking, and wrong for the
other two kinds of wait: `asked_at` is written at the question and nothing clears it, so a job that
asked, was answered, ran on and then failed still carried the moment of that first question. On a job
that stopped ten minutes ago and was named a minute later, the reading said "the wait was carried
after 2 hours 51 minutes".

The gap is now measured from where the wait in hand began: the question for a job that is asking, and
the failure, the stop or the hold for a blocked one. The question line is printed only while the job
is the one asking, so a decision somebody already made is never presented as this wait.

A red board records no start at all. The forge reading writes what it read without touching the row,
deliberately, so nothing holds the moment the checks turned red and the reading says that rather than
printing a number measured from something else. Whether that moment is worth a column is open: it
would give the third kind a true age, and it is a schema change nobody has asked for yet.

Two readings of what a job waits on now exist, one off a row and one off the record a caller holds. A
table test holds them to the same word for the same job, because a surface that decides the kind of
wait for itself is how two surfaces come to disagree about one job.

The scenario that proves this held a race, and it failed about one run in four. It let the task that
put the question sit in the model double while the answer started a second task, then made "the next
task" fail. The double fails whichever task reaches it first, so the failure landed on the task the
answer had already superseded. That failure is discarded, the second task succeeds, and the job runs
to done with nobody waiting on it and no wait to read. The scenario now lets the first task land
before the answer starts the second, so one task is in flight when the failure is armed. The model
double also tells a scenario about a second held task, which a `sync.Once` closed over the first one
only.
