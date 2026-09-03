**An execution is one run of one stage of one job, and it is no longer stored as a job.** A stage
that fanned out wrote a full job row for each requirement: a title, a brief, a sentence, a plan and
an acceptance, none of which a run of a stage has. Those rows stood in every listing of declared work
beside the work somebody asked for, and twelve places in the code asked whether a row had a parent to
decide whether it was really a job.

A run is now a row of its own in `executions`, holding what a run needs and nothing a person wrote.
What the session is asked, the mode it runs in and the repository it works in are read off the job at
the moment of the dispatch, so a run cannot carry a copy of the job that disagrees with the job. The
build boundary comes off the stage of the run rather than off a flag on a job row, so `building`
leaves the jobs table.

`parent` on a job stays and means what it always meant: a session running a job declares work under
that job, and a flow run declares each of its steps under the job carrying the run. Those are jobs.

Two calls read the new rows: `ListExecutions` gives the runs of one stage of one job, and
`StopExecution` halts one that has not ended. The migration moves every row that was a run all along
into the new table, keeping its identifier, and moves the records it wrote onto the job it belongs
to, naming the run.

Three things a run inherited from being a job are kept rather than lost with the row. A run that
answers without naming its pull request is asked once more, before anything is landed, so the
correction that costs one task does not cost a person an answer. A run holds the session it works in
open, and a system with runs in flight reads as moving, so nothing takes a container back from a
session that is writing a requirement's tests. And what a job cost is its own session plus every run
of its stages, so `krewe history` counts what a fan out spent instead of dropping it, counting each
pull request once however many runs land in it.

A job also says how many of its runs are working. The console reads that number, because the runs are
in no listing of declared work for it to count.
