**A run and the jobs it declared no longer read as unrelated work side by side.** A flow run is a job
that declares jobs. The jobs listing inside a project showed all of them flat, so a run of four steps
was five rows with nothing saying which was which.

Inside a project the listing is now the jobs that were declared. A run says in its own row how many
steps it holds, and `S` opens them. Escape comes back, the way it does from every other level. A step
is a job like any other, so enter on one still opens the work running under it, and a step that is
itself a run carries its own count.

The flat `:jobs` listing is unchanged and still holds every job the system has. A flat listing that
hid every step would be answering a question nobody asked it.

One correction underneath this: a resource's `DrillBy` narrowed every descent out of that view rather
than only the one enter makes. The jobs listing descends into its session's work on enter and into the
job's own steps on `S`, and a single narrowing for both sent the second to the wrong rows.
