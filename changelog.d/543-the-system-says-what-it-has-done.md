**The system says what it has done, so a session does not have to be told.** A session could read the
repository it was standing in and nothing else. It could not read the jobs that ran, what they cost,
which pull requests they opened, or where one went wrong. All of it was already in the `jobs` table,
and nothing gave a session a way in.

So the operator was the memory. A job that needed to know what happened got the facts typed into its
brief by hand, one at a time. One job to write about two days of this system's own work took a brief
of 1,109 words, and almost every word was a fact already in the database.

`krewe history` is that read. It takes a window, prints the window added up, then one line for each
job: when it was declared, the role it ran as, how it ended, what it cost, how long it took, the pull
request it opened, and, under a job that failed, why. A week of work comes back as about fifteen
lines.

Two ways it could have failed, and both are held by scenarios in [`features/`](features/). It must
not become a dump, so the window bounds it, `--limit` bounds it further, and a digest carries no
brief, no answer and no steps: those are what make a job too large to read a hundred of. And it must
not lie about being bounded, so the total is taken over the whole window before the limit cuts the
listing, and the listing says how many jobs it did not print. A cap nobody is told about reads as
complete coverage.

It is a command rather than a context level or a skill. A level holds text somebody wrote, and it is
stale the moment the next job runs. A skill holds a method, and the method here is one sentence. The
data was what was missing, and this computes it from the store on every read.

The arithmetic lives in `internal/job` rather than in either store, the way a session's last moved
time does. Both stores filter and order, and neither counts, so the two cannot disagree about a
number nobody could check.

`GetHistory` needs `job.read`, the verb `GetJob` and `ListJobs` already need. A digest is strictly
less than reading one job whole, so a second verb meaning the same thing would only be a second thing
to keep in step. The `marketing` role gains that verb and goes to version 2: a role that writes about
this system's work and cannot read it is the failure above.

What this does not do. It reads jobs and not tasks, so what a session said inside a job is still only
in that job's own record. It does not narrow by role, by phase or by label, because each of those
would be a way of asking a question the total above the rows would then answer wrongly. And nothing
yet reads it in the console.
