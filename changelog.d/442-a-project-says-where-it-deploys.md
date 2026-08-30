**A project says where it deploys.** Which cloud account a body of work ships to lived in one
person's memory. On the acceptance run of 29 August the operator had to say "use atlantic blue
instead", then "otherwise where are you going to deploy it?", because nothing in the crew held that
fact. The cost of getting it wrong is a tree of jobs that writes correct infrastructure for an
account it can never reach, and nothing could tell before a pipeline ran.

A project now carries the account, the region inside it, and the role a pipeline assumes to get
there. `quay target me/house-bills` reads it, the same command with `--account`, `--region` and
`--identity` declares it, and it is a column in `quay project list` and in the console's projects
view, so "where does this go" is answered by a row.

Three values, all of them or none. Half a target reads as an answer and is not one. The identity has
to belong to the account the project names, which is the check the record exists for: pasting the
role from the other account is the mistake that produces that tree of jobs, and the refusal names
both numbers rather than leaving somebody to compare two twelve digit strings by eye.

It is written for one cloud, in [internal/deploy/deploy.go](internal/deploy/deploy.go), because this
crew has one and a free text field is a record nobody can check.

One thing [#442](https://github.com/atlantic-blue/quay-crew/issues/442) asks for is not here. A job
whose brief says deploy is still not checked against its project's target before a controller picks
it up. That is admission rather than record keeping, and it needs this row to exist first.
