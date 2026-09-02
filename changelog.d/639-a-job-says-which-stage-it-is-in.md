**A job says which of the four stages it is in.** The phase says what the system is doing with the
row: pending, running, asking. It never said how far through the work the job is. A job waiting for
an answer about what it understood and a job waiting for an answer about a failed build both read
`asking`, and those two are days apart.

The stages are ideation, design, test and build. `krewe job show` says which one the job is in, what
closed the stage before it, and what opens the next one. `krewe job list` carries the stage beside
the phase, so a job stuck at the beginning reads differently from one further on. The stage never
replaces the phase: a job can be in the ideation stage and the asking phase at once, and that pair is
the useful thing to read.

There is no command that sets a stage. The stage follows from what the job has done, and it is read
off the row rather than written on it. Every boundary is already a fact the row carries, so a second
copy of it could only disagree with it.

**Only ideation is built.** A job that passed it reads as being in design, and the reading says
design is not built yet. The same line says what the job is doing instead, read off its own plan
columns: it writes its plan next, or it holds a plan nobody approved, or it carries on under a plan a
person approved. Test and build say nothing opens them, because nothing does.

A job that states no sentence is an errand and runs no stages, and a job declared under another
carries the stage of the job above it. Both say so rather than naming a stage they are not in. The
console jobs view does not carry the stage yet: its table cuts the title on a narrow window as soon
as a tenth column arrives.
