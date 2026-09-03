**A job cannot be under another job.** The hierarchy is workspaces, then projects, then jobs, then
executions. Four levels and no more. A job belongs to its project, and the only thing that belongs to
a job is its executions, so `parent` and `depth` are gone from the job: from the type, from both
stores, from the wire, from every filter and from every view.

Two things declared a job under a job, and both were wrong about what they were doing.

A session declaring work wrote the job it was running into the parent. That job is a job like any
other and belongs to its project, so it is listed beside every other job there. What caused it is
worth keeping, so the row records `cause`: the job whose session declared it, held as a plain
reference. Nothing treats it as containment and no code reads it to decide what a row is.

A flow run declaring its steps wrote the job carrying the run. A step of a flow belongs to the run,
so the row now records `run`, and a run reads its own steps by it. The job carrying a run lists none.

Three places asked whether a job had a parent to decide what it was. Each is now a statement about the
job itself: a job that states the one sentence runs the four stages, and a job that states none is an
errand. A step of a flow run still runs none, and what says so is the run it belongs to: the graph a
person imported is the plan those steps follow.

The sentence is no longer carried down from one job to another, because no job sits under another. A
job a session declares states its own, or none. A step of a flow run is given the sentence the graph
serves, read off the job carrying the run at the moment the step is declared.

The workspace ceiling that bounded how deep a tree could go is now how many jobs one session may
declare: `krewe limits <workspace> --max-declared <n>`. Zero still means none, so a workspace nobody
configured lets a session declare nothing. It bounds what one session starts rather than how far a
chain of them can go, because there is no chain to count. `--max-depth`, `--parent` and `--roots` are
each refused by name, saying what to type instead.

A steer belongs to the job it landed on. It used to count on that job and on every job above it, and
there is nothing above it now, so the count is the one job's and the report reads one job's marks.

The console's jobs view draws jobs under a project and each job's runs beneath it, and nothing else.
The briefing page draws every job that answers a block as a row of its own, because there is no tree
to indent.

The migration keeps every row. A row a session declared keeps its identity as a job in its project
and gains its cause; a row a flow run declared becomes a step of that run, matched on the run and the
node in its labels, which is what tells a step from the job carrying the run.
