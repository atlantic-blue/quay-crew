**A job can put a question to a person, so a choice is visible before it is built.** The acceptance
run of 29 August 2026 asked for a page that turns a video into text, on a project context that said
serverless throughout and nothing is always on, and named no store. A session read that, agreed with
it, and chose Aurora Serverless version two, which carries the word and bills a minimum capacity
continuously. The operator found out by asking "will this create an actual database?", which is a
question they had to think of. The crew chose well enough to sound right and the choice was invisible
until it was built.

Prose is advice, and a model may take it. So the crew has the other move now. A session running a job
calls `quay job ask "..."`, the question goes on the row, the job moves to `asking`, and nothing
moves it again until somebody runs `quay job answer <job> "..."`. The answer is sent into the same
conversation as the session's next task, carrying the question back with it, so the session carries
on from where it stopped rather than starting the job over.

`job.asked` has been a named event that nothing wrote since jobs existed, and the phase and the
column have been on the row just as long. This writes them, and adds `job.told` beside it, so the
record of a run says what was asked and what was decided without anybody opening a container that is
long gone.

Asking is not a fifth verb. A session asks about the job it is itself running, and its credential is
already bound to that job, so no role has to grant it and none can withhold it: the alternative to
asking is guessing, and no role should leave a session with only that. Answering is mapped to no call
at all, so nothing a role grants lets a session answer the question a person was asked.

The orchestrator role names the resources it intends to create, and what each costs at zero traffic,
before it declares the children that build them. That is one question at the top of a run instead of
an architecture steer every time something lands.

**What this costs, and what it does not do.** A session waiting to be told something keeps its
container, because the job is not over and the crew does not put away a session its job still wants.
An operator who answers in the morning has paid for the night. Nothing chases them: there is no
notification, and a job that is asking is found by reading `quay job list --phase asking`. And
nothing makes a session ask. A brief that says to state the resources first is still a brief, so a
session that decides not to still decides alone; the refusal that would make the wrong resource
impossible is a hook, and it is not in here.
