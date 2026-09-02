**A plan is read by several roles, and only what none of them settled is put to a person.** A plan
used to be read by one role, in one session, once. One reading finds what that reading looks for. The
design of the transcript page named the address shape, `/videos?id=<video id>`, and nobody asked what
a person types into it. A test writer asks that first, because an example needs an input and an
output. This system holds seventeen roles and the plan was read by one of them.

The discipline is example mapping run by three amigos: several people with different jobs write
concrete examples for one rule, and the questions nobody can answer become explicit. So a job now
carries question rows. A session writes one with `krewe job question "..."`, the way it records a
step, and a later reading settles one with `krewe job settle <number> "..."`. `krewe job show` prints
every row with its status and what settled it.

`flows/plan-reading.yaml` runs three readings over one plan, as plan-critic, architect and
test-writer. Each is a job of its own, so each is a session that never saw the reading before it. The
engine carries what each reading wrote up onto the plan, and hands the rows that are still open down
to the reading that comes next, without the earlier reader's prose. The graph ends in one question,
and that question carries the open rows and nothing else.

The plan itself reaches every reading as `{{plan}}`, rendered into each prompt. A step is a new
session with an empty working directory and a plan is a column on a row rather than a file, so a run
carries the plan of the job it hangs under: a session running a planned job starts the reading, and
its readings read that plan. A run started with a plan in its state keeps that one. A run under
nothing renders the template as typed, and each reading is told to stop and say so rather than report
on a plan it never got.

A dispatch reply now also lands under `reply.<node>`, beside the `session.<node>` key that was
already there. `result.reply` is exactly what it was, so every graph written against it keeps
working. Before this a run kept one reply, so the second reading took the first one's place and the
run held one of two readings.

Four bounds keep the gate from becoming noise, and the third is the one that does the work. A
reading writes at most three rows. A row that repeats one already on the job is refused, and the
refusal names the row it repeats. A later reading settles what its own lens can, so the count falls
between readings. And a reading that settled everything asks nobody: the run reaches the work with no
question at all.

**What this costs, and what it does not do.** Three readings cost three sessions where one used to
cost one, and a reading that settles nothing is three sessions with one lens. The number to watch is
rows settled by a later reading against rows written. The ceiling of three is chosen and not
measured; what replaces it is the count of rows per reading over the first month of real readings.
The duplicate measure is the words that carry the content, so it does not catch a rename: two
readings naming one hole in different words write two rows, and a person reads both. And nothing
makes a reading write a row. A prompt that says to ask what a person types is still a prompt.
