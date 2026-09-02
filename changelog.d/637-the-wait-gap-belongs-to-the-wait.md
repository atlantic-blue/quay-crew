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
