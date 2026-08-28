# Changelog

What has actually shipped, newest first. Nothing is released yet, so the dates are the days the work
landed on `main` rather than version numbers, and anything not listed here does not exist.

The behaviour of each of these is written out as scenarios in [`features/`](features/), which you can
read, or run with `make features`.

## 28 August 2026

- **The roles are quay's own words now.** The twelve in `roles/` arrived as a port and read like one:
  every brief opened by naming another product, and between them they named its files, its slash
  commands and its agents by their `gl-` prefix. This repository is going open source, so a reader
  would have taken all of that for a dependency, and a session running as one of these roles would
  have gone looking for a command that is not here.

  Nothing about what a role is for changed. Every mention came out of `roles/`, out of `docs/ROLES.md`
  and out of the scenarios, and what it named was replaced with quay's own: a brief that said it was
  spawned by a slash command now says a flow step or a piece of work names this role, which is how a
  role session actually starts. A brief that told the model to run another product's command now
  names the quay role that does that job, so the assessor declares a piece of work for the `security`
  role rather than spawning `gl-security`. Documents a brief writes and reads moved from that
  product's dot directory to plain `docs/` paths in the repository the role is working in.

  Where quay has no equivalent, the brief says so rather than inventing one. Every command written
  into these briefs was checked against `quay help`.

  **The honest lines stay honest.** Each brief still opens with what quay does not enforce, because
  none of that changed: a role cannot be told which files it may not touch, and a role session cannot
  put a question to the operator. The two sentences are just written without leaning on another
  product's name to carry them.

  **A guard, so this cannot come back.** A test reads `roles/`, `docs/` and the scenario files as
  directories rather than as a list, and fails on the name, on a `gl-` prefixed agent, or on a slash
  command of that shape. It refuses a directory that is not there and it reports how many files it
  opened, because a sweep that opens nothing finds nothing and finding nothing is what a clean tree
  looks like. Over the database, the briefs are read back out of Postgres and held to the same rule
  and to the file they came from, since a brief that came back empty would pass any check that only
  asks what is absent. A scenario runs a flow step as the shipped `architect` and reads the memory
  file that session is finally told to work by.

  The entry below for 27 August 2026 is left as it was written. It says what shipped that day.

## 27 August 2026

- **The front door says what the crew does today.** The README's list of what works predated
  seventeen pull requests that merged on 27 August 2026, and named none of them. It now leads with
  the shape of the product rather than with a feature list: you declare a piece of work, the crew
  writes it down, a controller makes reality match the record, the work outlives the controller that
  started it, it runs as a named role with only what that role receives, and every movement is
  exported carrying the trace the whole tree belongs to. There is a diagram of one piece of work from
  declared to done, and `docs/ORCHESTRATION.md` is the long version.

  The quick start is the single `make install`. The status paragraph was checked against the code
  before it was rewritten: chat channels still do not exist and the gateway is still a skeleton that
  boots and waits, there is still no admin dashboard, and nothing still consumes the event log. What
  was no longer true is the telemetry stack, which starts with the crew and carries logs, what each
  task cost, and traces of the crew's own calls.

  The blockquote is gone, and nothing brings it back. A reader who wants to copy a paragraph out had
  to strip a `>` from every line of it first.

  **The front door is now tested.** Three things it claims have an answer elsewhere in this
  repository, so they are checked against it rather than reread by hand: every `quay` command it
  names is one the tool this checkout builds actually lists, every `make` target it names is one the
  Makefile declares, and every document it points at is there. The quick start is held to one
  command, the diagram is held to showing a piece of work through the controller, the lease, the
  session and the role, and the whole file is swept for a blockquote, a table or a dash used as
  punctuation. What none of that says is whether a sentence is true: a bullet claiming a capability
  in words that name no command passes every case. `features/` is what says whether a capability is
  real.

- **A first run is one command.** It was four, and the order mattered: `make config`,
  `make sandbox-image`, `make up`, `make install`. Miss one and the failure arrived somewhere else.
  Compose read a configuration file that was not there, or a first task was refused for a missing
  image, which reads as a broken crew rather than a missing step.

  `make install` is now the whole first run. It writes `~/.quay/env` if there is none, builds the
  command line tool, the hooks and the sandbox image, brings the stack up, and prints the four
  commands it cannot run for you, in full. It cannot mint your model credential, so
  `claude setup-token` and the three `quay` commands after it are still yours.

  Running it twice is safe. It never writes over the configuration file you edited. On a crew that
  is already up it says what replacing the services costs, because a task in flight is executing
  through the control plane and ends with it, then waits for you to type the crew's name back the
  way `quay workspace delete` does. `YES=1` goes over it without being asked. Every refusal exits
  non zero, which is the defect issue 419 is open about one layer up.

  **The target that built only the command line tool is now `make tool`.** `make install` used to be
  that and nothing else, so the name had to move for the first run to take it. `make rebuild` is
  still the three builds together and still leaves a running crew alone, which is what to type when
  you want a new build and not a restart. `make config`, `make hooks`, `make sandbox-image` and
  `make up` are unchanged and still work on their own. `make upgrade` calls `make tool` where it
  called `make install`, so it still builds the tool before it drains and it still brings the stack
  up exactly once.

  A failed `go build` inside that target used to print "installed quay to ..." and exit 0, because a
  shell command list exits with the status of its last command. It exits non zero now.

- **Twelve roles ship, ported from greenlight.** Quay had the role mechanism and no roles, so
  `roles/` at the root of the repository now holds `architect`, `assessor`, `codebase-mapper`,
  `debugger`, `designer`, `implementer`, `marketing`, `marketing-researcher`, `security`,
  `test-writer`, `verifier` and `wrapper`, ported from the twelve agents in
  [`atlantic-blue/greenlight`](https://github.com/atlantic-blue/greenlight/tree/main/src/agents) as
  they were on 27 August 2026. Import one with `quay role import roles/<name>`. A fresh crew is
  seeded with none of them.

  Each agent's body is already an instruction, so it became the `ROLE.md`. The frontmatter did not
  carry over: a greenlight agent declares `tools:` and quay has no word for a tool or a file, so
  nothing was invented to stand in for it. The `gl-` prefix came off. Each role keeps greenlight's own
  model, and the wrapper, whose model greenlight resolves at run time, names sonnet here and says so.

  **A role cannot be told which files it may not touch, and eleven of these twelve briefs ask for
  exactly that.** `test-writer` says it never sees implementation code, `implementer` says it never
  edits a test file, `verifier` and `assessor` and `codebase-mapper` say they are read only. Quay
  enforces none of it: `receives` is `work`, `context` and `skills`, and none of the three is about
  the contents of a repository. So every role opens with a line saying what its brief asks that the
  crew does not hold it to, and names the greenlight paths and commands it mentions that do not exist
  here. `docs/ROLES.md` says it once for all twelve.

  All twelve receive `work`, `context` and `skills`. Only the assessor declares a `may` list,
  `work.create` and `work.read`, because its brief spawns a security review and reads the answer.

  Two briefs did not fit. A brief may be 16,384 bytes and greenlight's architect is 17,993 and its
  assessor 18,126, so both were refused at import as they stood. Restated lists and the sample of the
  message greenlight's orchestrator sends came out of those two, nothing either asks the role to do
  was removed, and `docs/ROLES.md` lists every cut. The ceiling was sized on an estimate that these
  roles "run to about twelve thousand bytes", and two of them are eighteen thousand, so raising it is
  worth a look.

  `role.All` reads a directory of roles and refuses one holding none, so the test that every shipped
  role imports reads `roles/` rather than a list somebody has to remember to extend, and a `roles/`
  that lost its contents fails rather than reporting a clean run over nothing.


- **A flow run declares work instead of waiting on it.** A run used to call `Dispatch` and read the
  reply from the same statement, so starting one lasted as long as the model did and the run could
  react to nothing while it waited. It writes the step down as a piece of work and returns. A
  controller sends the task, and the run carries on when that work reaches a terminal phase, read off
  a row by the poller. So a crew restarted while twenty steps were running picks all twenty up on its
  next tick rather than losing them, and the engine holds no goroutine, no call and no container.

  A run is carried by a piece of work, and every step is another under it, one level deeper. There is
  one tree and it is the work tree, because depth and budget are counted once and a run outside the
  tree would be counted by neither. Read a whole run out of it with
  `quay work list --label flow.run=<run>`, which `quay flow show` now prints.

  A step's answer is a field rather than a line of a transcript, which is what `quay answer` and
  `quay work show` read. `working` is a new run status, for a run whose step is out. The four records
  issue 349 named are written at last: `flow.run.started`, `flow.run.asked`, `flow.run.finished` and
  `flow.run.stopped`, as work events on the piece of work that carries the run.

  A run asking a person now holds no container. Its step's session is put away the moment that step's
  work ends, which is the trap issue 354 names by name.

  **A run no longer has one session, and a graph author has to know that.** Each step is a piece of
  work, a piece of work owns the session that does it, so a step does not see what the step before it
  was told. What travels between steps is the run's state: `{{result.reply}}` in a prompt is the last
  step's answer. A graph that leaned on the earlier conversation has to say what it needs.

  The work controller now runs work under a parent, because a flow declares every step under the run
  and a controller that started roots only would leave every step of every automation pending forever.
  Work waiting on something in `after` is still left alone, since nothing honours ordering yet, and
  the tree budget is enforced for nothing, a root included. The workspace's `max_depth` is checked
  when a run is declared, against the credential of
  whoever started it, and not again per step: a graph is a finite set of nodes with a transition cap
  rather than a way to recurse, and checking each step would have meant no flow ran at all until an
  operator raised a limit.

  `advance.go` is unchanged. The reducer never learns that work exists: the engine puts a run back to
  running before it feeds a step's result in. This is slice 2 of issue 399 and slice 8 of the
  delivery order in `docs/ORCHESTRATION.md`. The trigger node is slice 9 and is not here.
- **Work runs as a role, and what a role receives is enforced before a container starts.** A piece of
  work already carried a role and the version it was pinned to, and the controller read that column
  and left the work alone. Now it runs it: the session the controller opens runs as that role, so it
  is told the role's brief, it is given what the role receives and nothing else, and the credential
  the crew mints for its task carries the verbs the role's `may` list declares. A role that declares
  no `may` list may call nothing, which is the direction the capability model already took.

  The role comes off the work record and never from the caller of the task. A caller that could name
  its own role could name one granting more than the work was declared with.

  A piece of work can now say what it cannot be done without, with `--hands`, in the same three words
  a role receives: `work`, `context` and `skills`. Where the role does not receive what the work
  hands, the work is refused. The refusal names the role, the material and the two ways out, and the
  work stops with that reason on the row.

  It is checked at the write and again at the dispatch. The second check is the one that matters: a
  role can be detached, imported at a new version and attached again while work sits pending, so what
  the crew would put in front of a session is only settled at the moment it hands it over. Work
  refused there never reaches a container.

  Refused rather than withheld, which is the difference from a flow step naming a role. A step is
  given less, silently, because the operator wrote that boundary into the graph. A piece of work that
  says it cannot be done without the context is saying the opposite, so running it without would
  leave a session answering plausibly instead of stopping, which is what `--expect-file` already
  exists to catch.

  Work that names no role is unchanged. It hands its material to nobody, no boundary applies to it,
  and the scenarios written before this still pass as they were.

  Still root work only: work under a parent and work that waits for something are left alone, role or
  no role. A piece of work records the role version it was declared against and the session is built
  from the version the workspace holds now, so a role narrowed underneath declared work stops that
  work rather than running it as it was written.

- **A session gives its container back and keeps its history.** Nothing in the crew ever put a
  session away on its own, so a session that answered one question in March still held its container
  in August unless somebody restarted the crew. A session now has a fifth state, `reclaimed`: the
  container is gone and everything else stays, so a task sent to one builds a fresh container over
  the same conversation and the same files and carries on. Reclaimed is not stopped, deliberately. A
  stop is somebody's decision and a reader goes looking for who made it; a reclaim is the crew saving
  memory, and the next dispatch fixes it.

  The controller that runs work reads a fourth query each tick: the sessions nothing is holding open,
  meaning live, not running, and named by no piece of work still in flight. `quay limits <workspace>
  --reclaim <duration> --archive <duration>` says how long one of those keeps its container, and how
  long a reclaimed one waits before it is filed away.

  **Both times ship unset, and unset means the controller does nothing at all.** No number is written
  anywhere: not in a default, not in a comment, not in this entry. Three measurements decide them and
  none has been taken. Section 11 of `docs/ORCHESTRATION.md` names each one and the command that
  would take it. A crew upgraded to this build behaves exactly as the one before it, and there is a
  scenario that proves an unset time changes nothing however long the loop runs.

  A container an operator is typing into is never taken. The crew could not tell before: `quay
  attach` hands the operator a command to run against the container and records nothing afterwards.
  It now asks the container whether the `quay` tmux session inside it has a client. A crew that
  cannot get an answer reads that as attached, so an unreachable daemon costs a container held longer
  rather than a conversation closed under somebody's hands.

- **A running session can be stopped, and it survives.** `quay stop <session> [<reason>]` halts the
  task a session is running and keeps the session: its conversation, its container and its history
  all stay, and the next dispatch continues it. The task record reads `stopped` with the reason
  rather than `failed`, because an operator asking for a stop is not a fault and a stop that reports
  as a crash hides the real crashes. A piece of work running in that session is stopped with the same
  reason. A stop while nothing is running says so and changes nothing, and the command answers only
  once the task has actually ended. See issue 395.

  What people reached for before this was killing the dispatch client, which is not an interface and
  does not reliably end anything: on 27 August 2026 the same kill ended one task at once and left
  another working for sixteen more minutes, merging two pull requests after the operator believed it
  had stopped.

- **Every movement of a piece of work is on the record, and carries its trace.** Each movement was
  already written to `work_events` in the same transaction as the row it describes. Now it is also
  offered to `<workspace>.work` after that transaction commits, keyed by the work identifier so one
  piece of work's records stay in order on one partition. Six kinds are a contract another service
  may depend on: `work.declared`, `work.started`, `work.answered`, `work.failed`, `work.stopped` and
  `work.asked`. Two are internal and nothing outside should read them: `work.claimed` and
  `work.released`.

  Nothing about the rule changes. The store is the source of truth, the log is an audit export, a
  crew with no broker loses the export and nothing else, and a broker that refuses every record costs
  the copy and never the work. There is still no consumer.

  A piece of work now carries `trace_id` and `parent_span_id` as columns. The trace is minted at the
  root and inherited unchanged by every descendant, so one identifier joins a piece of work, its
  children, the tasks they ran and the spans around them. The `tasks` row carries the same field,
  which closes issue 346: before it, the durable record of what the crew did joined to neither the
  trace nor the log lines, and weeks later the logs are gone and the row is all that is left.

  Both are columns rather than something a process holds, and that is the point of them. A controller
  that picks up work somebody else started reads the trace off the row and goes on being part of it,
  which is what makes a trace survive the controller that died.

  A task carries its trace context to the container as `QC_TRACEPARENT`, on the task and never on the
  sandbox. A sandbox is born with its environment and is then reused, so a value written at birth
  would label the tenth task with the first task's span. Nothing inside the container reads it yet,
  which `docs/OBSERVABILITY.md` says plainly.

  Two spans: `work.attempt` for one attempt and `work` for the whole life of a piece of work. Both
  are recorded once the crew knows both ends rather than held open in memory, because a span held
  across a controller that died would be lost with it.
- **A session may declare work, within limits.** A role now says what a session running as it may
  call, in a `may` list beside `receives`: `work.create`, `work.read`, `work.answer` and `work.stop`,
  refused at import by name for a word the crew does not know. A role that declares none may call
  nothing, which is what every role written before this became.

  A session running a piece of work is handed a credential minted for that work. It carries the verbs
  its role declared and expires with the work, so a value read out of a container grants what that one
  piece of work could do and only until it ends. That is strictly less than the driver's token grants.
  It travels in the environment of one task and never at sandbox birth, because a sandbox keeps the
  configuration it was made with: a credential written at birth would label every later task with the
  first task's grant, and one minted afterwards would never reach the container.

  The parent of anything a session declares is read from that credential and never from the request,
  which is the whole of what makes depth bound anything: work at depth one creates at depth two, so a
  loop of any shape stops at the ceiling.

  `quay limits` reads and sets what a workspace allows: how deep the tree of work may go, how many
  pieces run at once, what a tree may spend, and how long a controller holds a piece of work. Max
  depth starts at zero, so no session declares work until an operator raises it, per workspace,
  deliberately. Only depth is enforced today, and the refusal names the limit and the command that
  raises it; nothing runs a child yet, so there is no fan out for the other two to bound.

  The four hook calls join what the driver may not do. A hook is a command that runs on a session's
  own tool use, so attaching one changes what every session in that workspace may do, and reading the
  list is reading the map of the guard the session is under.

- **The crew says how much room the machine has left.** The host ran out of memory and the kernel
  killed 18 sandboxes, three monitors and a build in one event. Nothing in quay reported it before,
  during or after. The console kept drawing a healthy crew, and every number that mattered had to be
  read from outside quay with `docker stats`. See issue 405.

  The control plane now reads the daemon it already talks to, on its own timer, and everything else
  reads that last sample. The header carries one figure and one word: what every container holds
  against the limit that binds, and `room`, `tight` or `full`. Full is drawn so it can be read
  without reading the number. A new console view is one line per sandbox, largest first, with what it
  holds, its share of one processor, how long since its last task, and what its session is doing. The
  last column is there because the largest sandbox may be the one doing the work.

  The limit that binds is the daemon's own and never the machine's memory, so a Mac with 36 gigabytes
  and a 7.8 gigabyte cap is full at 7.8. The machine underneath the daemon is reported apart from it,
  because the kill came from there while the daemon sat at less than half its cap and the machine was
  at 94 per cent of its swap.

  Nothing is estimated. Every figure is measured or it reads unknown, and unknown is never room. A
  read that fails never fails a command: the figures read unknown and the answer says why.

  `quay room` keeps the reading it always did inside a sandbox, which is the question a session about
  to run a gate is asking. Off a Mac it used to fail outright, so the operator most likely to need the
  number was the one who could not have it. It now asks the crew and prints what the crew read.

  The three words are fractions of the binding limit, three quarters and nine tenths, and both are
  provisional. `docs/OBSERVABILITY.md` states them, says what would measure them, and carries the two
  sandbox figures that show why the view exists: a working sandbox at 1,206 mebibytes and an idle one
  at about 1.6 megabytes.

- **Work survives the controller that started it.** A controller takes a lease on the work it runs,
  and renews it on every tick while its task is open. Kill that controller and the task keeps
  running, because the sandbox belongs to the crew rather than to the controller. The lease runs out,
  another controller finds the work, reads the task record before it does anything, and takes the
  answer that landed. The work is sent once and paid for once.

  The claim is one statement with its condition inside it, so two controllers racing over one row
  leave one winner and the loser is told the work is somebody else's. The take over is the same shape,
  because a race can happen at either moment.

  Only work with no task anywhere goes back to pending, since that is the one state that says for
  certain nothing was paid for. Where the row carries no session, the crew is asked for one named
  after the work, which covers a controller that died between sending the task and writing down where
  it went.

  The lease is a minute, and the number is derived rather than chosen. A holder renews every five
  seconds, so what the lease has to outlast is a gap between renewals rather than a task: measured on
  one machine against the real control plane and the real store, a tick with nothing to do cost 1 to 4
  microseconds and a whole piece of work from declared to done cost 2 milliseconds. A minute is twelve
  of those intervals. It is provisional until there are fifty completed pieces of work to measure the
  gap over, and `QC_WORK_LEASE` sets it in the meantime.

  Every movement writes its own record. `work.claimed` says who took the work and until when, and
  `work.released` names the controller that stopped and the phase its work was found in. Both are
  internal: a dashboard counting work should never break because the crew changed how it leases.

- **A controller makes declared work happen.** Declaring a piece of work recorded intent. A loop
  inside the control plane now makes reality match it: it reads the work the crew holds, sends the
  brief as a task, and writes what came back onto the row. Declare work, close the terminal, and find
  it done, with the answer on the record and the conversation that did it named beside it.

  The loop never waits on a model. It dispatches and lets go, and reads the answer off the task
  record on a later tick, so a piece of work that takes an hour costs the loop nothing. It ticks
  every five seconds and once on the way up, so work declared while the crew was down starts when the
  crew comes back.

  Asking twice is paying twice, so the claim is a conditional update in one statement: pending to
  running, or nothing. Two controllers ticking at the same moment leave one task, and a tick run
  again over a row that has already started does nothing.

  What a piece of work claimed is checked by the crew rather than believed from the model. An answer
  that does not carry what the work said it would, or a file the work said would be there and is not,
  stops the work with a reason naming the claim. The answer stays on the record either way, because
  what the model said is how somebody works out why the claim failed.

  It runs root work only: work that waits for something else, work in a role and work under a parent
  are read and left alone for the slices that honour their ordering, their boundary and their budget.
  There is no lease and no recovery from a controller that died mid dispatch yet.

- **A dispatch that cannot start says so instead of waiting without end.** A control plane that had
  come back from the machine running out of memory served every listing in under a second and
  started no work at all. `quay dispatch` wrote the session row, then stayed inside the call: no
  task recorded, no container made, no line in the log, four attempts given up on at 45, 60, 90 and
  120 seconds, and four sessions left sitting idle with nothing behind them. Restarting the
  container cleared it. See issue 400.

  The wait that reproduces every one of those symptoms is the export of the session record to the
  broker. A producer holds a record for as long as it is given, and by default it is given no limit,
  and the export deliberately carries a context detached from the caller's so a record outlives the
  request. Nothing was left to end it. A test now stands a broker up that accepts the connection and
  answers nothing, and the dispatch behind it is held for as long as anybody watches.

  The machine ran out of memory an hour before the crew wedged. Nothing here proves that is what put
  the broker in this state, so the trigger stays unproven and what is fixed is the wait.

  Four things change. Every wait between a session row and a running sandbox is named and logged, so
  a hang reads as "waiting for X" with no "waited for X" after it rather than as silence. The whole
  path from the row to the sandbox has one budget of a minute, measured from what a healthy start
  costs: about two seconds for `docker run` of the sandbox image, and under six and a half for a
  whole first dispatch in continuous integration. A dispatch that runs out says which wait it gave
  up on. A dispatch that fails after its row was written now says so on the row, as failed, with a
  task carrying the reason, so nothing is left looking like a session waiting for work. And the
  crew's one start at a time was a lock nobody could give up, so one stuck start held every later
  dispatch and every stop behind it; it is now a wait like the others.

  The export is a copy of a record the store already holds, so a broker that never answers now costs
  the export and never the work.

- **A crew is asked whether it can write, not only whether it answers.** The control plane serves
  the standard health check, and answering it writes: a row in the store, and a record on the event
  log. Both are writes a dispatch makes before a sandbox is ever asked for. The stack asks it every
  thirty seconds, through the service binary's own `health` mode, because the image carries no
  shell. A crew that reads well and cannot write reported itself healthy for an hour, and that is
  why nobody saw the fault above.

- **Work is a record the crew keeps.** A caller declares a piece of work with
  `quay work create --title "..." --brief "..."`, and the crew keeps it: a row with a title, a brief,
  a role, a mode, a claim about what the answer must carry, what it waits for, a deadline, a budget
  and its labels. `quay work list` says what there is, newest first, and narrows by phase, by label,
  by parent or to the roots. `quay work show` reads one piece of work whole. `quay work stop` halts
  work that has not ended, keeping the reason.

  The intent is a row rather than a list held in a process, so it outlives the terminal that asked
  for it. Declare work, close the terminal, read it back tomorrow.

  Nothing runs it yet. Declaring work records intent, and a controller that makes reality match the
  record is the next slice.

  Every rule is checked at the moment of the write, while the person who wrote it is looking. The
  identifier is the crew's to assign and one a caller sets is refused. The parent comes from the
  credential the caller presented and one in the request is refused, because a caller that could set
  its own parent could set its own depth. A title over 200 bytes, a brief over 16,384, a mode that is
  not a mode, a path that climbs out of the working directory, a budget below zero, more than 16
  labels, a label over 63 characters, a role the workspace does not hold and work waiting on
  something the crew does not hold are each refused with a sentence saying what to do instead.

  The record of what happened is a row beside the row it describes, written in the same transaction:
  the store is the source of truth, and a row with no record of how it came to exist is a state
  nothing can explain afterwards. Nothing is published to the event log yet.

- **The crew says which build it is, and drift is reported.** `quay version` prints three builds now:
  this tool, the crew, and the image every session runs in. Where two of them came from different
  commits it says so and names both. Any command, not only this one, puts one line on standard error
  when the tool and the crew are different builds, and then runs as it always did.

  A crew is three parts and each is built on its own. An upgrade stops every container, so it gets
  put off, and the three drift apart. On 27 August 2026 three defects were investigated as if they
  were live and all three were fixed already: the tool in use was built thirteen minutes before the
  first of those fixes landed.

  The control plane binary is stamped with its build the way the tool is, and the compose stack
  passes the commit it builds from. A crew from before this says nothing, so the tool reports the
  build as unknown and names when a crew began to answer, rather than reporting an error. A crew
  that cannot be reached is left to the command itself to report.

- **An answer comes back out as data.** `quay answer <session>` writes the reply of the most recent
  landed task to standard output, with nothing else on it. `--all` writes every landed reply, oldest
  first, one to a line.

  `quay ask` prints the reply of a task it waited for. A dispatch lets go of its task, so the answer
  to a dispatched task had no way out. The history listing is written for a person: it shortens a
  reply at 120 characters and puts a clock and a speaker beside it. A caller that piped it read a
  listing where the value belongs.

  A refusal goes to standard error and the exit status is not zero, so a caller that reads standard
  output never receives a sentence where the data belongs. A session with no landed task is refused.
  A task still running is refused as running, rather than answered with the task before it, because
  the answer to the previous question is not the answer to this one. What a failed task failed with
  is its answer, so it is printed, and the command still fails.

- **Every surface takes the identifier the listing prints, and enter opens a session.** A session
  carries two identifiers. The `id` is the crew's own row and it names the sandbox container. The
  `handle` is the name a channel dispatches to, because a chat channel knows what it calls a
  conversation before the crew has a session for it. The listing printed both: the `id` in a column
  headed `id`, and the `handle` under the heading `name`, which is where a name goes.

  So the operator read the `name` cell and typed it back, and `quay dispatch 615d48dc "and again"`
  did not refuse it. It joined the word to the message, sent `615d48dc and again` to a session
  nobody asked for, and said nothing. Naming a session then took the `handle` off the screen
  entirely, leaving a row with nothing on it that any command had ever taken.

  Now the first column is headed `session` and it holds the `id`, the `name` column holds a name and
  is empty until somebody writes one, and one resolver reads what the operator typed for every
  surface. There were two of these and they disagreed: one answered with the `id` and the other with
  the `handle`, and each wrote its own refusal. Both identifiers still reach a session, from
  `dispatch`, `tasks`, `attach`, `label`, `mode` and the console, because the `handle` is what a
  channel sends and it is in notes and in scripts. Only one of them is printed, and a refusal offers
  that one.

  A first word shaped like an identifier that names no session is refused, and the refusal says to
  quote the whole message if that is what it was. It is never absorbed. `quay dispatch hello there`
  is still a message.

  Enter on a console row could not report a failure. It wrote the reason into the model and asked for
  a listing in the same return, and the listing that came back blanked the reason before it was
  drawn, so the key looked like it did nothing at all. The reason is now held until the next key, and
  the scenario for it runs the command the key produces, feeds the answer back the way the runtime
  does, and asserts on where the operator is left.

## 22 August 2026

- **A session says what happened to it, and every record carries a kind.** The crew emitted one
  event, a finished task, and that record has no kind at all: the only thing that varied was
  `status`, which is `idle` or `failed`. So nothing could tell that a session had been made, that
  work had begun, or that one had been put away, and nothing could react to a change. A workflow
  trigger has nothing to match on when there is nothing to match.

  Eight kinds now, on a stream of their own: `session.created`, `session.started`,
  `session.completed`, `session.errored`, `session.stopped`, `session.archived`, `session.restored`
  and `session.deleted`. Each names something that happened, in the past tense, at one moment.
  `idle` and `running` are deliberately not among them: they are what the row says now, which is the
  fold of the events, and a consumer handed a state learns nothing about what changed.

  Each event lands in the store in the same breath as the thing it describes, and is exported to
  `<workspace>.sessions` after, keyed by session so one session's records stay in order on one
  partition. That is the split the crew decided on 9 August: the store is the truth, so
  `quay` and the console can read the lifecycle whether or not a broker is up, and an export that
  cannot land is dropped rather than failing what already happened. The detail each event carries
  goes through the same redactor a task does, because what came back and what failed can both hold
  something the operator pasted.

  `ListSessionEvents` reads them back, for one session or for the whole crew. The console's own view
  of them, and the kinds the flow engine should emit for a run, are the next two pieces.
  ([#349](https://github.com/atlantic-blue/quay-crew/issues/349))

- **The console says how full each session's context window is.** A new `ctx` column, beside what the
  conversation cost, holding `26%`. It turns yellow at thirty per cent, where the line under the
  prompt turns yellow, and both work the share out in one place so they cannot disagree about the same
  conversation.

  How full the window is is not what the conversation cost, and the difference matters: cost only
  grows, while the window empties again when the model compacts. So the column reads what the last
  answer carried, out of the transcript the model keeps, and skips a sub agent's answer because that
  fills a window of its own.

  The size of the window is nowhere in the transcript, and a list of models in the crew's own code
  would be right today and quietly wrong at the next one. Only the model runtime knows it, and it says
  it to the status line. So a session writes it down in the conversation directory the crew already
  mounts, and the crew reads it from there. Until some session in a workspace is attached to once, the
  column shows the count instead, for example `258k`. A count is true; a share worked out from a
  guessed window is not.

## 21 August 2026

- **The status line reaches a session, which yesterday it did not.** The setting was shipped in the
  sandbox image, at `/home/agent/.claude/settings.json`. The crew mounts the workspace's own directory
  over that exact path in every sandbox, and a mount hides whatever the image put underneath it, so no
  session ever read it. The feature was green in every gate and did nothing.

  The crew says it now, in the settings file it already renders for hooks and mounts read only. That
  file is the only thing the crew can say to the runtime that a mount cannot hide. Two things follow.
  Every session is given that file, holding no hook where there are none, rather than only the
  sessions running under a hook. And the image writes no settings at all: a test refuses the whole
  class, because nothing here builds that image and nothing would have caught it again.

  A task is told to load the file only when the file is on disk. The runtime refuses to start on
  settings that are missing, saying only `Settings file not found`, and that would be every task on
  the crew rather than one.

- **An attached conversation says how much of the context window it has used.** Attaching puts you in
  the conversation with the model, and the one number that decides whether that conversation is still
  worth continuing was nowhere on the screen. Not in the console, not in the panel's header, and
  asking the model for it costs a task and fills a little more of the window to answer.

  The model runtime keeps a line under the prompt and redraws it every time the conversation moves.
  The crew points that line at `quay statusline` (it shipped pointing there from the sandbox image,
  which no session reads; see the entry above):

  ```
  context 12% used (124k of 1M)
  context 34% used (340k of 1M), over the 30% mark
  ```

  The second one is yellow. Thirty per cent rather than something closer to full, because what you do
  about a filling window (finish the task, compact it, or open a fresh session) is much cheaper
  decided at thirty than at ninety.

  How big the window is, and how much of it the next task carries, are the runtime's to report rather
  than this build's to remember. A runtime that reports neither says so on the line instead of
  guessing, because a guessed window is a confident wrong number and a blank line reads as the crew
  being broken. A session that runs under hooks keeps the line: the hooks file is additional settings
  rather than settings instead.

  A sandbox keeps what it was made with, so this reaches a session that is already running only after
  `make rebuild`, and then stopping that session and dispatching again.

## 20 August 2026

- **A step of a flow runs as a role, in its own session and its own container.** A dispatch node
  takes `role: test-writer`, and that step no longer lands in the run's conversation. It gets a
  session of its own, so the work is done by somebody who has read only what the role declares.

  ```yaml
  tests: { type: dispatch, role: test-writer, prompt: "write the tests" }
  ```

  What that session holds is `receives` and nothing else. A role without `context` is told its brief
  and none of what the crew, the workspace or the project knows. A role without `skills` holds none:
  no index in its memory file, no skill directory mounted. The brief itself is always given, under
  its own mark, rendered every task and never read back.

  Two holes had to be closed for the boundary to be worth anything. Every session in a workspace
  shared one conversation store, so a role that must not see the code could read the transcript of
  the session that wrote it; a role session keeps its own store under the session instead. And a
  session's memory file is read back into the crew's context, so a role given nothing could have
  written what every session in the workspace is told; a role session's file is never read back.

  A step naming a role the workspace does not hold stops the run and says which role is missing,
  rather than walking its success edge on a task that never happened. A run now puts away every
  session it started, not only its own, and counts what every one of them spent against its ceiling.

  The model a role declares is still not read: the runner takes one model per crew. That, the
  product manager, sub tasks and the event trigger are the rest of
  [#354](https://github.com/atlantic-blue/quay-crew/issues/354), and
  [`docs/ROLES.md`](docs/ROLES.md) says which is which.

- **A conversation that cannot be opened says why, and the reason stays on the screen.** Attach is
  usually the whole command of a tmux pane, in the right half of the panel or beside the console, and
  a pane closes the moment its command exits. So a refusal printed its reason and lost it in the same
  instant. Measured against tmux 3.3a, the pane is gone before anything else can even list it. The
  operator pressed the key, the screen flickered, and nothing on it ever said why. The console then
  reported "the conversation opened and tmux does not say where", which is the one thing that had not
  happened.

  Attach stays now. It says what went wrong, names what to do about it, and waits there, the way
  open-conversation already keeps a finished conversation's terminal alive inside the sandbox. Enter
  gives the operator their console back. It reads the terminal rather than asking whether there is
  one, so a scripted attach reads the end of a pipeline's input and comes back at once instead of
  hanging. The console's own message says the conversation closed rather than claiming it opened.

  Part of #380.

- **A session knows how much memory it has.** A session could not run a repository's own gates. The
  linter, the build and the install were each killed part way through, and the session reported a
  partial check.

  The cause is a number that is not true. A sandbox with no memory limit of its own reports the whole
  machine in `/proc/meminfo`, so node sizes its heap from it, Go sizes its collector from it, and
  jest and webpack start one worker for each processor. What is really there is whatever the rest of
  the machine has not taken. Measured in one sandbox: 7836 megabytes advertised, about 1500 free, and
  an allocator killed after taking 1600. The session budgets against the first number and the kernel
  kills it against the second.

  ```
  quay room
  ```

  It says what this sandbox advertises, what is free, what it holds, and what an out of memory killer
  has already taken in it. The kill counts are the part that cannot be got any other way: a kill by
  the machine's own killer raises `oom_kill` and leaves the limit count at zero, so the pair says
  whether the machine ran out or the session did. The kernel log is not readable from inside a
  container, so nothing else in there answers that.

  Then it says what to do about a gate that does not fit: cap the heap under what is free, take one
  worker rather than one for each processor, run the gate over part of the tree and name the part it
  ran. If it still does not fit, say what could not run rather than reporting a partial check. That
  advice is in the tool rather than in each session's memory, so the answer is the same every time
  instead of being invented once per session.

  A task killed for memory says so now as well. Nothing taken by signal 9 gets to say why, so
  `run exited: exit status 137, and it said nothing about why` read as a hang, and it is also what
  an upgrade taking a container away produces. Both are named, with the command that answers which.

  `QC_SANDBOX_MEMORY` gives a session a limit of its own, as the daemon spells it, for example
  `4g`, with swap capped at the same figure so the limit means what it says. Unset, a session has no
  limit, which is where every session already is. The figure shares one machine between the stack,
  the sessions already running and this one, so it is the operator's to choose.

- **The analyser says why it cannot run, instead of shrugging.** It printed `no answer, carrying on`
  on every message, which is what a hook says when it has nothing to add and also what it said when
  it could not work at all. Those are not the same thing, and telling them apart meant reading a file
  in /tmp.

  The cause it was hiding: the subscription token reaches a session but not what the session starts.
  Claude Code removes `CLAUDE_CODE_OAUTH_TOKEN` from the environment of every process it spawns while
  passing nine other `CLAUDE_` variables through, and a hook is one of those processes. So the child
  model call answered `Not logged in · Please run /login` on standard output and exited 1, the hook
  discarded output because the status was not zero, and threw away the one sentence that said what
  was wrong. Measured in a sandbox: the session's own process has the token, its direct child does
  not.

  It now keeps both streams whatever the status, names the cause, and says the next move. Not logged
  in, no claude on the path, a timeout past `timeoutMs`, or anything else the child said, one line of
  it. Four tests cover the messages and one drives the whole hook to check the terminal is told the
  cause rather than that something went wrong. Mutation checked: putting the old shrug back turns it
  red.

  This does not make the analysis work. The credential still does not reach a hook, and until the
  crew hands it one the honest move is `quay hook detach crew prompt-analyser`, which the failure now
  says out loud.

- **`quay dispatch` lets go of the task, and `quay ask` waits for the answer.** The command line held
  every task in the client for as long as the work took, so the terminal was the weakest part of the
  crew: a dispatch killed at seventeen minutes recorded `failed: model: run exited: signal: killed`,
  said nothing about why, and the work was gone. The control plane could always run a task behind the
  answer. Only the console could ask for it.

  ```
  quay dispatch "read the repository and write the migration"
  quay ask "when is the electricity bill due"
  ```

  Dispatching answers as soon as the session exists, and says how to read the work back:
  `quay tasks <session>` for the history, `quay attach <session>` to sit in the conversation. Asking
  waits and prints the reply, which is what a short question wants.

  Two words rather than a flag, because quay takes no flags and a word in front of free text would be
  swallowed into the message: `quay dispatch wait for the build` would have meant something the
  operator did not type. `--detach`, `--wait` and `--no-wait` are each refused by name and say which
  word to type instead.

- **A task is recorded when it starts, so you can see what a session is working on.** A task was
  written to history only when it ended, and only a detached dispatch marked its session `running`. A
  dispatch typed at a terminal is not detached, so a session working for half an hour read `idle`
  with nothing in its history: three sessions ran real work for over thirty minutes each and every
  one of them read that way. One of them showed two finished tasks from three days earlier while the
  task burning its tokens was invisible, its counters climbing from 592 in and 158 thousand out to
  1.2 thousand in and 278.2 thousand out.

  Now every path records the task as it starts, marks the session `running` while it works, and
  writes the reply or the failure into that same record when it lands. `quay tasks` says `still
  running` for a task in flight, the web view says the same where the reply will go, and the
  console's history view carries the status it already had a column for.

  One record per task, not two: the landing closes the row the start opened, so the prompt and the
  time the operator asked stay as they were written. The export to the event log is unchanged, one
  record per task at the end, because a consumer handed the same task twice would have to work out
  which of the two to believe.

  A crew that restarts mid task now says which task died with it, rather than recording a failure
  with no prompt on it.

- **Every identifier a listing prints reaches the session, in an address too.** A listing prints two
  of them. The id has a column of its own, and the handle sits in the name column until a label or a
  description takes that place. So on a session anybody has named, the id is the only identifier on
  the operator's screen, and it was the one form an address refused:

  ```
  quay attach a4db600a                     # worked
  quay attach me/house-bills/a4db600a      # this crew has no session "a4db600a"
  ```

  The address resolver takes either identifier now, whole or shortened, and it still answers with the
  handle, so nothing downstream of an address changes. Every session scoped command shares that
  resolver, which is `quay attach`, `quay dispatch`, `quay tasks`, `quay mode` and `quay label`. A
  refusal names both identifiers, because naming only the handle sent the operator to look for a
  value their screen does not carry. Closes #365, and part of #380.

- **A conversation opens whatever the session's row says.** The row is the crew's own bookkeeping. It
  used to decide whether the operator could open a session at all, and it refused three states.

  A stopped session was refused with "restart it first". That is the one an operator meets most,
  because `make upgrade` drains before it rebuilds, and a drain puts every live session down. After
  an upgrade the whole crew read stopped, so every session in it refused to open.

  An archived session was refused with the same sentence, because archiving sets the row to stopped
  as well and that answer came first. Restarting an archived session is itself refused, so attach
  named the one action that could not be taken.

  A session whose first task failed holds no conversation, and that was refused too. It sat in the
  listing with nothing that could open it.

  All three open now. The row is then brought up to date rather than obeyed: the session comes back
  to idle, an archived one comes back into the listing, and a session with no conversation is given
  one, the way the driver already was. A row that still said stopped would have the next startup reap
  the container out from under the conversation. See issue #380.

## 19 August 2026

- **A session can see what it built.** The sandbox image carries a browser now, and `quay render`
  draws a page into a picture the session then reads. Before this, a change with a visual result was
  delivered on the strength of a passing build: the build, the linter, the type check and the tests
  all pass on a layout that is wrong, and the operator was the first person to look at it. On one
  site that meant a spacing defect on every section of the home page, found by the first screenshot
  anybody took.

  ```
  quay render http://localhost:3000
  quay render localhost:3000 home.png 390x844 dark 2s
  ```

  The url comes first and everything after it is recognised by its shape rather than its position, so
  a file name, a size, `light` or `dark` and a wait can be typed in any order. It draws the whole page
  rather than the first screen of it, waits after load so a page that draws itself in script is not
  caught blank, then reads the picture back and says what it drew and how big it is. A browser that
  exits well and writes nothing is a failure rather than a report, because a session cannot tell
  those two apart from the exit status.

  The image installs Playwright and the headless browser it drives, pinned like everything else in
  there, with the browser's dependencies installed first so a missing shared library fails the build
  rather than a session. The stated cost is about 450 megabytes on the image.

  A `browser` skill goes with it, which says the part no command can enforce: a picture nobody looks
  at is worth nothing, and a rendered sample presented as observed output is worth less than nothing.

  A sandbox keeps what it was made with, so a session that was already running when this landed has
  no browser. Stop it and dispatch again.

## 18 August 2026

- **Upgrading no longer takes a task away from under a session.** `make upgrade` removes every
  sandbox container by name, because a container built from the old image is a session a build behind
  and its name blocks the new one. Done from the daemon that removal took whatever was working with
  it: the operator read `model: run exited: exit status 137, and it said nothing about why` against a
  conversation they had been watching, and nothing anywhere said an upgrade had done it.

  The upgrade now asks the crew to put its sessions down first. Each one is stopped properly, so the
  row says stopped, which is true, and the sandbox is closed rather than ripped out. A task still
  working refuses the whole upgrade and names the session it belongs to, so you can wait for it. Say
  `make upgrade FORCE=1` to go over it, and the drain says whose task went.

  It is a command of its own too: `quay drain`, and `quay drain anyway` over a task in flight. A crew
  that is not up, and a crew from before this existed, both say so and let the upgrade carry on
  rather than blocking it.

- **A secret every workspace needs is held once by the crew.** Say `crew` where a workspace goes:
  `gh auth token | quay secret set crew GH_TOKEN`, `quay secret mount crew gitconfig < ~/.gitconfig`.
  Every workspace then reads it, including the ones made afterwards, which is the difference between
  setting a crew up once and setting each workspace up again. Both projections work, so a shared
  token and a shared credential file both come from one place.

  A workspace wins on a name. The crew's level is what every workspace gets by default, not a floor,
  so the one workspace that needs a different token sets its own and no other workspace loses the
  shared one. The merge happens in one place rather than in each backend, because two readers of the
  rule are two chances to disagree about which level wins.

  `quay secret list` says which level holds each one, and `quay secret list crew` asks for the
  crew's alone. Removing a workspace no longer counts the crew's secrets among what it takes with it:
  they belong to no workspace and survive every one of them.

  No workspace may be called `crew` any more. That word is what every address takes for the level
  above a workspace, so a workspace with that name would take the secrets, skills, hooks and roles
  meant for all of them, and nothing else would ever read them.

  Removing a secret is still not possible, at either level, the same as before this.

## 17 August 2026

- **A role is imported, pinned to a version, and attached at a level.** A role is a named way of
  working a session is given: a brief the model reads, the model it runs on, and the material it is
  allowed to receive. `quay role import`, `quay role list`, `quay role attach` and `quay role detach`,
  which is the shape a skill and a hook already have, at the same two levels.

  The point of it is the boundary rather than the persona. A flow sends work to one session today, so
  the session that writes the code has already read everything the session that planned it said, and
  a second opinion that read the first one is not a second opinion. So a role declares what it
  receives, from three words the crew actually assembles: `work`, `context` and `skills`. A fourth is
  refused at import by name, because a boundary that means nothing looks exactly like one that holds.

  A role's brief may be four pages where a skill's may be one, and the reason is who pays: a skill's
  line reaches every session on every conversation, and a role's brief reaches one session once.

  Importing, attaching and detaching a role are refused to the driver's token, on the same line as a
  skill. A role carries a brief, a model and the material a session receives, so a session that could
  attach one could write itself a way of working nobody approved and then be run as it. Reading what
  the crew holds stays open, because choosing from what the operator attached is the point.

  Nothing runs as a role yet. Attaching one changes what the crew may be asked for and changes
  nothing about a session already open. The design is in [`docs/ROLES.md`](docs/ROLES.md) and the
  rest of it is [issue 354](https://github.com/atlantic-blue/quay-crew/issues/354).

- **A repository is cloned once for the workspace, and each session works in a tree of its own.** The
  git brief said to clone into your working directory, and that directory belongs to one session, so a
  workspace working in one repository across four sessions held four clones of it. Driving a live crew
  found it the plain way: one session cloned, and the session beside it saw an empty room. The volume
  every session in the workspace shares sat empty, because no brief had ever mentioned it.

  The clone goes in `/home/agent/shared/repos/<name>` now, and each session takes a working tree at
  `/home/agent/shared/worktrees/$QC_SESSION_ID/<name>` on a branch of its own. `QC_SESSION_ID` is new:
  every sandbox carries the identifier of the session it is, which is what a session names anything of
  its own in a shared directory after.

  The tree has to sit under the session identifier, and that is the part that is not obvious. A clone
  records where its working trees are, every sandbox sees the same paths, so two sessions adding a tree
  at one path register one path between them and the second takes the first away. A test runs real git
  over the layout and proves both halves: two trees out of one clone, and the collision that happens
  when the path is shared.

  The stated cost: nothing removes a working tree when a session ends, so the volume keeps a directory
  per session that ever worked in a repository. That half of
  [#255](https://github.com/atlantic-blue/quay-crew/issues/255) is still open. This is a convention in
  a brief rather than machinery, so a session that ignores the brief still clones wherever it likes.

## 16 August 2026

- **`make up` starts everything. There are no compose profiles left.** Grafana, Loki, Tempo and
  Prometheus sat behind an `observability` profile and Redpanda behind an `export` one, each to keep
  a laptop light. What that bought was a crew you could not see and an audit export nobody had
  switched on, plus two more commands to know exist. A signal nobody starts is a signal nobody has.

  `QC_KAFKA_SEEDS` now defaults to `redpanda:9092`, so the export is on rather than merely possible.
  Set it to empty in your own configuration to turn it off, which loses nothing: the store is the
  source of truth and the log only ever carried a copy.

  The collector keeps its queue and its retry again. They were off because the stores behind them
  might not be running; now they always are, so a batch that arrives while one is still coming up is
  held and delivered rather than dropped.

  `make up-observability` still works and says it is now the same as `make up`, because it is in
  fingers and in notes. `deploy/telemetry_test.go` refuses any service that goes back behind a
  profile, so this cannot return one service at a time.

  The cost is four more containers on every `make up`. That is the trade, stated here rather than
  discovered.

- **Setting up a workspace is written down in one page.** The knowledge was there and it was
  scattered. The README made a workspace in four commands, `docs/SANDBOX.md` covered the image and
  the two credentials that are files, `docs/SKILLS.md` and `docs/HOOKS.md` each covered their own
  subsystem, and `quay manual` listed every command. Nothing took an operator from nothing to a
  workspace whose sessions can read a repository and commit as them.
  [`docs/WORKSPACE.md`](docs/WORKSPACE.md) does that: the two ways a secret reaches a session and
  which to choose, who a session commits as, the four levels of context and the two files they land
  in, the shared volume, what happens about repositories, skills, hooks, and which changes need a new
  sandbox.

  Two corrections came out of writing it. `docs/SANDBOX.md` named two mounted directories where the
  code mounts three, so the workspace volume at `/home/agent/shared` was documented nowhere, and it
  printed a working directory of `projects/<project>/workspace`, which moved under
  `sessions/<session>/` when a working directory became a session's rather than a project's. The
  README said credentials are set through the dashboard or the API, which is not how anybody sets
  one.

- **A session can sign with your own gpg key.** Signing worked and signed as somebody else. The crew
  took one key, an ssh one, so a commit made in a sandbox carried a second identity on the same
  history as the commits made on the operator's laptop, and the account had to know both.

  A workspace can mount `GPG_SIGNING_KEY` now, the armoured export of the key you already sign with,
  and the session signs as you do everywhere else. `GPG_SIGNING_KEY_PASSPHRASE` goes beside it for a
  key that has one, which is most of them. Both are mounted, never set, on the same terms as the ssh
  key: a private key in a container's environment is readable through `docker inspect` for the life
  of that container, and the passphrase that unlocks the key is worth what the key is worth.

  What ssh avoided and this brings back is the keyring and the passphrase prompt. The keyring is made
  at sandbox birth in `/dev/shm`, which is memory, per container, and gone with it, so an imported key
  never reaches the writable layer the daemon keeps on disk. The prompt is answered by not having
  one: gpg runs in batch with no terminal, so a key whose passphrase the workspace did not mount fails
  in a second with a message rather than hanging a task nobody is watching.

  Nothing is enforced anywhere. A workspace mounting no key does not sign and nothing fails, which is
  what it did before. A workspace mounting both kinds signs with the gpg one.

  The image carries `gnupg` for this, because git makes an OpenPGP signature by running gpg, and the
  earlier measurement of an image without it recorded exactly that: `cannot run gpg`. Two integration
  tests make a real commit in a container the crew built, one with a passphrase and one without, and
  check the signature carries the fingerprint of the key the workspace mounted rather than any key.

- **The archived view shows a whole name.** The column was ten characters wide, from when it held
  the first eight of an identifier. A name is a sentence, so "the electricity bill" read as "the
  elect…". It takes what is left of the row now, as the live sessions view does, and the stamp beside
  it is a fixed eight. Two columns that both take what is left are each given the whole of it, which
  draws a row wider than the panel, so a test now holds every view to one flexible column.

- **The archived view names its name column.** The cell holds what a session is called. The header
  above it read `session`, which is what an identifier is called. So an operator read the header,
  looked for eight characters of hexadecimal, and found a sentence. The live sessions view has said
  `name` since the name shipped. The archived view kept the old header.
- **A task says what it cost.** Three instruments now leave the crew after every task: tasks run,
  tokens spent split into input, output, cache read and cache written, and what those tokens would
  cost at published prices. Each carries the workspace, the project, the model and the status, by
  name rather than by identifier, because nobody groups a cost dashboard by a uuid.

  The numbers were already arriving and being thrown away. The model runner calls the command line
  tool with `--output-format stream-json`, and the result event on that stream carries both the usage
  and the cost. The struct that reads the stream did not have the fields, so the one number that says
  whether a crew of agents is affordable was discarded at the point it was known.

  The cost is not a charge anybody receives. The crew runs under a subscription, so this is the
  model's own tooling pricing the task at published rates, which is exactly the figure worth
  watching.

  A task whose backend reported nothing is counted as a task and contributes to neither of the other
  two, so an unknown never reads as a zero. A task that failed is counted with `status="failed"` and
  contributes no tokens, because a failed task returns nothing to read them from; what it spent
  before it failed is still invisible.

  Reusing `sandbox.Usage` rather than inventing a second set of names for the same four numbers: the
  crew already reads that shape off a conversation's transcript for "what has this thread cost". This
  is the per task figure, which is what a counter wants.
  ([#16](https://github.com/atlantic-blue/quay-crew/issues/16))

- **Logs reach Loki, and a log line and its trace link to each other.** The crew's log lines now go to
  the collector as well as to stdout, so Loki holds them. A line carrying a correlation id has a link
  on it that opens the trace, and a span offers the lines that call wrote. That link is the whole
  point of the correlation id: without it the id is a string somebody copies by hand, which is the
  same as not having it.

  It is a copy and not a move. A container's stdout is what you read when the collector is the broken
  thing, so it keeps carrying every line whatever happens downstream. The first line each service
  writes goes to stdout only, because it is written before the exporter exists.

  Loki takes OTLP directly, so there is no Loki specific format to keep working, and no log shipper
  reading the host's Docker directory.
  ([#12](https://github.com/atlantic-blue/quay-crew/issues/12))

- **The telemetry stack carries traces, and Grafana comes up joined.** `make up-observability` started
  four containers that had nothing to do with each other. The collector's only exporter was `debug`,
  which prints a summary and drops what it was given. Prometheus had no configuration file at all, so
  it fell back to the image default and scraped only itself. Grafana had no data source, so it opened
  onto nothing. Every part was up and the picture was empty.

  Traces now go to Tempo, the collector republishes metrics for Prometheus to scrape, and Grafana's
  three data sources are provisioned from a file in this repository rather than added by hand on each
  machine. Dispatch a turn, open Explore, pick Tempo, and the turn is there as one span.

  The observability profile is still a profile, so a plain `make up` runs the collector with nowhere
  to forward to. The queue and the retry on the Tempo exporter are off for that reason: an
  undeliverable batch is dropped with one line rather than held and retried forever.

  Loki is provisioned and empty, because the services log to their own stdout and nothing forwards it
  yet. Prometheus scrapes a real endpoint that publishes an empty set, because nothing creates a
  metric instrument. Both are said out loud in `docs/OBSERVABILITY.md` rather than left to be
  discovered.

  `deploy/telemetry_test.go` holds the halves together: a host that is not a service in the compose
  file, a pipeline that reaches nothing but `debug`, a Grafana data source pointed at a store that is
  not there, or a scrape port that is not the one the collector publishes on all fail here rather
  than as an empty dashboard.
  ([#12](https://github.com/atlantic-blue/quay-crew/issues/12))

- **A call can be followed after it happened.** Every message the control plane serves now runs in a
  span, and every log line written while it runs carries a `correlation_id` equal to that span's
  trace id. Take an id off a log line and you have the trace; open the trace and you can filter the
  logs back to it.

  The tracing was wired and unused. `telemetry.Init` built a tracer provider and an exporter on
  startup, nothing ever started a span, and so the collector had received nothing since the stack was
  written. It is a stats handler rather than an interceptor, because a stats handler runs before the
  token guard: a refused call is traced too, and that is the call somebody comes looking for.

  The logging half was worse than unused, it was misleading. Each service built a JSON logger and
  never made it the default, while every line inside `internal/` is written through the package level
  `slog`. So the crew's own log lines, the ones that say a secret could not be mounted or a turn could
  not be exported, were going out in Go's default text format while the documentation said the
  services log structured JSON. They do now.

  A line only carries the id when the call site logs with a context, so those call sites now pass
  one. The id survives `context.WithoutCancel`, which is what a turn and a flow run detach with, so
  the half of a turn that outlives the request is correlated rather than orphaned.

  Metrics are still wired and empty, the collector still exports to `debug` only, and nothing is
  traced beyond the crew's own handling of one message.
  ([#3](https://github.com/atlantic-blue/quay-crew/issues/3))

- **A graph can tell work that was done from work that was explained away.** The first flow run
  against a real crew finished at `done`, reported four transitions and 669,649 tokens, and read back
  as a success. None of the work happened. The repository was not in the run's session, every task
  said so in its own words, and the run took the success edge anyway, because the only signal a
  `choice` node had was `result.failed`, and `result.failed` says the model did not error. A task
  that could not do the work is not a failed task. The more capable the model, the worse it reads: it
  answers plausibly rather than stopping, and that plausible answer is what the run ends up carrying
  as its summary.

  A dispatch node now says what will show it worked, and the crew checks it rather than reading the
  model's account of itself. `expect: { file: package.json }` is a path that must be in the run's
  session after the task, read from the working directory the crew already keeps, so nothing the model
  says can satisfy it. `expect: { contains: "all green" }` is a string the reply must carry, weaker
  because it is still prose, and there for work that leaves no file behind.

  An expectation that does not hold stops the run, naming the node and what was not there. It stops
  rather than branching, because the crew knows the work did not happen and does not know why, and
  because a run that halts is read correctly while a run that finishes is believed. The session is
  left alone rather than archived: that is where the evidence is. An expectation nothing could check
  stops the run too, since a check that quietly passes when nobody could look is the same false green
  as no check at all. A graph that declares nothing behaves exactly as it did.

  Left out on purpose: a command the crew runs and requires to exit zero. That makes an imported
  graph a way to run arbitrary commands through the control plane, which is a decision to take on its
  own. ([#263](https://github.com/atlantic-blue/quay-crew/issues/263))

- **A graph says what its runs may do, and the documents say what a run starts with.** A run owns its
  own session, and that session is made by the run's first dispatch. So there was nothing to set a mode
  on before the run started, `quay mode` had nothing to point at, and every automation ran in the
  mode a session is born in. A graph whose first step is "clone this" could not take that step:
  cloning needs the network, and a task nobody is watching has nobody to approve it.

  A graph now declares `mode: dangerous` beside its name and its version, and every dispatch of the
  run carries it. The word is checked at import, against the same table the command line and the
  console read, so a mode nobody has is refused with the three there are rather than failing on the
  run's first task. A graph that declares nothing is unchanged.

  The other half is a document. Nothing said what a run's session starts with, which is exactly the
  assumption a graph author makes without noticing: it starts empty. `docs/ARCHITECTURE.md` now says
  so, and says which of the two mounted directories survives the run. The workspace's volume at
  `/home/agent/shared` is the one that does, so a graph that needs a repository puts it there.
  Cloning into the working directory pays for the clone on every run.

  What this is not is the clone that serves every session, which is
  [#255](https://github.com/atlantic-blue/quay-crew/issues/255) and is still open. A run can fill its
  own room now; it does not yet find one already filled.
  ([#264](https://github.com/atlantic-blue/quay-crew/issues/264))

- **What a finished flow run did can be read again.** A run archives its own session when it ends,
  which is right: otherwise every run leaves a container behind. Archiving is also what put the run's
  record out of reach. `quay flow show` printed the session identifier, and `quay tasks` refused that
  exact identifier, because every command that reads a session asked the live listing and nothing
  else. The tasks were in the store the whole time. Reading them took `psql`.

  That mattered more than a missing convenience. The run's summary is the model's own account of what
  happened, and the tasks are the record of it. The two can disagree: the first run against a real
  crew reported four transitions and a tidy summary while every task under it said the working
  directory was empty.

  So a session is resolved against the archived listing when the live one does not name it. The live
  listing is asked first, so an identifier that names a live session still names the same one. Nothing
  about reading a history needs the session to be live, and `quay attach`, which does need it, still
  refuses on its own terms and says to restore it first. `quay flow show` now prints the command that
  reads the run's tasks rather than an identifier to work it out from, and the console's archived
  view takes `l` for a history, the same key the live view takes.
  ([#265](https://github.com/atlantic-blue/quay-crew/issues/265))

- **An archived session runs nothing.** Archiving stops a session and takes its container away. The
  task that was running in it landed a moment later, and the crew wrote down what that task came to,
  so the row went back to idle or to failed. The archived listing then showed a session that is
  working, and nobody can reach it.

  An archived session keeps its status now. It still keeps its conversation handle, so restoring it
  comes back to the conversation it was in.

  A dispatch to an archived session is refused too, and says to restore it first. A handle is matched
  whether the session is put away or not, so `quay dispatch` to an archived session started a container
  for a session that is not in the listing.

- **Restart works on a live session, and asks first.** The crew refused to restart a session unless it
  was already stopped. So the key you press when a container is wrong did nothing, and you had to
  stop the session yourself before the key would work.

  Restart stops the session now, removes the container, and starts a new one. In the console it is `R`
  or ctrl+r. It asks before it acts on a session that is not stopped, because the task in that session
  and the conversation attached to it go with the old container. A stopped session has neither, so
  that one acts at once.

  An archived session is refused, and says to restore it first. An archived row says stopped, so a
  restart that read only the status started a container for a session nobody can see.

- **Every view in the console is coloured cell by cell.** A row carries a state, and the state was
  drawn over the whole line, so a row's workspace, project, name and mode all arrived on screen in
  the same colour. Nine of the ten views set a state on every row they list, so nine listings came
  out flat and the tenth came out in two colours. The cell colours were there the whole time and
  nothing could reach them.

  The state lives on the status cell now, which is where the sessions tool keeps it: green for idle,
  yellow for running, red for failed, dim for stopped. A failed row is the one exception and still
  takes the whole line, because it has to read as failed before it reads as anything else.

  Age takes the same three bands as that tool's idle column, so a session touched a moment ago, one
  from this morning and one from last week are told apart without reading the number. The views that
  had no cell colour at all have one now: names carry their own colour, identifiers and counts are
  dim, a key is cyan wherever it is read. A case sweeps every registered view, so the next one added
  cannot be the flat one.
- **`quay web`: the crew reads in a browser, on this machine only.** A terminal pane is a poor place
  to read a long reply with code in it, which is the one gap
  [#302](https://github.com/atlantic-blue/quay-crew/issues/302) found that the console cannot close
  and no other plan already claims. The command serves `127.0.0.1:8080`, lists every live
  conversation, and opens one to read its tasks in the order they happened.

  It reads and cannot do anything else. The server holds an interface naming five calls, all of them
  `List` or `Get`, so a handler here cannot dispatch a task or delete a workspace: there is no method
  to call. A test walks that interface and fails on any name that is not a read, which makes it a
  rule about the whole class rather than about the five calls that exist today.

  It refuses to bind anywhere but this machine, `:8080` and `0.0.0.0` included, because the control
  plane behind it is a local only port guarded by one shared token and this server holds that token.
  Reaching the crew from another device needs a token for each device, a way to withdraw one, and a
  rule about encryption. The crew has none of the three, and a chat channel is the road planned for
  that need, so this refuses rather than deciding it by accident.

  The pages are Go templates and one stylesheet, embedded. No package manager, no build step and
  nothing fetched from the internet, so the binary stays one static file and the page works with no
  network. The design tool's export loads Tailwind and a web font from a content delivery network;
  its tokens were copied and its dependencies were not.

  This slice is plain on purpose. The design system, replies rendered as formatted text with coloured
  code, what a task cost, and following a session as it works are
  [#331](https://github.com/atlantic-blue/quay-crew/issues/331),
  [#332](https://github.com/atlantic-blue/quay-crew/issues/332),
  [#333](https://github.com/atlantic-blue/quay-crew/issues/333) and
  [#334](https://github.com/atlantic-blue/quay-crew/issues/334). Every action that writes is out of
  all of them. ([#330](https://github.com/atlantic-blue/quay-crew/issues/330))

- **A task says which model to run, and it runs Opus.** The crew never passed `--model`, so the
  choice belonged to the command line tool, and the tool chooses Sonnet. Every session on this crew
  was running Sonnet 5 against an Opus subscription, and nothing anywhere said so.

  `QC_CLAUDE_MODEL` in `~/.quay/env` now, defaulting to `claude-opus-5`. The default sits in the
  compose file rather than in the code, so a configuration written before the key existed gets it
  anyway; `make env-check` still names it, so you can put it in your own file and change it. A full
  name rather than the `opus` alias, for the same reason the tool itself is pinned below: an alias
  moves to the next generation on its own and nothing records that it did.

  `QC_MODEL` is unchanged and is a different decision: it picks the backend, `claude-code` or `echo`.

- **The sandbox image pins Claude Code.** It was installed at whatever the registry called latest on
  the day the image was built, so two builds of the same commit produced two different images and
  nothing recorded which one a session ran. `gh`, terraform and the AWS command line were already
  pinned; the model runtime, the one thing a session cannot work without, was not.

  It is `CLAUDE_CODE_VERSION` now, and raising it is a deliberate change with a commit behind it.
  Two checks: one refuses any global install in this image that does not name a version, so the next
  tool added unpinned fails here rather than months later, and one asks the built image what it runs,
  because a pin the registry quietly ignores reads exactly like a pin that works.

## 16 August 2026

- **A turn is called a task.** "Turn" is a word from conversation analysis, where it means one party
  holding the floor. It arrived in this product the way it arrived in every model provider's
  documentation, already meaning something, and it never fitted: a turn in conversation is short, and
  here holding the floor means reading a repository for ten minutes. The word implied an exchange and
  the thing is a piece of work.

  The glossary made it worse. It defined five words and then defined `thread` with a sixth it never
  defined anywhere: "one conversation. A turn runs in a project." The one place that exists to teach
  the vocabulary used the undefined term to explain a defined one.

  So `quay turns` is `quay tasks`, the console's history view is `tasks`, and the glossary gains the
  entry it never had, including the part nobody was told: minutes is normal. `quay turns` is refused
  by name and says what to type instead, rather than becoming an unknown command, because it is in
  fingers, in scripts and in notes. The console keeps `turns` as an alias, since muscle memory
  outlives a rename and answering to it costs nothing.

  The protocol, the store and its table still say turn. That is the next change, and it is invisible
  from outside.

## 15 August 2026

- **The prompt analyser is Go, and it has tests.** It was 672 lines of TypeScript across two files
  with no test of any kind behind them, run by node through a shebang that stripped types at load.
  Every rule it applied, which lines of a model's answer survive, what a half written config file
  falls back to, which variables the child inherits, was readable only by reading the hook and
  hoping.

  It is 746 lines of Go across two files, so the hook itself is the same size, and it is its own
  module because a hook is a plugin rather than part of the crew: it does not share the crew's
  dependencies and cannot import its internals. The standard library is all it needs. 47 tests cover
  the parts that were bare before, and the two that carry the most weight were mutation checked:
  dropping the guard from the child environment, and keeping every line of the model's answer
  instead of the known fields, each task the suite red.

  The entry point is `bin/hook`, built by `make hooks` and by the image build rather than committed.
  A hook is an executable, an executable is a build artifact, and one committed binary runs on one
  processor type while this image is built on both arm and amd machines. `node` is off the hook's
  list of what it needs, leaving `claude` alone.

  Behaviour is unchanged, and it was checked by running the built binary the way the runtime does:
  the message as typed and the analysis both reach the session, the guard and the zero thinking
  budget reach the child, the subscription token survives while the session's own variables are
  dropped, and a missing model, an unreadable payload and an empty message each end in exit 0 with
  the message still getting through.

- **Tab cycles the wizard's options.** Every question the wizard asks that has a fixed set of
  answers, what to make, which workspace, which mode a session may start in, took the answer as
  typed text and offered the choices only as a hint beside it. Naming a session's mode meant
  spelling "dangerous" correctly. Tab now fills in one candidate at a time, in the order they are
  offered, and shift tab goes back; enter accepts whatever tab last landed on the same way it
  accepts anything typed by hand. Typing still narrows the list the way it always has, and tab
  cycles inside whatever is left.
- **Go, in the sandbox image.** A session working on this repository could read the Go source and
  never run it: `make fmt`, `make lint` and `go test` all need the toolchain, and the sandbox
  carried none. Copied from the stage that already builds `quay` rather than downloaded again, so
  the sandbox never carries a Go that disagrees with the one `quay` itself was built with.
- **The signing key is mounted, not set.** It was a workspace secret that reached the environment, so
  the private key sat in every container's environment for the life of that container, where
  `docker inspect` reads it. That was the exposure the file projection was built to remove, and the
  key is the most sensitive thing this crew carries.

  `quay secret mount <workspace> GIT_SSH_SIGNING_KEY ~/.ssh/id_ed25519` now, and the crew only points
  git at where the file lands. The write that put the key on disk by hand is gone with it, and so is
  the crew ever holding the value. Setting the key is refused, and the refusal says what to type
  instead: a key that looks stored and never signs anything is worse than one that was never
  accepted.

  Nothing to migrate. Checked before deciding: no workspace on this crew holds a signing key, so
  nobody had opted into signing at all.

- **An operator's git configuration reaches a session.** A session commits as the operator and had no
  way to know who that was. Identity was four environment variables set on the task's own process, so
  a commit made from an attached terminal, or by anything the session started for itself, had none,
  and git refused it with `Author identity unknown`.

  The image now ships a git configuration holding one line, an include pointing at where a mounted
  secret named `gitconfig` lands. `quay secret mount <workspace> gitconfig ~/.gitconfig` gives every
  git process in the sandbox the operator's own identity, aliases and settings, from any shell. A
  workspace that mounts nothing is unchanged, because git ignores an include that is not there.

  Signing is the one part the crew decides rather than the operator. Most configurations that sign
  have it on for everything, against a key the machine holds and a container does not, so a workspace
  holding no signing key is now told not to sign rather than told nothing. Left alone, a mounted
  configuration fails every commit: with that half taken back out, the same test dies with
  `cannot run gpg`, which is what the measurement showed.

- **A session can sign a commit.** Signing landed on 13 August and never worked. Git makes an ssh format
  signature by running `ssh-keygen`, and the sandbox image did not carry it, so a workspace with a
  signing key configured could not commit at all: git answered `cannot run ssh-keygen` before it read
  the key. Every commit in every sandbox failed, whatever the key was. The image now installs
  `openssh-client` alongside git, and an integration test makes a commit in a container the crew
  built and checks git verifies the signature against the public half of the workspace's key. Run
  against the image without the package, that test reproduces the original failure exactly.

- **A secret can reach a session as a file.** Some credentials are not values. A git configuration, a
  private key, a cloud credentials file: a tool opens each one by path, so there was nothing a crew
  could do for them. One credential had already been forced through, the ssh signing key, by a script
  written for that one case.

  A secret now says how it reaches a sandbox, which is the shape Kubernetes and Docker both settled
  on: the store holds bytes under a name, and whether those bytes become an environment variable or a
  file is a separate choice. `quay secret set` is the environment and stays the default, so nothing
  already set moves. `quay secret mount gitconfig ~/.gitconfig` is a file, landing at
  `/run/secrets/gitconfig`, and `quay secret list` says where each one goes.

  A mounted secret does not also reach the environment, and that is the second reason to mount one.
  A container's environment is readable for the life of that container, through `docker inspect`
  among other things, which `docs/SANDBOX.md` has recorded as an accepted cost since the subscription
  token landed. The directory is created with the container, memory backed, owned by the sandbox user
  and shut to everybody else, so a mounted value never reaches the container's writable layer or the
  host's disk. Proved against the real daemon and the real image: without the owner on the mount the
  write is refused, which is what the measurement showed before the code was written.

- **A fix to a shipped hook can now reach a crew that already has it.** Seeding both imported and
  attached, and both only into a crew holding no hooks at all, which is no crew that has ever been
  used. So the analyser's credential fix above landed in the repository, shipped in the image, and
  reached nobody: an upgraded crew kept running the version it was seeded with, and the only way to
  see that was to read the hook's own file inside a container.

  The two halves are separate now. Importing runs on every start, so a newer version of a shipped hook
  reaches the catalogue of any crew that upgrades. Attaching still runs only into a crew that held
  nothing, so an operator who took a hook off keeps it off, and an upgrade never moves a crew onto a
  newer version of a constraint by itself. A hook is pinned so it cannot change under a running
  session, and `quay hook attach` is how somebody decides to take the new one.

- **The analyser's child model call keeps the credential it needs.** It shipped stripping every
  `CLAUDE_` variable before running its child, so the child would not inherit what the running session
  set for itself. On a machine with a logged in install that costs nothing, because the credential is
  a file. A quay sandbox has no credentials file: the workspace's subscription arrives as
  `CLAUDE_CODE_OAUTH_TOKEN`, and it was being dropped.

  Nothing looked wrong. The hook ran in 946 milliseconds, exited 0 and let the message through,
  because it fails open by design. The child exited 1 with an empty standard error, and the only sign
  anywhere was the word "no answer" in a file in `/tmp`. Found by dispatching a task on a real crew and
  reading what the hook actually wrote, not by any test.

  A stub on the path now stands in for the model in an integration test, so the token's arrival is
  proved without a subscription.

- **The prompt analyser is the crew's first hook, and every crew starts under it.** It reads the
  message a session was sent, asks a small model to restate it, and hands the session the message and
  that restatement together. It never replaces what was typed: the runtime does not allow that, and it
  should not, because a reading of a message is a guess and the words are not.

  It goes first because it cannot be wrong in the expensive direction. Every other hook worth having
  refuses something, and one that refuses wrongly blocks the work. This one only adds, so it is the
  one hook a fresh crew is given rather than merely offered. Taking it off and restarting leaves it
  off: seeding runs only into a crew that holds none, because putting a constraint back is the crew
  overruling the person operating it.

  What it reads inside a sandbox is not what the same hook reads on a laptop, which is why the paths
  are configuration rather than code: the skills at `/home/agent/skills`, and what the session was
  told at `/home/agent/.claude/CLAUDE.md`.

  One defect worth recording, because every test was green when it shipped and it failed on the first
  message. Node decides whether to strip TypeScript types by the file extension, not by the flag in
  the shebang, so an entry point named `bin/hook` was read as plain JavaScript and died on its own
  type imports with `SyntaxError: Unexpected identifier 'AnalysisFacts'`. It is `bin/hook.ts` now,
  there is a test that any entry point using type stripping is named so node strips them, and there is
  another that runs the shipped analyser inside the real sandbox image and reads what it says.

- **A crew can enforce a rule, not only ask for one.** Every rule a crew carries was context, and
  context is advice the model takes or leaves. The evidence is one working session: 100 kilobytes of
  rules in the context, three of them broken, and the one it did not break was the one a hook refuses
  on the operator's machine. A quay sandbox had no such gate.

  A hook is now content the crew holds, the same shape as a skill: a directory with a manifest and an
  executable, imported with `quay hook import`, pinned to a version, attached to a workspace or to the
  whole crew with `quay hook attach`. It is a third entity beside a skill and a workflow, and keeping
  them apart is the point. A skill is a capability, what a session **can** do, and it is passive. A
  workflow is a plan, with control flow, state and a durable run. A hook is a constraint: no state, no
  say in what happens next, and never in the model's context. Moving a checkable rule out of the
  prompt makes the advice that stays behind stronger.

  What a hook fires on is an allow list rather than free text, and that is the refusal worth knowing
  about. A misspelled event imports, attaches, mounts and is never called, and a hook that is never
  called cannot be told from one that approves of everything. The import is the only moment anybody is
  looking, so it is refused there, by name, with the events that exist.

  The files are mounted read only at `/home/agent/hooks`, and a settings file the crew owns outright
  is rendered beside them and loaded with `claude --settings`. Not into the conversation directory's
  own settings, which the runtime writes and the operator edits: that would mean merging on every task
  and losing an edit the first time the merge was wrong. Both the dispatched task and the attached
  conversation load it, because a gate that only runs on dispatched tasks is one you walk around by
  opening the session.

  A hook reaches a container when the container is built and never after, so `quay hook attach` says
  so every time. Somebody who believes a gate is on when it is not is worse off than somebody who
  knows there is no gate.

  Proved against a real container rather than a double: the command the settings file names is run
  inside the container, by absolute path, and the mount is checked to be read only by trying to
  rewrite the file that binds the hooks. A session that can edit what constrains it is not constrained.

- **Starting a session from the console no longer waits for its first task.** Creating a session in
  the wizard failed, and the operator read the failure as the container being slow to start. Neither
  half was true. The wizard put a thirty second deadline on the call, and a dispatch runs the whole
  model task before it answers, so any first message worth sending was killed by the deadline. What
  was left behind was a session with a container, a row, and no conversation in it. Measured on the
  machine that reported it: a bare `docker run` of the sandbox image takes 0.24 seconds, and a
  command line dispatch that created a fresh container and answered a short prompt took 4.0 seconds.
  The wait was the wizard holding the screen, not the daemon.

  A dispatch can now detach: it answers as soon as the session exists and runs the task behind the
  answer. The console uses it, so the wizard closes at once and the row says `running` until the task
  lands. The command line and the flow engine still wait, because both want the reply. Driven for
  real afterwards through the console: the wizard came back in under a second, and the task ran for
  87.56 seconds, nearly three times the deadline that used to kill it, then landed as `idle`.

  Two things fall out of it. A task runs in the crew's own process, so a session the store still calls
  running when the crew starts is one whose task died with the last process; startup settles those
  rather than leaving a conversation that appears to have been thinking since the restart. And a
  graceful stop now waits for tasks as well as for calls, because a detached task is a goroutine and
  draining requests does not drain it.

- **A failed task says what actually failed.** Every failure read "the model did not complete the
  task", so a deadline, a crash and a refusal were one line with nothing to act on in it. That
  sentence is what sent this bug to the wrong place: the record said the model had not finished, when
  the caller had stopped waiting. The reason is recorded now, and a task killed by a deadline or a
  cancellation is named for what happened to it rather than for the plumbing underneath.

- **A session says what it is about, written by the crew.** A label fixes a listing only for the
  sessions somebody stopped to name, and naming things is work nobody does consistently. A session now
  describes itself after its first task, and again once the conversation has moved ten tasks past that
  description. A listing prefers your own name when there is one, then the crew's, then the
  identifier, and nothing automatic ever writes the name you chose.

  The describing call is its own conversation, not the session's, so a request you never made does not
  land in your history and its tokens do not count towards what the listing says the session cost. It
  runs behind the answer, so a task is never slower for it, and every failure in it is a log line:
  a task that worked is not reported as failed because the crew could not think of a name.

  Ten is a starting number, not a measured one. Nothing has run long enough to say how far a
  conversation drifts per task, so it is `QC_DESCRIBE_EVERY` in the crew's configuration rather than a
  number presented as derived, and what would replace it is a count of how often a rewrite actually
  differs from the description before it. `QC_DESCRIBE_EVERY=off` tasks it off, which a crew running
  automation wants, since a flow makes a session per run.

  What comes back is checked against the question that was asked, line for line, and discarded when it
  is the question rather than an answer to it. A backend that echoes hands the instruction straight
  back, and continuous integration runs one, so without that check every session in a composed stack
  was named "Here is the start of a conversation:". The check matches whole lines rather than any
  occurrence, because the question carries examples of a good answer and a model that produced one of
  those examples exactly would otherwise have it thrown away.
  ([#271](https://github.com/atlantic-blue/quay-crew/issues/271))

- **A session carries a name you chose.** A listing was a column of hexadecimal, so working out which
  conversation was the one about the electricity bill meant opening them. `quay label <session>
  "the electricity bill"` names one, no text reads it back, and `""` clears it. `L` in the console
  names the selected session, starting from the name it already has so changing one word does not mean
  retyping it. The listing shows the name where the session identifier was, falling back to the
  identifier when there is none, and the breadcrumb reads it too, so drilling in says
  `me > house-bills > the electricity bill`.

  Not `r`, which the `sessions` tool uses and which the issue asked for: `r` is refresh here by an
  earlier decision, and refreshing is pressed constantly while naming is rare, so the cheap key stays
  with the frequent action.

  A label is trimmed, flattened onto one line and capped at 60 characters rather than refused. It is a
  name somebody typed, so the only ways it can be wrong are invisible leading space, a newline that
  would draw a row two rows tall and break the cursor, and a length that pushes every other column off
  the screen.

  The migration adds the description column beside it, unused until the crew writes its own.
  ([#84](https://github.com/atlantic-blue/quay-crew/issues/84))

- **A run is not written while it is being read.** `quay flow start` failed about one run in six with
  `grpc: error while marshaling: size mismatch, calculated=110, measured=169`. `Engine.Begin` answered
  with the run and drove the same value in a goroutine behind that answer. A `Run` carries two maps,
  so the struct copy shared them: the goroutine wrote into a map while gRPC was marshalling a response
  that contained it, and the message grew between protobuf's sizing pass and its encoding pass. The
  goroutine drives its own copy now. `go test -race ./cmd/quay/` reported five races before and none
  after, and twelve runs of the package that was failing all pass.

- **The sessions listing is readable in colour.** Thirty rows in one colour meant reading every
  character of every row to find the conversation you wanted. Each cell is now coloured the way the
  `sessions` tool colours its own, because that is the listing already read all day: a workspace,
  project and session name each carry a colour of their own so the eye finds their rows without reading
  them, identifiers and ages and counts are dim so they stop competing, a token count in the millions
  is marked, and the mode is coloured by how much it allows, since that is the cell that costs most to
  misread. The selected row drops cell colour entirely, because the cursor is a bar across the row and
  coloured text on a coloured background is unreadable.

  The palette is twenty four colours rather than the `sessions` tool's twelve, and that is measured
  rather than preferred: across the fourteen workspace and project names on this crew, twelve put
  `atlantic-blue` and `itv` on the same colour, and both are workspaces. Ten names into twelve slots
  collide by arithmetic, so a different hash does not fix it, and FNV-1a over the same names collided
  three times where this one collided once. At twenty four every workspace is distinct and two pairs
  of projects in different workspaces share.

## 14 August 2026

- **A session's mode is picked in the console, not cycled.** `D` flipped between edits and dangerous,
  so planning was reachable from the command line and from the wizard and not from the surface an
  operator actually lives in. `m` now offers all three, narrowest first, and takes a pick. `D` opens
  the same picker, because it was the flip for as long as the console has had one and a key that
  silently stopped working is worse than one that changed shape.

  Widening what a session may do asks first, the way every key that gives a session more room does.
  Narrowing does not: it takes capability away, so there is nothing to be careful about and asking
  would only be in the way. Which way a pick goes is computed from the order the modes sit in rather
  than from a list of pairs, so a fourth mode cannot be added without it.

  ```
   mode d754610f   plan  edits  dangerous   j and k move, enter picks, esc cancels
  ```

  Rendered from a table test, not captured from a running console.
  ([#270](https://github.com/atlantic-blue/quay-crew/issues/270))

- **What a session may do when it is born comes from the crew's configuration.** It was a constant in
  the control plane and another in the store, so every session that did not come through the console's
  wizard arrived in `acceptEdits`, and the only way to change that was to edit the binary. A
  dispatched task has nobody to approve anything, so a crew whose work needs more than that had a
  session stopping to ask somebody who was not there, every time, from birth. `QC_PERMISSION_MODE` in
  `~/.quay/env` takes `plan`, `edits` or `dangerous`.

  A value that is not a mode stops the crew starting and names the three. Falling back would be
  silent, and a crew configured for `planning` would run every task in `acceptEdits` while looking
  exactly like a crew configured for `acceptEdits`. Unset is not a refusal: it keeps `edits`, which is
  what every session has had since the control plane was written, so an upgrade cannot quietly widen
  what a session may do.

  Configuration reaches a session when it is created and never after. A session that already exists
  keeps what it was born in, because changing a file must not widen a conversation already running.
  `quay mode <session>` still changes one session after the fact.

  The words for a mode were written out three times, in the command line tool, in the wizard, and
  nearly a fourth time here. They live with the model now, and all three read the same table.
  ([#270](https://github.com/atlantic-blue/quay-crew/issues/270))

- **A crew is one directory.** It was three. The data sat under `~/.quaycrew`, the tool's own files
  followed `XDG_CONFIG_HOME` into `~/.config/quay`, and configuration was in a checkout an installed
  crew does not have. Three places to find, three to back up, and no answer to "where is my crew".
  Everything now lives in `~/.quay`: `env`, `data/`, `context` and `panel-view`. Set `QUAY_HOME` to put
  it elsewhere.

  Nothing is moved for you. What is sitting there is a gigabyte of transcripts, two tokens and the key
  that unseals every secret, so both ways in refuse instead and print the exact commands: the tool
  before it reads a token, and `make up` before it mounts the data directory. Starting anyway was the
  real risk, because a crew that comes up on an empty directory mints a new token and reads as one
  that lost every conversation.

  ```sh
  mkdir -p ~/.quay
  mv ~/.quaycrew/data ~/.quay/data
  mv ~/.config/quay/context ~/.quay/context
  mv ~/.config/quay/panel-view ~/.quay/panel-view
  ```

  ([#248](https://github.com/atlantic-blue/quay-crew/issues/248))

- **Configuration lives outside the checkout.** A crew read `deploy/.env`, which compose loads on its
  own because it sits beside the compose file. That file cannot be given to anybody: a crew that was
  installed rather than cloned has no checkout to keep it in, and the quick start told the operator to
  create one with `cp`. Configuration now lives at `~/.quay/env`, which is where a crew keeps what
  belongs to it on this machine, and compose is told the path rather than left to find a file. `make
  config` writes it from `deploy/env.example` and leaves an existing one alone, and `make up` calls it,
  so a first run works and an edit survives. Set `QUAY_HOME` to put it elsewhere. A test reads what
  make computes and fails if the path is relative or inside the checkout, and a second test scans every
  tracked file for the old path, so the instruction cannot come back through a document nobody thought
  to change.
  ([#248](https://github.com/atlantic-blue/quay-crew/issues/248))

  Carry your settings over with one command, once:

  ```sh
  mkdir -p ~/.quay && mv deploy/.env ~/.quay/env
  ```

## 13 August 2026

- **A command takes the identifier the listing shows you.** A listing prints two identifiers for
  every session, the id and the handle, and dispatch takes an address on top of those. `attach`,
  `mode`, `panel` and `tasks` took only the id. So an operator read `300979a5` off the session column,
  typed it back, and got `no session with id or prefix "300979a5"` from all four. The mode of a new
  session was the case that hurt: you could see the session and you could not give it room to work.
  All four now take the id, the handle, a prefix of either, and an address, which is every form on
  the screen. A refusal names the sessions that exist and both of their identifiers, rather than only
  repeating what was typed.
  ([#265](https://github.com/atlantic-blue/quay-crew/issues/265))

- **The wizard asks what a session may do, and the session is born in it.** `n` then `session` started a
  session and never asked, so every one arrived in the crew's default. That is the one decision worth
  making at birth: a sandbox is born with its capabilities and never drifts, so changing the mode
  afterwards costs a restart, and a session born unable to act is a session that apologises. On this
  crew one was asked to clone a repository and answered that it needed approval from somebody who was
  not there. The wizard now offers plan, edits and dangerous, in the words `quay mode` already takes,
  and refuses anything else. `DispatchRequest` carries the mode so it applies before the sandbox is
  built rather than after. The guided first run does not ask: it is already six questions long, and a
  session it starts keeps the default exactly as it did.
  ([#270](https://github.com/atlantic-blue/quay-crew/issues/270))

- **A session can sign its commits.** A session commits as the operator and had no way to sign, so on
  any repository that requires verified signatures it produced a branch nobody could merge, which is
  most of the work it was asked to do. A session on this crew hit exactly that today and refused to
  commit rather than commit unsigned, which is what its rules ask for and is also a dead end. Now a
  workspace holding `GIT_SSH_SIGNING_KEY` gets sandboxes that sign: the key is written once when the
  sandbox is born, under `umask 077`, and git is pointed at it. An ssh key rather than a gpg one,
  because signing with ssh needs one private key file and no agent, no keyring and no pinentry prompt
  to hang a task nobody is watching. A workspace without the key is untouched, because a private key
  is the most sensitive thing this crew carries and silence is the right default. The key is read
  from the container's own environment rather than passed as an argument, where every process could
  read it and the task record would keep it.
  ([#279](https://github.com/atlantic-blue/quay-crew/issues/279))

- **`quay` opens the crew. It no longer refuses to.** `quay use atlantic-blue` printed "now in
  atlantic-blue", and `quay` then answered "say where to open: `quay use <workspace>/<project>`
  first". The workspace just named counted for nothing: the driver read where you were standing only
  when you stood in a project, then counted projects across the whole crew, and refused because there
  was more than one. Now it narrows to the workspace you are standing in, so a workspace holding one
  project opens it. Where it still cannot choose, the console opens on its own and you pick from it,
  which is what the same code already did for a crew with no projects at all. `quay` opening the crew
  is the whole command; telling you to type something first was never an answer. It had no test, in
  the way an untested branch usually does not; it has a table test now, mutation checked.

## 12 August 2026

- **Ctrl+c quits the console from wherever you are standing.** It took two presses: inside the command
  bar, the filter or the wizard, ctrl+c was a second escape, so the first press dropped you back to
  browsing and only the second one quit. The command bar is the one way in now, so that was most
  presses. Ctrl+c is handled once, before any mode sees it, and escape is the key that cancels a mode.

- **A fresh crew starts holding the skills this build ships with.** The image carries `skills/`, and a
  crew whose catalogue is empty imports all seven on startup and takes git and github at the crew
  level, so the first workspace has them without an import and without an attach. The other five sit
  in the catalogue waiting to be attached, because a cloud or a tracker skill in front of every
  session is a decision rather than a default. Only ever on an empty catalogue: an operator who takes
  a skill off the crew has said something, and restarting the control plane does not undo it. The
  behaviour scenarios seed from the same `skills/` directory the image carries, so a shipped manifest
  that stops loading fails in continuous integration rather than on somebody's first run.
  ([#273](https://github.com/atlantic-blue/quay-crew/issues/273))
- **A skill can be held by the whole crew, so a new workspace starts with something.** `quay skill
  attach crew github` and every workspace has it, including the ones made next month, which is the
  difference between setting a crew up once and setting each workspace up again. It takes the word
  crew where a workspace goes, exactly as `quay context set crew` does, so the two levels are said the
  same way. Until now the only way to reach every session was the crew's own skills directory on the
  machine, which a crew running on a pod has no way to fill, and the only way from the tool was
  importing a skill and then attaching it to each workspace by hand. The crew's holding and a
  workspace's are separate statements: the workspace's wins on a name, and detaching from the crew
  leaves a workspace that attached it for itself holding it. `quay skill list` says which ones came
  from the crew. Proved against real Postgres as well as the memory store, through the conformance
  suite both answer. ([#273](https://github.com/atlantic-blue/quay-crew/issues/273))
- **A skill whose secret is not set is left out of the session, rather than stopping the task.** One
  skill the workspace had not finished setting up refused every conversation in it: the github skill
  names `GH_TOKEN`, and without it a dispatch came back `FailedPrecondition` before a sandbox was
  built, whatever the task was actually about. Now that skill alone is left out. It is not mounted,
  the model is never told it exists, and `quay skill list` carries the line `left out: needs the
  secret GH_TOKEN ...` against it with the command that sets it. The reasoning that produced the
  refusal has not changed and is why the skill is withheld whole rather than half given: a
  capability that silently does not work is worse than one that is absent, because the model
  improvises around it and the operator reads the improvisation as the answer. What changed is the
  blast radius, and it changes now because a skill held by the whole crew cannot take every
  workspace down with it. A missing binary still refuses the task: the image is one thing for the
  whole crew, while a secret is one workspace's to set.
  ([#273](https://github.com/atlantic-blue/quay-crew/issues/273))
- **A skill that asks for nothing can be imported.** The Simplified Technical English skill declares
  no binaries and no secrets, and importing it into a real crew was refused: `null value in column
  "binaries" violates not-null constraint`. A nil list arrives at Postgres as an explicit NULL rather
  than as an absent value, so the column's `default '{}'` never applies. Every skill shipped before
  this one declared at least one binary, and the conformance suite only ever imported a skill
  declaring `git` and `gh`, so nothing had written an empty list. The memory store accepted it
  happily, which is how the two stores diverged without a test noticing. There is now a conformance
  case both stores answer, proven red then green against real Postgres.
- **The Simplified Technical English skill, and it is off unless you ask for it.** ASD-STE100 is the
  controlled English the aerospace industry writes maintenance documentation in, built so a sentence
  cannot be read two ways: one meaning per word, active voice, simple tenses, one instruction per
  sentence, twenty words. That is worth having for text something else has to parse, an error
  message, a tool description, a message to another session. It is the wrong thing entirely for
  writing a person is meant to enjoy, because the flatness that removes ambiguity also removes
  rhythm and voice. So this is the first shipped skill that is off by default: holding it is not a
  reason to use it, it applies only when the operator asks, and it never touches a blog post, a
  newsletter, marketing, or anything whose context describes how the writing should sound. Where the
  skill and a context disagree the context wins. The method and the worked examples live in
  `rewriting.md` beside the brief, read only when a rewrite is actually happening.

## 11 August 2026

- **One listing of sessions, in both surfaces.** The console showed ten columns and the command line
  four, from two separate pieces of code, so a session's mode, its tokens in and out, its cache spend
  and how long ago it was touched were visible in one place and invisible in the other. Neither
  printed a header, and reading `102` as a task count when it is input tokens is what happens without
  one. Both now render from `display.SessionCells`, so the two cannot drift again, and the command
  line prints a header with columns as wide as their widest value, since it has the whole terminal.
  A listing narrowed to where you are standing says so and how to widen it, because a narrowed
  listing and a smaller crew look identical. ([#244](https://github.com/atlantic-blue/quay-crew/issues/244))

- **A crew can be taken apart: `quay workspace delete` and `quay project delete`.** The command line
  had create and list and nothing else, so a crew only ever grew. A workspace made by a typo was
  there for good, and starting again meant going around the tool entirely, into Docker and the data
  directory, which is what wiping a live crew actually took today. Both calls already existed on the
  control plane and were reachable only by writing a throwaway program against the API. Deleting
  names what goes with it, a workspace's projects, sessions and secrets counted out, and asks for the
  name to be typed back before anything happens, which is the only guard a tool with no flags can
  offer. Piping the name in makes it scriptable without making it silent. Deleting where you are
  standing steps you back out, rather than leaving the tool pointing at something gone.
  ([#250](https://github.com/atlantic-blue/quay-crew/issues/250))

- **A secret is piped in rather than typed, so it never reaches your shell history.** The value was a
  positional argument and there was no other road, which put every credential the crew holds into the
  history file and into the process list, where any other process can read it with `ps`. It is the
  first thing anybody does with a new crew, so the model token was always the first casualty, and a
  real Jira token was pasted onto a command line while driving this and had to be rotated. Now
  `gh auth token | quay secret set GH_TOKEN` works, and so does `quay secret set GH_TOKEN < token.txt`.
  Whether something is being piped in decides how the arguments read, so the form scripts already use
  keeps working. The trailing newline every credential tool prints is trimmed, because a token
  carrying one authenticates nothing while looking exactly right in the listing, and an empty pipe is
  refused rather than stored. ([#253](https://github.com/atlantic-blue/quay-crew/issues/253))

- **The command that loads a file is in the usage, an empty file no longer erases a level, and a
  workspace's context is visible before it has a project.** Migrating an org into a crew is writing
  context, and `context set` was the only command that could do it from a file while being the one
  command the usage never mentioned: it offered the listing and `context edit`, which opens an editor
  and so cannot be scripted. It is listed now, beside a new `context clear`. That exists because
  `context set` with nothing on standard input silently wrote zero characters over whatever was
  there, with no undo, which is a forgotten redirection rather than a request to erase: it is refused
  now, naming how much it is protecting, and emptying a level on purpose says what it removed. And a
  workspace with no projects contributed no row to the listing at all, because the rows were built by
  walking projects, so an org's context could be written, stored and rendered while the crew appeared
  to hold nothing. ([#252](https://github.com/atlantic-blue/quay-crew/issues/252))

- **A refusal names the level that failed, and says what is actually there.** One message served all
  three levels of an address and it named the wrong one: a missing project came out as
  `workspace: no workspace with that id or name: project "nope"`, which blames the only part of the
  address that was correct and sends the operator to check their workspace. Now `quay use itv/nope`
  says `itv has no project "nope". it has: fe-player`, and the same shape covers a missing workspace
  and a missing session. A level with nothing in it says how to make one rather than listing nothing.
  And where you are standing lives on this machine while the crew's state does not, so a wiped crew,
  a fresh install or a different crew left every defaulting command refusing with a sentence about a
  missing workspace: it now says `you are standing in ghost/gone, which this crew does not have`, and
  how to move. An address you typed yourself still gets the plain refusal.
  ([#242](https://github.com/atlantic-blue/quay-crew/issues/242),
  [#251](https://github.com/atlantic-blue/quay-crew/issues/251))

- **Asking for help is answered, and `session` is a command.** The first thing anybody types was
  refused four ways: `help` and `-h` came back as unknown commands, and `--help` and `--version` were
  told the tool takes no flags and to say where with an address instead, which is not what either was
  asking. Now `help`, `-h`, `--help`, `-help` and `?` all print the usage and succeed, `--version`
  names `quay version`, and a flag that really is somebody trying to say where they are still gets
  the address advice. Separately the tool taught the word session three times on the same screen and
  then refused it as a command: `quay session list` and `quay sessions` now answer exactly as
  `quay sessions` does. ([#240](https://github.com/atlantic-blue/quay-crew/issues/240),
  [#241](https://github.com/atlantic-blue/quay-crew/issues/241))

- **`quay mode <session> [<mode>]`, so a task can be given room to work without a terminal.** A session
  is born in `edits`, and a dispatched task has nobody at a keyboard, so every approval the model
  asked for was denied by nobody and came back as a polite refusal. A session asked to clone a
  repository answered that it needed explicit approval for network access, with the token sitting
  right there in its environment. The mode could only be changed by pressing `D` in the full screen
  console, so a task from a script, a flow or the driver was stuck with what it was born in. Now it
  reads the mode with one argument and sets it with two, taking the words the listing prints
  (`plan`, `edits`, `dangerous`) as well as the model's own spellings, and refusing anything else by
  naming the three. It takes effect on the next task with nothing restarted, because the mode travels
  with the task rather than with the container.
  ([#254](https://github.com/atlantic-blue/quay-crew/issues/254))

- **A workspace's secrets reach that workspace's sandboxes.** Setting a secret is now the whole of
  the decision. There used to be a second list, `QC_SANDBOX_SECRETS` in `deploy/.env`, naming which
  secrets were allowed to leave the store at all, and a name missing from it meant the task was
  refused however carefully the operator had set the secret and attached the skill. Nothing in
  `quay secret list` said so, the refusal only arrived at dispatch, and the fix lived in a file
  inside a repository that somebody who installed the tool does not have. It is deleted rather than
  moved: the operator says yes by setting the secret on the workspace, and again by attaching the
  skill, and a third answer to the same question was worth less than the confusion it caused. One
  boundary is kept and one is new: another workspace's secrets are still never carried, and a
  workspace secret whose name starts `QC_` never travels, because those names are how the crew
  tells a sandbox where it is and what to dial with. A skill whose secret the workspace has not set
  is still refused before anything is built, naming the secret and the command that sets it. A crew
  started with the old list still in its configuration says out loud that it is no longer read.
  ([#247](https://github.com/atlantic-blue/quay-crew/issues/247))

## 9 August 2026

- **Two fixes to the command bar, from driving it.** Output lines are cut and padded to exactly the
  panel's width: the frame puts a border either side of whatever it is handed, so a short line left
  the right border sitting next to the text and a long one spilled past it, and the panel came out
  ragged either way. And typing the tool's own name at the front, which is what anybody does out of
  habit, ran it as an argument to itself and answered `unknown command "quay"`, which reads as the
  bar being broken rather than as a typo; the prefix is dropped now, and the name on its own says
  you are already looking at the crew.
  ([#236](https://github.com/atlantic-blue/quay-crew/pull/236))
- **The command bar is the one way in.** The commands that take over the screen used to be refused
  there, which left it a reading tool: `:attach <session>` now suspends the console, hands over the
  real terminal, and comes back when you leave the conversation, the same handover pressing enter on
  a row already made. Everything that only prints is still captured into the output panel. The one
  thing refused is opening a console inside the console, because that is recursion rather than a
  feature and the refusal says so. ([#234](https://github.com/atlantic-blue/quay-crew/issues/234))
- **The command bar runs quay commands, not just view names.** Typing `:` in the console has always
  opened a bar at the bottom, vim style, and it switched views. It now runs anything the tool can
  do: `:workspace list`, `:flow list`, `:skill list <workspace>`. Output opens in a panel over the
  rows that scrolls with `j` and `k` and closes on any other key, the way the help overlay already
  did, because a listing is taller than one row. A view name still switches views, so `:sessions`
  keeps working and the bar does the obvious thing with whatever was typed. A command that failed
  shows what it said rather than just that it failed, since "exit status 1" on its own tells nobody
  anything. Commands that take over the screen (`attach`, `panel`, `console`, `header`) are refused
  by name, because capturing one would leave the console waiting forever for output that is never
  coming. ([#232](https://github.com/atlantic-blue/quay-crew/issues/232))
- **A flow can start itself.** A graph declares `on: { every: 24h }`, and
  `quay flow schedule <workspace>/<project> <graph>` says where it runs. Until now an automation
  was a script somebody still had to remember to run. The interval lives in the graph, versioned
  and reviewable alongside what it does; the placement is the operator's, because a run needs a
  project to dispatch into. The schedule is a row with a next time, read by the same poller the
  waits use, so a restart loses none of them. Scheduling is deliberately not starting: the first
  run is one interval away, or an operator could not arrange something for tonight without also
  running it now. Nothing shorter than fifteen minutes is accepted, because a graph started faster
  than it finishes spends money as fast as the model can take it.
  ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **A flow can ask, and only a person answers.** The last of the five node types. An `ask` node
  puts its question to the operator, rendered from the run's state, and the run waits: no timer and
  no poller moves it, and the poller's own query passes over asking runs on their status, so an
  automation nobody answered can never take silence for a yes. The answer lands in state under one
  name, so an ordinary `choice` branches on it without the graph needing an expression language.
  Answered with `quay flow answer <run> <answer>`, which is what lets the whole shape ship with no
  chat channel and no bot token; a channel later is a second delivery of the same thing rather than
  the first. ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **A flow can wait, and a restart does not lose it.** A `wait` node says how long, as `for: 10m`,
  and reaching one puts the run down: recorded as waiting with a due time on its row, asking for
  nothing, costing nothing until its time comes. A poller reads the due rows every few seconds and
  once on the way up, so a crew restarted onto a pile of overdue waits resumes them immediately
  rather than losing them, which is the whole reason a wait is a column rather than a timer
  somebody is holding. A resumed run is carried on with the graph version it pinned, so editing a
  file while a run waits cannot change what that run does when it wakes.
  ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **A flow run can be stopped, and the reason is kept.** `quay flow stop <run> [<reason>]` halts a
  run in flight. Before this the only lever over a running automation was taking the crew down,
  which takes every other conversation with it. The stop is cooperative rather than a kill: a run
  waiting on a task finishes that task, because the model is already working and abandoning it mid
  sentence gains nothing, and what it cannot do is take another step. The database enforces that
  rather than the engine noticing, so a stop landing while the engine waits is never written back
  over. A run that already ended is not stopped again, because how it ended is the useful part.
  ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **A flow run cannot spend without bound.** An automation dispatches tasks with nobody watching, so
  a graph with a cycle was bounded only by its own shape. Every graph now has a transition cap,
  declared as `limits.transitions` or defaulted to 100, and may declare `limits.tokens` as a ceiling
  on what its own conversation costs. Both are checked before a movement rather than after it, so
  the dispatch that would cross a line is never made and never paid for. A run that hits either
  stops and carries the reason it stopped, which `quay flow show` prints on its own line: a run that
  was halted and a run that went quiet must never read the same. The token ceiling is opt in,
  because what is reasonable differs per automation; the transition cap is not.
  ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **`quay flow`: the operator can actually run one.** The engine shipped with nothing able to reach
  it, which delivers nothing. Four calls now sit in front of it, and `quay flow import <file>`,
  `start [<address>] <graph>`, `list` and `show <run>` in front of those. Importing parses the
  graph on both sides, so a graph a run could fall off is refused at the moment somebody writes it
  rather than hours later inside a run. Starting answers with the run and drives it behind that
  answer, because a task takes as long as the model takes; `show` says where it got to and what it
  knows. The driver may start a run, because a run is dispatch and it already has that, and may not
  import a graph, because writing an automation down is the operator deciding what the crew may do
  on its own. ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **The flow engine: a graph runs across sessions, every movement a row.** `internal/flow` is the
  automation substrate the review decided on: a graph of dispatches and choices authored as a file,
  imported at a version a run is pinned to, and a pure reducer a table test can hold. A run owns
  its own session, named after the graph and the run; a movement, its record and its dispatch claim
  land in one Postgres transaction (migration 0014), so a run is reconstructable by construction
  and the same task can never be sent, and paid for, twice: the claim is keyed by run, node and
  attempt. A finished run archives its session, because a finished run must not leave a container
  behind. Slice one ships dispatch, choice and done with the engine driven directly; wait, ask,
  ceilings, stop and the operator surface follow. Phase 3 of the architecture review, second
  slice. ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **History is written in the same breath as the task, and the broker became optional.** A task
  used to reach the `tasks` table only by going out to Redpanda and back through a projection, so
  every task that ran while the broker was down or `QC_KAFKA_SEEDS` was unset was silently and
  permanently absent from history. The dispatch path now writes the redacted task to the store
  synchronously, on a context detached from the request's, so a client hanging up after a long
  task cannot lose the record either. The projection is retired. Publishing to the log became an
  audit export for a second consumer that does not exist yet: Redpanda moved behind the compose
  `export` profile, and a plain `make up` needs Postgres and Docker and nothing else. Phase 3 of
  the architecture review, first slice.
  ([#182](https://github.com/atlantic-blue/quay-crew/issues/182))
- **The aws skill, reads but never mutates.** `skills/aws/` closes the set Julian named: describe,
  list, get and logs are always fine, starting with `aws sts get-caller-identity` so every answer
  says which account it read; anything that mutates infrastructure or data ships as Terraform
  through a pull request. The credential pair travels by name to sessions holding the skill, and
  which account and region a workspace points at is workspace context. The command line is pinned
  into the image; the stated cost is about a hundred and thirty megabytes, the heaviest thing a
  skill has asked of it. ([#218](https://github.com/atlantic-blue/quay-crew/issues/218))
- **The terraform skill, plans but never applies.** `skills/terraform/` carries the standing rule
  in its brief: validate, fmt and plan are always fine, and infrastructure mutates only through a
  pull request that continuous integration applies on merge, in every environment, however safe a
  change looks. The binary is pinned into the sandbox image the way gh is, proven by the class
  guard inside a real container. The stated cost: about ninety megabytes on the image, one image
  for now, revisited when sandbox tiers give skills their own.
  ([#217](https://github.com/atlantic-blue/quay-crew/issues/217))
- **The jira skill.** The linear skill's sibling in `skills/jira/`: a brief over Jira Cloud's REST
  API, authenticating as the JIRA_EMAIL and JIRA_API_TOKEN pair, both by name. Where the instance
  lives is workspace context rather than skill content, and the brief says to ask rather than
  guess when nothing names it. Reading is always fine; commenting and transitions happen when the
  conversation asks; creating, resolving and reassigning only on explicit instruction.
  ([#216](https://github.com/atlantic-blue/quay-crew/issues/216))
- **The linear skill.** A brief over Linear's GraphQL API in `skills/linear/`: reading is always
  fine, writing happens when the conversation asks for it, and the key travels as
  LINEAR_API_KEY to sessions holding the skill and nowhere else. No binary beyond curl. The image
  guard also grew from one binary to the whole class: every binary any shipped skill declares is
  proven present inside a real container, so the next skill cannot promise a tool the sandbox does
  not have. ([#215](https://github.com/atlantic-blue/quay-crew/issues/215))
- **The github skill, and gh in the image.** The second shipped skill, in `skills/github/`, and
  separate from git on purpose: git needs a repository and nothing else, github needs a credential,
  the network, and it does things that cannot be undone. The brief says how pull requests are
  opened here (branch first, a short What and Why, watch the checks land) and what a session never
  does: merge, close somebody else's work, or delete a branch it did not make, unless asked in the
  conversation. The image gains `gh` at a pinned release and curl beside it, proven usable inside a
  real container. The stated cost: about forty megabytes on the sandbox image.
  ([#214](https://github.com/atlantic-blue/quay-crew/issues/214))
- **A crew that opens empty walks you through its own setup.** The console opening with no
  workspaces used to show an empty listing and nothing suggesting what to do next; getting to a
  working session took four passes of the wizard and prior knowledge of the order. It now offers a
  guided setup chaining the wizard's own stages in the order the crew needs them: a workspace, a
  project, the model token, context for the project (pasted, or a file path the crew reads), a
  skill from what the store holds, and a first message that becomes the first session. An empty
  answer skips a stage, a skipped project takes the stages that need one with it, a crew holding
  no skills is never asked about them, and escape leaves the setup keeping whatever was already
  made. Once a workspace exists it is never offered. The wizard also learned `skill` as a kind of
  its own, so attaching a skill no longer needs the command line.
  ([#211](https://github.com/atlantic-blue/quay-crew/issues/211))
- **A repository is cloned in conversation, following the git skill, and the machinery that did it
  for you is gone.** A skill is a text file the session follows, and the git capability had grown an
  API instead: repository records on the workspace, a clone at sandbox birth into the workspace's
  volume, a git worktree per session. All of it is removed: the RPCs, the `quay repository`
  commands, the table (dropped forward, migration 0013), the clone and the worktrees. `quay
  repository <anything>` now refuses loudly and names the new way. What stays is the invisible
  plumbing a brief can rely on: the git identity environment, and a credential helper baked into the
  sandbox image that answers git from GH_TOKEN at the moment it asks, proven against a real
  container. The git skill itself ships in `skills/git/`, the first real one: clone it yourself,
  branch first, stage named files, commit as the operator. The stated cost: each session clones its
  own copy, so first tasks on big repositories are slower and disk is spent per session.
  ([#210](https://github.com/atlantic-blue/quay-crew/issues/210))
- **A session whose sandbox predates the workspace's skills says so, instead of being lied to.** A
  sandbox is born with its capabilities and never drifts: the mount, the secrets and the setup only
  happen at container creation. What each live sandbox was born holding is now recorded, and a
  session whose birth set differs from the workspace's current skills is marked stale, over the API
  and beside its status in the console. Stopping and restarting it builds a sandbox born current,
  and the conversation is bind mounted so nothing is lost. Attach and detach say this out loud. A
  stopped session is never stale, because its next sandbox is born with the current set.
  ([#208](https://github.com/atlantic-blue/quay-crew/issues/208))
- **One resolver answers what a session holds, and the listing asks it.** What a session holds used
  to be answered four separate times per sandbox creation, four store round trips that could
  disagree, and the only honest answer was exposed to nobody. One resolver now answers holdings,
  mounts and which skills need their files written out, in one round trip, and
  `quay skill list <workspace>/<project>/<session>` reports what that session actually holds: the
  crew's own skills and the workspace's, the workspace winning a name collision, exactly what its
  sandbox is built from. ([#206](https://github.com/atlantic-blue/quay-crew/issues/206))
- **A skill can no longer select secrets the operator did not hand out.** A held skill's declared
  secret travels only when the operator also names it in QC_SANDBOX_SECRETS; a task whose skill
  secret is set but not handed out is refused naming exactly what to add and where. A manifest
  naming a secret starting QC_ or CLAUDE_ is refused at validation on both roads in, because those
  names are the crew's own configuration and the model's token, and one stored by an earlier build
  is filtered at the sandbox boundary. Before this a manifest was an arbitrary secret selector over
  whatever the workspace held, bypassing the allowlist entirely.
  ([#204](https://github.com/atlantic-blue/quay-crew/issues/204))
- **The protocol says session, the way every surface already does.** `message Session` is now
  `Session`, the session RPCs are session RPCs, and a dispatch returns the session's `id` beside its
  `handle` instead of a `session_id` beside a `handle`. The three identifiers a session carries
  each have one job: `id` is the crew's row and names the sandbox container, `handle` is what a
  channel dispatches to, and `model_session_id` stays in the model's own word because it is the
  model's conversation. The store keeps the word session internally. Done now because the
  repository is going public with `v1` in the package name, where the same rename becomes a
  breaking change. ([#202](https://github.com/atlantic-blue/quay-crew/issues/202))
- **A secret pasted into a conversation is not persisted in the clear.** Every published task
  payload, the prompt, the reply and the failure, goes through the crew's redactor before it
  reaches the log, and therefore before the projection writes it to the `tasks` table: every value
  the workspace keeps sealed is replaced with the secret's name, the driver's token is caught for a
  driver session, and anything shaped like a subscription token is caught even when the crew never
  held the value. What the crew cannot recognise is stored as typed, and `docs/EVENTS.md` says so.
  ([#200](https://github.com/atlantic-blue/quay-crew/issues/200))
- **The driver cannot grant itself anything.** The driver is handed its own token at sandbox birth,
  minted into `driver.token` beside the crew's, so the control plane can tell its calls apart, and
  the calls that grant capability are refused to it: setting or listing secrets, importing,
  attaching or detaching skills, a session's permission mode, and context at the crew scope. The
  refusal says the call is the operator's to make. Everything the driver exists to do stays open:
  workspaces, projects, sessions, dispatch, and context at the workspace and project scopes. Before
  this the driver held the operator's own token, so a session that could drive the crew could also
  widen itself. ([#198](https://github.com/atlantic-blue/quay-crew/issues/198))
- **The crew refuses a caller it cannot recognise.** The control plane mints a token the first time
  it starts, kept at `crew.token` beside the key that seals secrets, and every call has to carry it
  or is refused with a message naming what to present. The listener binds to loopback by default and
  the compose file publishes the port on the host's loopback only. `quay` presents the token from
  QC_TOKEN or from the crew's data directory, so the operator's own machine works with nothing to
  configure, and the driver session is handed it at sandbox birth beside the crew's address. Before
  this, anyone who could dial the port held the whole crew: every secret name, every session, the
  context injected into every sandbox.
  ([#196](https://github.com/atlantic-blue/quay-crew/issues/196))
- **The documents say what the code does, and carry the day's decisions.** The architecture document
  stopped calling the log the source of truth, which the code decided against long ago; the store is
  the truth and the log is the export. It also now records four decisions from the 9 August
  architectural review: automation runs live in Postgres with the log as export, what a session
  holds is fixed when its sandbox is born, the operator facing word is session and the protocol
  aligns before going public, and authentication is a bearer token per crew with the listener on
  loopback by default. The skills document stopped reading as though its whole delivery had shipped:
  slices one to five are done, the git and github skills and everything after them are open.
  ([#192](https://github.com/atlantic-blue/quay-crew/issues/192))
- **Sandboxes stop leaking.** Four ways a container outlived what it belonged to, all closed.
  Deleting a workspace or a project now stops the sessions it hides and closes their sandboxes,
  where before every container kept running with the workspace's secrets in its environment.
  Stopping or archiving a session now removes its container even after a control plane restart,
  because the close asks the daemon by name rather than a process map that a restart empties. A
  sandbox whose clone or skill setup fails is closed rather than left running and untracked, one
  per attempt. And starting up reaps what earlier builds left behind: a container whose session is
  stopped, archived or gone belongs to nobody and is removed on the way up.
  ([#191](https://github.com/atlantic-blue/quay-crew/issues/191))
- **A skills index left behind by an earlier build stops becoming session context.** A build before
  the index moved wrote it into the session's own memory file. Read back by a later build that only
  knew the mark in the outer file, the whole index was swept into session context, stored as though
  the operator had typed it, and rendered again on every task from then on. The mark is recognised
  in every file now, what sits under it is dropped rather than swept, and a level whose stored
  context already carries a swept index is cleaned the next time it renders. The read back also
  stops filing a note appended to the workspace memory file under the index's own mark, which was
  quietly dropping it; a note an agent appends is kept as workspace context.
  ([#190](https://github.com/atlantic-blue/quay-crew/issues/190))

## 8 August 2026

- **A workspace has a volume, and its repositories are cloned into it once.** The volume is a directory
  of the workspace's own, mounted read write into every session in it, and it is general: repositories are
  the first thing to live there, and anything else a workspace accumulates and wants its sessions to share
  can follow without a feature each.
  The clone happens inside the container, so a repository the operator has never had on their machine
  works exactly like one they have, and it happens once for the workspace rather than once per session.
  The difference is one copy of a large checkout against one per conversation.
  Each session then gets its own git working tree of it, on a branch named after the session, linked into
  its working directory where the model starts. A working tree rather than the shared checkout because git
  allows one per branch: two conversations in one directory share an index, and the first checkout moves
  the ground under the other.
  The working trees live in the volume rather than under a session's own directory, which looks like a
  detail and is not. A clone records where every working tree cut from it lives, and that record is shared;
  a session's directory is at the same path inside every container, so two sessions would register the
  same path and the second would prune the first, leaving it holding a tree its clone had forgotten. Two
  sessions in one workspace is what found that, and nothing with one session ever would.
  Proved in a real container: one clone, two working trees, two branches, one session's commit invisible
  to the other, and asking again leaving a session's own work alone.

- **A repository belongs to the workspace, and a workspace can work in several.** It sat on the project
  for one commit and that was the wrong level: the workspace is already where a credential lives and where
  a skill attaches, which are the two things a repository needs, and every project in a workspace almost
  always works in the same code. Several rather than one, because a workspace routinely spans more than
  one: a service and its infrastructure, or a frontend and the api behind it.
  `quay repository add <url>`, `quay repository list`, `quay repository remove <name>`. Each lands in a
  directory of its own under every session's working directory, named after the repository, and two
  remotes that would want the same directory are refused when the second is added rather than the second
  one quietly never being cloned.
  `quay project remote` and `--remote` are gone and say so, naming the command to use instead. A flag that
  is ignored is worse than one that never existed: `--remote` absorbed silently would have become the
  project's name.
  The migration carries over anything already set on a project, folding two projects that named the same
  repository into the one row, and that carry over is tested against a real database rather than assumed.
  For [#179](https://github.com/atlantic-blue/quay-crew/issues/179).

- **The rest of the commentary, trimmed to the same rule.** The control plane, the console's model,
  its resource registry and the command line tool, on top of the files in the pass before. A comment
  earns its place by saying something the code cannot: a constraint from outside, a trap, or why the
  obvious way fails. Everything else is gone rather than reworded.

## 8 August 2026
- **A project names the repository its sessions work in, and the first task clones it.** A session's
  working directory started empty, so the skills work had built the way to describe git and there was
  nowhere to run it: `quay project create <name> --remote <url>`, or `quay project remote set <url>` on a
  project that already exists. The remote sits on the project rather than the workspace, because a body of
  work is one repository.
  It clones into a directory under the working directory, not into it, because the memory file the model
  reads is written there before the container exists and git refuses to clone into somewhere that is not
  empty.
  Three things about the command, and each is the reason it is built rather than formatted. It clones only
  when there is no checkout yet, because a sandbox is adopted across tasks and a second clone either fails
  or throws away what the first task did. The remote is a positional argument and never part of the
  script, because it comes from a person and a remote inside a command can end that command and start
  another. The credential is read from `GH_TOKEN` in the environment by a helper at the moment git asks, so
  no token is ever in an argument list, which anything that can inspect the container could read.
  A remote is refused when it is set rather than when a clone runs, because the person who typed it is
  there to read the refusal, and one carrying a credential is refused outright: it would otherwise be a
  password in the database and in every listing that prints a project.
  Proved by cloning for real inside a container, twice, from a bare repository made in the container so
  it needs no network: the checkout arrives, the memory file beside it survives, and the second run leaves
  the session's own work alone. That real run is also what caught the helper being handed to git with no
  configuration key, which every unit test had passed straight over.
  The continuous integration run now builds the sandbox image, so this test and the commit test from
  earlier today actually execute there. Both had been skipping: they need a container with git in it, and
  nothing in the pipeline was setting one, so the two things only a real container can prove were running
  on one laptop and nowhere else, while the check reported a pass.
  Not covered by a test: a private clone over https, which needs a real token against a real host.
  Fifth slice of [`docs/SKILLS.md`](docs/SKILLS.md), for
  [#179](https://github.com/atlantic-blue/quay-crew/issues/179).

- **A workspace can be given a skill of its own, imported and pinned.** The crew's skills directory
  reaches every session, which is the crew level. This is the other one: `quay skill import` reads a
  skill's directory and sends its files, and `quay skill attach` gives it to a workspace, so a token for
  one capability is not handed to every session the crew has. The files travel rather than the path,
  because the control plane runs in a container where a directory on your machine means nothing, and a
  crew on a pod has no host directory to go back to for whatever it did not copy.
  A workspace pins the version it attached. Importing a newer revision does not move it, and importing a
  different skill under a version already held is refused rather than overwriting what a running session
  is using.
  What reaches a session is now one line per skill rather than each brief. The line says the skill exists
  and where to read it; the brief is a file beside it, opened when that kind of work comes up. This is a
  change to what shipped earlier today, and the reason is measured rather than guessed: this crew's four
  levels of context reached 51,727 bytes at the workspace, paid by every session before a word was typed.
  Both sources are mounted read only at the same path, so nothing in a sandbox can rewrite what it was
  granted, and where a name is held by both the workspace's own wins.
  Proved by importing a real directory through the real command line tool into a real Postgres, then
  reading the brief and the environment from inside the container the session runs in. Six mutations
  checked red, two of which found assertions that were passing for the wrong reason.
  Third slice of [`docs/SKILLS.md`](docs/SKILLS.md), for
  [#179](https://github.com/atlantic-blue/quay-crew/issues/179).

- **Less commentary, and what is left says the constraint.** The repository was 3,429 comment lines
  against 24,132, and some files were over half comment. Five entries in this file quoted the operator
  by name, typos and all, and comments narrated their own redesigns: it was called that for a day, it
  used to be six lines, it was the row under the cursor for a while. None of that is for the next
  reader, and most of the rest was the code said twice in prose.
  A comment earns its place by saying something the code cannot: a constraint from outside, a trap, or
  why the obvious way fails. `Argv is the command and its arguments` is not one of those. What is kept
  says the rule rather than the route to it: the wordmark is one line because six lines cost six rows,
  a key while the wizard is working is not an answer because nothing is being asked.
  A first pass over the densest files. The largest three are still to do.

## 8 August 2026

- **A skill reaches a session.** A skill is a directory with `skill.yaml`, a `SKILL.md` the model
  reads, and whatever else it needs. Put one in the crew's skills directory and every session gets it:
  the brief lands in the memory file it already reads, the directory is mounted read only beside the
  working directory, and the secrets the skill names are carried in.
  It refuses rather than half working. A skill naming a secret the workspace has not set is refused
  before a container is made, saying which secret and how to set it. A skill naming a binary the image
  does not carry is refused once the container can be asked, naming the binary and the image to add it
  to. A capability that silently does nothing is worse than one that is absent, because the model
  improvises around it and the improvisation reads as the answer.
  Read only, because a session that can rewrite its own instructions can give itself a capability
  nobody approved. `bin/setup` runs once per container rather than once per task. A brief is marked in
  the memory file like every other section, so it is never read back into the crew's context and
  rendered beside itself from then on.
  First of the pull requests in issue #143's skills plan. Attaching a skill at a level rather than to
  the whole crew, and pinning a session to the version it started with, are the next one.

## 8 August 2026

- **A session can commit as you.** `git` has been in the sandbox image the whole time and unusable,
  because a container has no identity and the tool refuses to commit rather than guessing. That
  refusal is correct and it is a wall to walk into halfway through a piece of work.
  `QC_GIT_AUTHOR_NAME` and `QC_GIT_AUTHOR_EMAIL` are carried into every sandbox, as the author and the
  committer both, because git names them separately and refuses on either missing. Half of one is
  carried as none of one: it is refused the same way and it looks configured, which sends the operator
  looking for the problem somewhere else.
  Proved by committing in a real container and reading the author back, rather than by asserting four
  variables were set. That assertion would have passed just as happily with the wrong names in them.
  Second slice of [`docs/SKILLS.md`](docs/SKILLS.md). Per workspace identity arrives with skills; this
  is the crew's.

## 8 August 2026

- **A workspace's secrets reach a sandbox by name.** One line decided what a session could ever be
  given: the model's own token, hardcoded, and nothing else. A workspace could hold a credential for
  anything at all and no session could use it, which is why `git` sits in the image with no way to
  push and why there is no `gh` worth adding yet.
  Named rather than all of them. A sandbox holds a value for the life of its container and the model
  can read it, which is the point of giving it one, so the crew hands over what a session needs and
  not everything the workspace happens to hold. `QC_SANDBOX_SECRETS` is the list; the model's token is
  always carried and needs no naming; a name with nothing set against it is skipped rather than
  refused, because a crew configured for a skill nobody has set up yet should still run its tasks.
  First slice of [`docs/SKILLS.md`](docs/SKILLS.md). When skills exist they contribute the names, and
  this is the path they will use.

## 8 August 2026

- **What the crew has cost is in the header.** Beside the build, so it is in front of you while you
  work rather than only when you go and look at a listing: what came back, what was sent, what was
  read from the cache. Its own call rather than part of `GetInfo`, because that one answers what a
  task dispatched here would do and is fetched once, and this changes with every task. The console
  refreshes it with the rows now instead of at startup, since a total from when the console opened
  looks live and is not.
  It gives way before the wordmark, deliberately: the number is also in the listing, and the wordmark
  is what makes the panel look like something. Archived sessions are counted, because what a piece of
  work came to does not stop being true when the session is put away, and a total that shrinks when
  somebody tidies up is worse than no total.

## 8 August 2026

- **What a session has cost is in the listing.** Three columns, `in`, `out` and `cache`, read from the
  transcript the model keeps, in numbers a person can compare at a glance: 52, 6.9k, 1.7M. A session
  that has spent nothing shows nothing, because a conversation nobody has had has not cost zero.
- **A column can give way when the window is too narrow to hold them all.** A line too long was cut at
  whatever happened to be at the end rather than at whatever mattered least, which in a panel is most
  of the time, since the console has half the window. A resource now says which columns may go and in
  what order: the cache first, then what came back, then what was sent, and never a session's
  identifier, status or age.

## 8 August 2026

- **A session reports what its conversation has cost.** Four numbers, read from the transcript the
  model keeps: what was sent, what came back, what was read from the cache and what was written to it.
  It has to come from there, because the conversations worth counting are the ones held in the panel
  and those never pass through the control plane at all.
  Four rather than two, because two would be a lie by omission. On a real conversation the input was
  52 tokens and the cache read was 1,723,404: almost everything sent is the context being read again
  every task, so inbound and outbound alone would show the 52 and hide the rest.
  A session nobody has spoken in reports nothing rather than a cost of nothing, a torn last line is
  skipped rather than failing the whole file, since the tool appends as it goes, and each transcript
  is counted once until it changes, so a console refreshing every few seconds does not reparse every
  conversation in the crew.

## 8 August 2026

- **The crew names a conversation instead of learning what it was called.** A conversation started
  inside a sandbox picked its own identifier and told nobody, so every conversation opened from the
  panel was one the crew could not name: no history to read back, nothing to attribute a cost to, and
  no way to tell one transcript in a workspace from another. One machine held eleven transcripts for
  two sessions it knew about. The control plane now chooses the identifier when a conversation is
  opened, records it on the session, and hands it down, so what the crew holds and what the model
  writes are the same name.
  The sandbox decides how to open it, because it is the only place that can see whether the transcript
  is there: it resumes one that exists and starts one under the given name when it does not. That
  replaces a refusal. A session whose conversation had been lost with its container used to be turned
  away, and could not be told apart from a conversation the crew had just named and nobody had spoken
  in yet. Both now open, which is what the operator wanted in either case.

## 8 August 2026

- **A shell says which sandbox it is in.** Every session has a container of its own with its own empty
  working directory over the same image, so shelling into two of them gave two screens that were
  identical in every visible respect: same prompt, same empty listing. It read as `s` opening the same
  shell whichever session you chose. It was always the right container, and nothing on the screen said
  so. The prompt now carries the session and its project, on every line.

## 8 August 2026

- **Cycling panes skips the header, and the pane with the keyboard is lit.** The header is one row of
  text with nothing to type into, so the pane keys landed on it and cost a press to arrive and another
  to leave, which put the two halves you actually use three presses apart. A hook scoped to the
  panel's window bounces the selection on to the console, so the keys are a toggle between the console
  and the conversation. The active pane's border is drawn in the same colour the console uses for the
  row the cursor is on, and the inactive ones are dimmed, because three panes and an unlit border meant
  typing something to find out where you were. Both are window scoped: your own tmux sessions are
  unchanged. An open panel picks this up the next time it is rebuilt, which this build triggers.

## 8 August 2026

- **What a skill costs, in [`docs/SKILLS.md`](docs/SKILLS.md).** The design said nothing about how
  big a brief may be, and a skill whose brief is a manual is paid for on every session that holds it.
  A brief is now short by construction: it says when to use the skill and what it can do, and the
  detail lives in files in the skill's own mounted directory that the model opens only when it needs
  them. Measured rather than asserted: one real crew rendered 51,727 bytes of context into every
  session in every workspace, about thirteen thousand tokens before a word was typed, none of it a
  skill. A level that reaches everything gets filled until it hurts, and skills would be next.

- **A design for skills, in [`docs/SKILLS.md`](docs/SKILLS.md).** A session opens knowing nothing
  about how you work, and `git` in the image with no identity and no credential is the shape of that
  gap. A skill is a capability written down as code: a brief, the binaries it needs, the secrets it
  names, and its own setup. Authored as files so it is reviewable and shareable, imported and pinned
  so a crew on a pod still has it and it cannot change under a running session, rendered back into the
  sandbox because the model reads files. Skills and workflows stay separate entities: a skill is what
  a session can do, a workflow is what should happen, and the second one composes the first.
  Nothing is built yet. The document ends with the slices, and the first of them is that a workspace's
  secrets should reach a sandbox by name rather than through one hardcoded key.

## 8 August 2026

- **`make upgrade` names the configuration your `deploy/.env` does not have.** An upgrade adds
  configuration and nobody's copy grows with it. Compose fills a key that is not there with an empty
  string, so whatever it turns on is off and nothing says why, which is exactly how a driver came to
  report the control plane refusing connections for an evening. `make env-check` compares the two and
  names the difference, and an upgrade runs it. It says nothing when there is nothing to say.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **A session says when it was not told where the crew is.** `quay` inside a sandbox that was never
  given an address falls back to localhost, and localhost inside a container is the container, so it
  printed `dial tcp [::1]:50051: connect: connection refused` while the control plane was up the whole
  time. That reads as the crew being down and it is not: this session was not given the two pieces of
  configuration that let it reach the crew, and neither can be set from in there. It now says which
  ones, and that a sandbox keeps the configuration it was made with, so the session has to be started
  again. Only inside a container, only when nothing was given, only on a refusal to connect: on the
  operator's own machine localhost is where their stack runs and the dial error is the right answer.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

## 7 August 2026

- **`N` says when it could not end the conversation.** Ending the conversation beside the console
  discarded whatever the attempt had to say, and the pane is reopened straight after. A conversation
  that is still running is attached to rather than started, so it came back with its history and the
  key read as doing nothing at all. A container that is not running and an image too old to have tmux
  in it both ended nothing, silently. Whether it worked is answered by asking afterwards rather than
  by the exit status, because ending a conversation that was never there fails too and that is the
  state the next open wants. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **When the crew cannot open, it says why.** Opening the panel and failing fell back to a single
  console pane, whatever the reason: tmux missing, a crew with two projects and nowhere named to open
  in, a header with no room to draw. All of them looked identical from the outside, so the panel read
  as a thing that sometimes does not appear, and the reason was printed nowhere. There is one reason
  left to fall back on, a crew with nothing to put beside the console yet, which is what a first run
  looks like. Every other failure is reported, and each of those refusals already names what to do
  about it. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The driver is made able to act rather than to ask.** It was created in the same mode as any other
  session, so the one session whose whole job is to drive the crew stopped and asked before every step:
  asked to make a project, it described how you would go about making one. It is created able to act
  now. What bounds it is the sandbox, which is the same boundary it had in either mode, and `D` in the
  console still sets it back to asking like any other session. A driver made before this keeps the mode
  it has; `D` moves it.
  The rule went into the store conformance suite rather than into one of the two stores, because the
  driver is created separately in each and only one of them said which mode it starts in.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`make upgrade` rebuilds the sandbox image, and a stale one says so.** Upgrading fast forwarded the
  checkout, reinstalled the tool and rebuilt the stack, and never touched the sandbox image. Sessions
  run whatever that image holds, so the tool and the control plane moved forward while every
  conversation carried on in a container from the build before, with a `quay` inside it older than the
  crew or not in the image at all. That is why a driver had no `quay` to drive anything with.
  The image now carries the build it was made from as a label, the tool inside it reports the same
  build, and the crew reads the label back: the console's header says in red when the sandboxes are
  running an image older than the build, and the help panel names the build they are on. An image
  from before this was stamped says nothing, and the crew then claims nothing about it rather than
  calling a good image old. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **A context change reaches a session that is already running.** Context only travelled to a sandbox
  when that sandbox was made, so telling a running session something did nothing you could see until it
  was replaced, and nobody replaces a container to deliver a note. `quay context set` writes out to
  every live session that reads the level it changed. The level just set wins over the file for that
  one write, because a set that hands you back the body you were replacing is not a set; every other
  level is still read back and kept.
  A model already running does not see it mid conversation. The command line tool reads its memory
  when a conversation starts, so it lands on the next task or the next time the conversation is
  opened. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **A memory file the crew never wrote no longer replaces what the store holds.** A `CLAUDE.md` with
  none of the crew's marks in it was read back as an edit of the context the store holds, which is
  impossible: whoever wrote that file had never seen the store's body. It replaced it outright. That
  is why a driver taught what quay is lost the manual moments later, the file already in its working
  directory was claimed as its context and the manual was gone before anybody opened the
  conversation. An unmarked file is added to what the store holds now rather than replacing it, so
  both survive, and the manual is written out to the file the driver reads when it is taught rather
  than waiting for a sandbox to be made.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The header stops repeating down the screen.** It redrew by printing after what was already there,
  so every second added another header to the pane's history and the pane scrolled: seventeen of them
  stacked up. It draws on the alternate screen now, where nothing it writes becomes scrollback, homes
  the cursor and clears rather than printing on the end, and cuts each line a column short of the pane
  so a line reaching the last column cannot wrap and push the one below it out.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`N` starts a fresh conversation beside the console.** Opening the crew comes back to the one you
  were in, because it runs in a tmux session inside the sandbox that is attached to rather than started
  when it is already there. That is what `ctrl-q` is for, and it meant the driver could never give you
  a clean start. `N` ends the one that is there and opens a new one; `p` is unchanged and only shows
  and hides.
  The conversation beside the console is always the driver now, whatever the cursor is on. It was the
  row under the cursor for a while, which reads well until you press the key for a fresh conversation
  and it ends whichever session you happened to be scrolled to.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The driver opens knowing what quay is.** Opening the crew writes the manual into the driver's own
  context, so an agent in it starts with the model, the commands and what the crew can actually do,
  rather than having to be told every time. Its own level, not the project's, because the project's
  context belongs to the work being done there. Written once: an operator who edits it has a reason
  to, and overwriting on every open would make it the one context nobody can change.
  The command list moved into `internal/manual`, so what `quay` prints and what a session is told are
  one string and cannot describe two different tools.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The driver is its own session, and the only one that can drive the crew.** `quay` opens the
  project's driver rather than whichever conversation happened to be newest, creating it the first
  time. One per project, held by a unique index rather than by reading first and writing after.
  A session is marked as the driver, and everything that widens is gated on that mark: only the driver
  joins the control plane's network, only the driver is told where the crew is, and only the driver
  gets the host paths you hand it with `QC_DRIVER_MOUNTS`. An ordinary session can reach nothing of
  ours and sees nothing of the machine.
  That last part is what makes it the glue: without host paths it can reach the crew and has nothing
  to bring to it. Hand it your hub read only and it can load a ticket folder as a project's context.
  Opening a driver that has never spoken starts a conversation rather than refusing, because it is
  made the moment you open the crew and telling you to dispatch a task to the thing you just opened is
  a loop. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The logo is the logo again.** It had been replaced with the name written out in text, which is not
  what was asked for: "you have replaced it with some text". The block letters are back, at half the
  height, each row carrying two rows of the original through the half block characters. Three rows
  rather than six, so the header still costs little.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay` rebuilds a panel left over from an older build.** Opening it reattached to the tmux session
  already there, whose panes were still running the binary from before the upgrade, so a fix that had
  shipped was not in what you were looking at however many times you ran `make upgrade`. The panel
  records the build that made it and is made again when that differs. A panel from this build is still
  just reattached to, or every open would restart the conversation you were reading.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **A session can drive the crew from inside its sandbox.** The sandbox image carries `quay`, the
  sandbox joins the control plane's network, and the control plane puts its own address into every
  sandbox, so an agent in a session runs `quay workspace create` and it works with nothing to
  configure. That is what makes the conversation beside the console able to do anything rather than
  just talk about it.
  It is off unless it is turned on: a session that can drive the crew can also stop other sessions.
  `deploy/env.example` turns it on for a local stack, and without both the network and the address a
  sandbox reaches nothing of ours. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The wordmark is drawn in the panel's header again.** A height check left over from when the
  wordmark was six lines of block letters dropped it from any pane shorter than seven rows, and the
  header pane is one row by design. One line costs no rows, so only width can stop it now.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The header is the wordmark, which build this is, and how to reach everything else.** It carried the
  crew's description and this view's keys, which at half the window left no room for the wordmark and
  pushed it off the screen. One row now: the
  wordmark is one line rather than six of block letters, and it survives a conversation beside the
  console at every width worth drawing a console in.
- **The help panel carries everything the header dropped**, on top of what it already had: where the
  crew is, where you are standing in it, and what it is running underneath. It scrolls with the arrow
  keys when a short window cannot show all of it, rather than cutting the end off silently, which is
  how a panel missing half its keys looks exactly like a complete one.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay` is the panel. There is no `quay panel`.** Running `quay` opens the crew: the header across
  the whole width, the console under it on the left, and a conversation on the right. `p` shows or
  hides the conversation. One command opens everything, rather than a second command for the thing the
  first command is for, and the header reaches the whole width.
  The header is the console's own, drawn in a pane of its own so it can reach across both halves, and
  held at exactly its own rows when the terminal is resized. With no conversation to open yet, `quay`
  opens the console on its own rather than refusing.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`p` shows or hides a conversation beside the console.** It opens the one under the cursor, or the
  one last spoken to when nothing is selected, and pressing it again closes the one it opened. It works
  in `quay`, not only in `quay panel`, so you do not have to decide before opening the console whether
  you wanted a conversation next to it.
- **The console keeps its own header, always.** It was replaced in the panel by a status line tmux
  drew, and what came back had lost the wordmark and the engines and sat on top of the console's own
  rows. The status line is gone: the header is the console's, full width on its own and squeezed to half when a
  conversation is beside it. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The panel's header is tmux's own status line, and the panel is two panes again.** It was a third
  pane, which meant a process to draw it, and that process could not see which view the console was on,
  so the console had to publish it. tmux draws a status line itself, across the full width, at a height
  it owns and with no scrollback to scroll into, so the header needs no process of its own.
  Gone with it: the `quay header` command, the alternate screen handling, the view publishing and the
  resize hook that held the pane at a height.
- **The header is the bare essentials and the wordmark.** Which build, which crew, and where you are
  standing, asked again while you work so `quay use` in the other pane moves it.
- **`:stats` is what the crew is running underneath**: the model backend, the sandbox and store
  engines, where secrets and state are kept, whether anything reads the event log. It was six lines of
  the header, which is what made the header too tall to be fixed.
- **`:keys` is every key the console answers to**, as a view you can leave open beside what you are
  doing rather than only an overlay behind the question mark. It reads the same list the overlay does.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay manual`: quay describing itself, to be loaded as a session's context.** A session sitting in
  the panel beside the console knew nothing about the crew it was next to. Pipe it where it is needed:
  `quay manual | quay context set me/house-bills`. Most of it is assembled rather than written, from the
  tool's own usage and the behaviour specification embedded in the binary, so neither half can drift
  from what the tool actually does. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The panel's header spans the full width, above both halves.** Three regions now: the header across
  the top, then the console and a conversation side by side underneath. A tmux pane is a rectangle, so
  the header is a pane of its own, given exactly the rows it needs. With the whole width it lays its
  key hints out in columns instead of one.
  The console draws no header inside the panel and says which view it has moved to, so the header names
  that view's keys rather than the keys of whichever view the panel opened on.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The panel opens from inside tmux.** It was being built correctly and then never shown: tmux
  refuses to attach a client that is already inside one, so the two panes sat there running while the
  terminal said `sessions should be nested with care` and nothing appeared. From inside tmux the
  client is switched to the panel instead of attaching a second one.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay panel`: the console and a conversation, side by side, half the width each.** The console
  shows the crew and a conversation shows one session, and using both meant losing sight of one. tmux
  does the splitting, the same tmux that already keeps an open conversation alive behind `ctrl-q`.
  Named a session it opens that one; named nothing it opens the conversation you were last in, and
  refuses rather than opening half a panel when there is none.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

## 6 August 2026

- **A failed task says why.** Every model failure read `run task: model: run exited: exit status 1`,
  which is the same sentence for an expired token, a network failure, a missing model in the sandbox
  image and the model refusing outright. It now carries the reason: the model's own words where it got
  far enough to say anything, and what came back from the sandbox where it did not. A rejected token
  now reads `Failed to authenticate. API Error: 401 OAuth access token is invalid. (status 401)`, and
  a sandbox with no model in it names the binary it could not find.
  ([#51](https://github.com/atlantic-blue/quay-crew/issues/51))
- **Nothing a failed task says can carry the subscription token.** A task runs with the token in its
  environment, so every place a failure can quote is a place it turns up. Values passed in this task's
  environment are matched exactly, and the published token shape is matched as well for one this
  process never held.

## 5 August 2026

- **The wizard makes one thing at a time.** `n` asks what to make first, then only the questions that
  thing needs, and where a question needs a parent it offers what the crew already has instead of a
  blank name. A workspace, a project, the subscription token, a project's context or a session, each on
  its own. It could previously only make a whole new crew from nothing, so there was no way to add a
  project to a workspace you had, or set a token on one. Making a project makes a project and nothing
  else. ([#138](https://github.com/atlantic-blue/quay-crew/issues/138))
- **The wizard closes when it has made what it was asked for.** It made everything correctly and then
  stayed drawn over the list it had already refreshed, so nothing looked like it had happened, and the
  next enter was taken as an answer to the step that was already working, whose prompt is the words
  `making it`, so the wizard read as making nothing at all. A key other than escape now does nothing while the crew is making it, because the wizard is
  asking nothing at that point. ([#140](https://github.com/atlantic-blue/quay-crew/issues/140))
- **A wizard that makes things.** `n` in the console asks for a workspace, a project, the subscription
  token, the project's context and a first message, in the order they depend on each other, and makes
  them in one pass at the end. Escape at any step makes nothing and forgets everything, including a half
  typed token. The token is never echoed. The command line could already create all five; the console
  could create none. ([#139](https://github.com/atlantic-blue/quay-crew/pull/139))
- **The key list stops cutting its longest entries.** Columns are as wide as the widest entry rather
  than an even share of the room, because an even share truncates and a key list missing its last few
  characters is one that lies.
- **Leaving a conversation is `ctrl-q`, and ending one no longer takes the work with it.** A conversation
  runs through a wrapper that keeps its terminal alive: pressing ctrl-d twice used to end the tmux
  session and everything the model was in the middle of, and now it says the conversation is closed and
  waits, with enter to open it again. The status line says how to leave, because it was off and there
  was nothing on screen telling anybody. ctrl-q works because the wrapper tasks off flow control first,
  which is the only reason that key can be a key.
  ([#137](https://github.com/atlantic-blue/quay-crew/pull/137))

## 4 August 2026

- **A view of what each workspace has set.** `:secrets` in the console and `quay secret list`, naming
  the workspace and the secret and saying `set, and not shown anywhere` where a value would be. There is
  no call that returns a value and no field for one, so this cannot leak by mistake rather than by
  policy. ([#136](https://github.com/atlantic-blue/quay-crew/pull/136))
- **The console shows a session's history.** `l` on a session opens a `tasks` view of what it was
  asked and what came back, read from the projection, so it answers without starting a container and
  keeps answering long after the sandbox is gone. A failed task shows why it failed where the reply
  would be. Enter still opens the conversation: that is the thing an operator does most on that row,
  so it keeps the cheapest key, and there is a scenario that fails if anything bound to enter starts
  descending instead. ([#134](https://github.com/atlantic-blue/quay-crew/issues/134))
- **The subscription token survives a restart.** Secrets were held in memory, so every `make up` lost
  the token and the next task failed saying nothing useful. They are kept in Postgres now, sealed with
  a key made once and kept on the host at `~/.quaycrew/data/secrets.key`, so holding the database is
  not enough to read one. The status block says `Secrets: postgres, sealed`, and says it in red when
  they are still in memory. ([#133](https://github.com/atlantic-blue/quay-crew/pull/133))
- **A session's history can be read back.** Tasks went onto the event log and nothing read them, so a
  conversation was write only from the outside. A projection now consumes every workspace's task
  stream, by pattern rather than by a list fixed at startup so a workspace created while the crew is
  running is read too, and materialises it into a `tasks` table. `quay tasks <session>` prints what a
  session was asked and what came back, in the order it happened, without starting a container and
  long after the sandbox is gone. Delivery from a log is at least once, so each event carries an id
  and writing the same one twice leaves one task: there is a conformance test for that against both
  stores, and an integration test that runs it against a real broker.
  ([#130](https://github.com/atlantic-blue/quay-crew/issues/130))
- **Every level of context is visible and settable, from both surfaces.** The crew's own level is in
  every listing, `quay context set [<address>|crew]` takes it from a file on standard input, and the
  console edits any level in your editor through a scratch file rather than the rendered one, because a
  level is rendered into every session that reads it and there is no single file to open. That is the
  path for moving what you already have into the crew.
  ([#131](https://github.com/atlantic-blue/quay-crew/pull/131))
- **Every task is written to the event log.** The broker had run in the stack for weeks holding zero
  topics, because the boundary was built and nothing on either end of it was. The control plane now
  publishes a task to `<workspace>.tasks` whenever one runs, keyed by session so a conversation stays
  in order, carrying the prompt, the reply, the status and where the session sits, so a consumer never
  has to query the store to know what it is reading. A task that failed is published too, because that
  is the one somebody comes looking for. Publishing never fails a task: the task already happened, and
  a broker that is unreachable is logged and dropped. A stack with no `QC_KAFKA_SEEDS` runs tasks and
  says out loud that nothing records them. The topic is created on first use, which an integration
  test against a real Redpanda caught: the very first record to a new workspace was being rejected and
  quietly dropped. ([#128](https://github.com/atlantic-blue/quay-crew/issues/128))
- **Four levels of context, and a working directory per session.** Crew, workspace, project and session,
  layered into the two files the model actually reads: the outer two in the conversation store every
  session in a workspace sees, the inner two in that session's own working directory. Sessions no
  longer share a project's working directory, which is what makes the innermost level possible and
  stops two conversations changing files under each other. What something inside a sandbox writes is
  read back into the level it belongs to, and a note appended at the end lands on the session.
  ([#124](https://github.com/atlantic-blue/quay-crew/pull/124))
- **`make up-observability` starts all four services.** Tempo was pointed at `/etc/tempo.yaml`, a file
  neither in its image nor mounted, so it exited on startup and the profile quietly came up one short.
  Loki and Tempo are now configured from `deploy/loki.yaml` and `deploy/tempo.yaml`, kept here rather
  than left to an image default that can move underneath the stack, and a test refuses any service
  that names a config file nobody provides, so the next one cannot fail the same way silently.
  ([#126](https://github.com/atlantic-blue/quay-crew/issues/126))
- **The database doc covers the `contexts` table, and says session throughout.** It was written hours
  before context moved into the store and before the console went back to the word the database uses,
  so it described five tables and called a session a session. Six tables now, with what `scope` and
  `owner` mean and why that table has no foreign key, plus a query for what the model has been told.
  ([#123](https://github.com/atlantic-blue/quay-crew/issues/123))
- **Observability is documented, including the part that does not work.**
  [`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md) says which of the three signals is real: structured
  logs are, and no span or metric is created anywhere in the codebase, so the OpenTelemetry wiring
  exports nothing and the collector discards what it receives. It also records that `tempo` in the
  compose profile is pointed at a config file that is neither in the image nor mounted, so it cannot
  start, and that Grafana has no data sources provisioned. What to read when something is wrong, and
  the order the three open issues have to land in.
  ([#121](https://github.com/atlantic-blue/quay-crew/issues/121))
- **The console says sessions again.** It was sessions for a day. The database and the API both say
  session, and one name across the whole system beats a console that translates. `sessions`, `session`
  and `t` still open the view, the way `sessions` did while it was called sessions.
  ([#120](https://github.com/atlantic-blue/quay-crew/pull/120))
- **Context lives in the database, and the file in a sandbox is a rendering of it.** It was only ever
  files on one machine, which works nowhere else: a pod has no host directory to bind mount and an API
  cannot edit a file on somebody's laptop. Setting it writes the file too, so a running sandbox picks it
  up on its next task, and **what an agent writes into its own memory is read back into the store**
  rather than overwritten, because an agent that cannot write down what it learned is the problem this
  project exists to solve. ([#119](https://github.com/atlantic-blue/quay-crew/pull/119))
- **The database and the event log are documented.** [`docs/DATABASE.md`](docs/DATABASE.md) covers why
  a session survives a restart at all, how to shell in with psql, what every table and column means, the
  queries worth knowing, and why reading from the prompt is safe while writing from it is not.
  [`docs/EVENTS.md`](docs/EVENTS.md) covers what the log is for, how to inspect Redpanda with `rpk`,
  how topics are named, and the state it is actually in: the boundary and its client exist, nothing
  publishes to it and nothing consumes it, so a stack today holds zero topics. Documentation only, no
  behaviour changed. ([#113](https://github.com/atlantic-blue/quay-crew/issues/113))
- **The command bar says what it can open.** Pressing `:` asked a question and gave nothing to answer
  it with, so the only way to learn a view's name was to know it already. It now lists them and narrows
  as you type, and `?` has a views section saying what to type for each.
  ([#116](https://github.com/atlantic-blue/quay-crew/pull/116))
- **The detach key works.** It was `ctrl-o` for one release, and on macOS `^O` is the terminal's own
  DISCARD character, so the line discipline swallows it and tmux never sees it: the key did nothing.
  It is `ctrl-space d` now, `ctrl-b d` still works when nothing is nested, and a test refuses every
  control character a terminal reserves rather than the one spelling that broke.
  ([#115](https://github.com/atlantic-blue/quay-crew/pull/115))
- **Editing context works with no `EDITOR` exported.** It refused rather than falling back, which made
  the whole thing dead on any machine that has not set one, which is most machines. `VISUAL`, then
  `EDITOR`, then `vi`, which is what git and crontab do.
  ([#114](https://github.com/atlantic-blue/quay-crew/pull/114))
- **You can edit context from either surface.** `enter` or `e` on a row in the `context` view opens the
  memory file in your own `$EDITOR`, and `quay context edit [<address>]` does the same from the command
  line. The directory is made first, so an editor writing into a project whose sandbox has never run
  does not fail on a path nobody created, and an unset `EDITOR` is said out loud rather than guessed at.
  ([#112](https://github.com/atlantic-blue/quay-crew/pull/112))
- **You can find the files the model reads.** `quay context` on the command line and a `context` view in
  the console, both clients of one control plane call: a row per workspace and per project, where it is
  on your machine, where it appears inside a sandbox, and whether `CLAUDE.md` has been written yet. The
  mounts have existed for a while and nothing said where they were, so the feature worked and nobody
  could find it. ([#111](https://github.com/atlantic-blue/quay-crew/pull/111))
- **You can leave an open session without ending it.** Opening a conversation handed the terminal to
  `claude --resume` and the only way back to the list was to end it. It now runs inside tmux in the
  session's own sandbox, so `ctrl-b d` leaves the model running and returns you to the console, and
  opening the session again lands in the same live conversation. The sandbox image carries tmux, with
  `ctrl-o` as its prefix so it still works when you opened the console from inside your own tmux, and
  `ctrl-b` as a second prefix for when nothing is nested.
  ([#109](https://github.com/atlantic-blue/quay-crew/pull/109))
- **The key list stops silently dropping keys.** It folded into two columns and then cut whatever did
  not fit, so adding a binding pushed the last one off the bottom with nothing to say so. It folds into
  as many columns as it takes.
- **Opening a session runs in the mode that session is set to.** The attached session carried no mode at
  all, so a session armed to skip permissions stopped and asked the moment you opened it, which reads as
  the toggle not working. ([#107](https://github.com/atlantic-blue/quay-crew/pull/107))
- **The daemon is the source of truth about containers, not a map in the control plane.** It remembered
  every sandbox it had made and trusted that memory forever, so anything that removed a container
  behind its back left a handle pointing at nothing and handed the operator a name Docker had never
  heard of: `No such container: quaycrew-1edc8349315233e36bf4fd53`, over and over. Every task and every
  attach now asks the provider, which adopts the container already carrying that name or makes one.
  ([#106](https://github.com/atlantic-blue/quay-crew/pull/106))
- **A session's permission mode, shown and toggled.** Every task ran `acceptEdits`, hardcoded, and no
  operator could see it or change it. The mode now belongs to the session and survives a restart, the
  listing has a `MODE` column reading `edits`, `plan` or `dangerous`, and `D` in the console flips the
  selected session between asking and skipping every permission, through the same confirmation as the
  other keys that change what a session is. A mode the model does not understand is refused rather than
  handed to it, and `bypassPermissions` is refused outright when tasks run on the host instead of in a
  container. ([#105](https://github.com/atlantic-blue/quay-crew/pull/105))

## 3 August 2026

- **The refusals are in the operator's words, and name the session on their screen.** "Its conversation
  is gone, it predates state on the host" is a sentence only somebody who worked on this understands,
  and it named a twenty four character identifier that appears nowhere in the list. Attach now says
  what happened and what to do, about `session 34e1a6c7` rather than `session 134c2c6dbf1e907413753cc5`.
  ([#103](https://github.com/atlantic-blue/quay-crew/pull/103))
- **A session whose conversation is gone says so.** A session's handle points into a store the crew does
  not own, so it can outlive what it points at: every conversation from a sandbox built before state
  was kept on the host died with that container while the row kept the handle. Resuming one printed
  `No conversation found` inside the container and exited, which from the console looked like nothing
  happening. Attaching now checks the workspace's store first and says to dispatch a task instead.
  ([#102](https://github.com/atlantic-blue/quay-crew/pull/102))
- **Opening an idle session works again.** Attaching answered from the database row alone, so after the
  control plane restarted it handed back a container name the daemon had never heard of:
  `No such container: quaycrew-134c2c6d...`. Attaching now starts the session's sandbox when there is
  not one, and creating a sandbox adopts the container already carrying that name instead of colliding
  with it. ([#101](https://github.com/atlantic-blue/quay-crew/pull/101))
- **`r` refreshes the view.** It restarted a session for one afternoon. Refreshing is the key you reach
  for constantly, so it holds the short obvious letter; restart moved to `R`, beside `A` for archive,
  and `g` still refreshes too. ([#99](https://github.com/atlantic-blue/quay-crew/pull/99))
- **A session can be put away, and brought back.** `A` archives one through the same confirmation, and an
  `archived` view lists what was put away with `u` to restore it. Archiving stops the session, closes its
  sandbox and hides it from the default listing. Nothing is deleted: the row, the conversation and the
  project files all stay. ([#97](https://github.com/atlantic-blue/quay-crew/pull/97))
- **A stopped session can be restarted, with its container.** `r` in the console, `RestartSession` on the
  control plane: back to idle with the sandbox already running, so you can attach into the conversation
  instead of dispatching a task to make the container exist. Only safe because a session's state lives
  on the host. ([#96](https://github.com/atlantic-blue/quay-crew/pull/96))
- **A destructive key asks first, and backspace stops a session.** `stop session d754610f?`, drawn where
  the command bar draws. Yes acts, and every other key cancels, because an accidental cancel costs one
  keypress and an accidental yes costs a conversation. `x` still stops, through the same question.
  ([#95](https://github.com/atlantic-blue/quay-crew/pull/95))
- **Enter opens a session's conversation.** A session has nothing to drill into, so enter did nothing
  at all on the one view where the obvious key has an obvious meaning. `a` still works, and the
  question mark now lists every key an action answers to.
  ([#93](https://github.com/atlantic-blue/quay-crew/pull/93))
- **The console calls them sessions.** The view, its panel title and the breadcrumb say sessions, because
  a row in that list is one conversation. The control plane still calls the running session a session,
  which is a real distinction inside it and means nothing to somebody reading a list of fourteen rows.
  `sessions`, `session`, `sess` and `s` all still open the view.
  ([#90](https://github.com/atlantic-blue/quay-crew/pull/90))
- **A flag is refused rather than swallowed.** `quay dispatch --project default "hello"` used to make
  the flag and its value the first two words of the message, and then complain about the workspace. The
  flags addresses replaced are named, with what to type instead, and any other flag is refused too, so
  the next thing replaced by an address cannot repeat it.
  ([#89](https://github.com/atlantic-blue/quay-crew/pull/89))
- **The status block says what is true, in engines.** `Sandbox engine`, `Store engine` and
  `Events engine`, `Workspace` and `Project` rather than a borrowed `Context`, and the state line names
  where a conversation is kept instead of promising it survives. The events line says
  `none, nothing reads or writes the log yet`, which is the truth: Redpanda is in the compose stack and
  no service is connected to it. ([#87](https://github.com/atlantic-blue/quay-crew/pull/87))
- **`make upgrade` brings the stack back the way you configured it**, and clears the sandboxes from
  before the upgrade. Two bugs: it restarted compose with the defaults, so a stack started with
  `QC_MODEL=claude-code` came back running `echo`, and it left every old sandbox running, which blocks
  those sessions from ever starting again because the control plane has forgotten them and their names
  are taken. Configuration now lives in `deploy/.env`, which compose reads on every command.
  ([#86](https://github.com/atlantic-blue/quay-crew/pull/86))
- **The console says when the control plane is older than the tool**, rather than quietly showing four
  fewer lines: `Quay: this control plane is older than the tool, run make upgrade`. Installing the tool
  does not rebuild the stack, so this is the normal state of things after an upgrade, and silence reads
  as the console being broken. ([#81](https://github.com/atlantic-blue/quay-crew/pull/81))
- **`quay features`**, and a `features` view in the console: what this build can do and what proves it,
  read from the behaviour specification embedded in the binary. It asks the control plane nothing,
  because a capability belongs to the build rather than to a running stack, and the question is usually
  asked by somebody who has not started one yet. A hand written list would drift from the scenarios;
  this one cannot. ([#82](https://github.com/atlantic-blue/quay-crew/pull/82))
- **`make upgrade`**: fetch, fast forward, rebuild the tool and the images, restart the stack. One
  command for "bring everything to the latest", because `make install` only ever builds the tool and a
  new tool against an old control plane is the mismatch that costs an afternoon. It refuses on a
  branch, on a dirty checkout, and when it cannot reach the newest build, rather than quietly
  rebuilding the stack from something else.
  ([#80](https://github.com/atlantic-blue/quay-crew/pull/80))
- **The console header reads like k9s**: the status block says which build, which control plane, where
  you are standing, and what a task would run in; this view's own commands sit beside it as
  `<a> Attach`; `?` lists every key; the panel title is centred; the sorted column is marked `SESSION↑`;
  and the wordmark sits on the right when there is room for it.
  ([#77](https://github.com/atlantic-blue/quay-crew/pull/77))
- **`quay version`**, and every build stamped with the commit it came from, marked dirty when the
  checkout had uncommitted changes. Part of #74; `quay update` and release tags are still open.
  ([#77](https://github.com/atlantic-blue/quay-crew/pull/77))
- **`make install` installs over the copy your shell actually runs**, and says which commit it built,
  so a stale binary earlier on your PATH cannot quietly keep serving you yesterday's tool. Set
  `BINDIR` to put it somewhere specific. ([#73](https://github.com/atlantic-blue/quay-crew/pull/73))
- **The console says which crew you are on and what you can press.** A status block naming the
  address, the model backend, the sandbox, the store and whether a conversation outlives its
  container, with the key hints beside it. The rows sit in a framed panel titled with their scope and
  count, `sessions(house-bills)[3]`, ordered by a column marked with an arrow, and the breadcrumb at
  the bottom reads `me > house-bills > sessions` so it is clear what escape goes back to.
  ([#72](https://github.com/atlantic-blue/quay-crew/pull/72))
- **The control plane says what it is running**: the model backend a task runs against, what a session
  is isolated in, where workspaces and sessions are kept, and whether a conversation outlives its
  container. Two stacks look identical from a list of sessions and behave nothing alike.
  ([#71](https://github.com/atlantic-blue/quay-crew/pull/71))
- **A session's conversation and files outlive its container.** Every sandbox mounts two directories
  from the host: the workspace's conversation store at `/home/agent/.claude` and the project's working
  directory at `/home/agent/workspace`. Before this, stopping a session ran `docker rm -f` and deleted
  the conversation the database still held a handle to.
  ([#66](https://github.com/atlantic-blue/quay-crew/pull/66))
- **Shared context, through the same directories.** A workspace and a project each own a directory the
  sandbox mounts, so the memory the model already reads from `CLAUDE.md` is a file you edit rather than
  text this tool assembles. Both are read write, because the model writes its own store into one of
  them. ([#66](https://github.com/atlantic-blue/quay-crew/pull/66))
- **The crew is addressed by path, from a current context.** `quay use me/house-bills` and then
  `quay dispatch "..."`, with the place kept in `~/.config/quay/context`. Creating something moves you
  into it. An address typed on a command applies to that command only, and a session is the third level,
  so standing in one continues that conversation. Replaces `--workspace`, `--project` and `--session`.
  ([#69](https://github.com/atlantic-blue/quay-crew/pull/69))
- **Names have to be addressable.** A workspace or project name is lowercase letters, digits and
  hyphens, refused otherwise with a suggestion that would work. A name is half of an address, so it has
  to survive being typed without quoting. ([#68](https://github.com/atlantic-blue/quay-crew/pull/68))
- **Attach to a session's conversation, not just a shell in its sandbox.** `quay attach <session>`, or
  `a` in the console, runs the model's own resume inside that session's container, so you land in the
  conversation with its history. Shelling in shows you the room; this shows you the conversation.
  ([#61](https://github.com/atlantic-blue/quay-crew/pull/61))
- **The sandbox carries the workspace's environment from the moment it is created**, so attaching needs
  no credential from your shell and no tool has to carry a token around.
  ([#62](https://github.com/atlantic-blue/quay-crew/pull/62))
- **Projects, between a workspace and its sessions.** A workspace is who you are, a project is a body of
  work, a session is one conversation. A session identifier is unique inside its project, which is the
  reason the level exists. ([#59](https://github.com/atlantic-blue/quay-crew/pull/59))
- **`project` renamed to `workspace`** throughout, because the level that already existed was the
  tenancy one, and the word for a body of work inside it was needed.
  ([#58](https://github.com/atlantic-blue/quay-crew/pull/58))
- **Names and short identifiers everywhere a listing prints.** `5d013d07  me/house-bills  session
  d754610f  idle` rather than three lines of hexadecimal.
  ([#53](https://github.com/atlantic-blue/quay-crew/pull/53))
- **A project can be addressed by its name**, not only by the identifier printed once at creation. An
  id still wins, and an ambiguous name is refused with the candidates rather than guessed.
  ([#50](https://github.com/atlantic-blue/quay-crew/pull/50))
- **Attaching says why it cannot proceed** when a session has never had a task, or has been stopped,
  instead of opening something that immediately errors.
  ([#63](https://github.com/atlantic-blue/quay-crew/pull/63))
- **The sandbox image ships past the model's first run.** Onboarding and the trust prompt are already
  answered, so attaching lands in the conversation rather than a theme picker, which reads exactly like
  a broken token. ([#64](https://github.com/atlantic-blue/quay-crew/pull/64))

## 2 August 2026

- **The quay console.** `quay` with no arguments opens a full screen view of the crew, in the shape of
  k9s: `:` to switch resource, `/` to filter, enter to drill in, `s` to shell into a session, `x` to
  stop it. Adding a view is declaring a resource, which is the deliverable rather than the two views it
  ships with. ([#48](https://github.com/atlantic-blue/quay-crew/pull/48))
- **Workspaces and sessions survive a restart**, in Postgres, behind one store interface with an in
  memory implementation held to the same conformance suite. A failed task never erases the conversation
  handle, because that handle is the only pointer to a conversation the model keeps on its own disk.
  ([#44](https://github.com/atlantic-blue/quay-crew/pull/44))
- **Behaviour specifications.** [`features/`](features/) states what the product does, driven against
  the real control plane interface over an in process connection. The suite fails if it finds no
  feature files, because a run that proves nothing otherwise reports success.
  ([#41](https://github.com/atlantic-blue/quay-crew/pull/41))
- **Automation graphs designed** as a pure reducer over the event log, in
  [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Legibility is what is wanted at that layer, not
  power: knowing what an arbitrary reducer does means running it.
  ([#43](https://github.com/atlantic-blue/quay-crew/pull/43))

## 11 July 2026

- **Real Claude tasks, on your subscription, inside the sandbox.** The image carries the Claude Code
  command line tool and no credentials; the token is a workspace secret, injected into the session's
  sandbox. No API cost. ([#38](https://github.com/atlantic-blue/quay-crew/pull/38))
- **A task runs in a Docker sandbox**, one long lived container per session, proved by continuous
  integration dispatching a real task against the composed stack. Asserting the services were merely
  running had let a stack ship that could not execute a single task.
  ([#37](https://github.com/atlantic-blue/quay-crew/pull/37))

## 8 to 10 July 2026

- **The event log**, consumed by group, committing only after a batch is handled, so delivery is at
  least once and handlers have to be idempotent.
  ([#31](https://github.com/atlantic-blue/quay-crew/pull/31))
- **The `quay` command line tool**, a synchronous client of the control plane.
  ([#30](https://github.com/atlantic-blue/quay-crew/pull/30))
- **The control plane**: the gRPC service, the model adapter, and the sandbox provider, each behind an
  interface so nothing downstream depends on an implementation.
  ([#29](https://github.com/atlantic-blue/quay-crew/pull/29))
- **The channel message contract**, so a chat app and the command line tool say the same thing.
  ([#28](https://github.com/atlantic-blue/quay-crew/pull/28))
- **The scaffold**: one Go monorepo, a service per component, the compose stack, and continuous
  integration. ([#27](https://github.com/atlantic-blue/quay-crew/pull/27))

## Not shipped, despite appearances

Things the documentation has claimed and the code does not do yet, listed here so nobody plans
around them:

- **Only the crew's own handling of a message is traced.** There is no span around a turn, a sandbox
  or the model, and the command line tool starts no trace, so a trace stops at the edge of the
  control plane. ([#3](https://github.com/atlantic-blue/quay-crew/issues/3))
- **A turn in the `turns` table carries no trace id**, so history and traces cannot be joined the way
  logs and traces now can. ([#3](https://github.com/atlantic-blue/quay-crew/issues/3))
- **There are no dashboards and no alerts**, so there is no cost ceiling that fires. Nothing measures
  the host, a session's processes, or a GPU.
  ([#16](https://github.com/atlantic-blue/quay-crew/issues/16))
- **Nothing creates a metric instrument.** The meter provider is wired and never used, so no token
  count, no cost and no resource number leaves the crew.
  ([#16](https://github.com/atlantic-blue/quay-crew/issues/16))
- **The telemetry stack is not connected.** Grafana, Loki, Tempo and Prometheus are in the compose file
  with nothing joining them up. Spans now reach the collector and stop there, because its only
  exporter is `debug`, and Grafana comes up with no data source.
  ([#12](https://github.com/atlantic-blue/quay-crew/issues/12))
- **Only the crew's own handling of a message is traced.** There is no span around a turn, a sandbox
  or the model, and the command line tool starts no trace, so a trace stops at the edge of the
  control plane. ([#3](https://github.com/atlantic-blue/quay-crew/issues/3))
