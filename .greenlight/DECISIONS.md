# Decisions: a project carries its own context

Seeded 2026-09-04 from the design session in the worktree
`design-a-project-carries-its-own-context`.

Two kinds of entry. A settled decision states what is true and why. A proposed decision waits for
The operator, and nothing is built on it.

Append the moment a decision is made, in the same turn. Never argue with a settled entry. If it looks
wrong, raise it as a question and let the operator change it.

## Settled in this session

**The design and the path are context, not orchestration.**
- Status: settled, and it is the whole claim of this design.
- Why. The job subsystem died on 3 September 2026 because a controller sat above the session. A
  refusal never reused the session that did the work, so each ask ran four workers again from
  nothing. That day cost about 1.23 billion cache read tokens and delivered one column on one
  listing.
- What follows. Rows the project owns, rendered into the files a session already reads. No stage, no
  controller, no fan out. The system never starts a session by itself.
- Source: `internal/store/migrations/0060_remove_jobs_flows_and_roles.up.sql`, commit `f323024`,
  pull request 693, and the hub decisions file entry of 3 September 2026.

**The guard, so a later change can be measured.**
- Status: settled.
- Any component that dispatches a session without the operator asking is the controller. Refuse it.
- Any stage word on a project or a session, with something that moves the row between values, is the
  same failure under a different name. Refuse it.

**The design belongs to the project, not to a piece of work.**
- Status: settled.
- Why. A project is read many times. A job was declared once and thrown away. The record earns its
  storage only on a thing that outlives a session.

**The design lives in the store and renders into a file.**
- Status: settled, subject to proposed decision 1 below.
- Why. Migration 0006 already answered this for context. A pod has no host directory to bind mount.
  An interface cannot edit a file on somebody's laptop. A project's repository field is optional.
- Rejected: files in a repository as the truth.

**The step text is the dispatch text.**
- Status: settled.
- Why. `Dispatch` already carries text to the model. Composing the step into that text needs no
  change to `Dispatch`, no change to `internal/model`, and no change to the sandbox.
- Rejected: a new field on `DispatchRequest`. Rejected: the `--append-system-prompt` flag on the
  model command line.

**The control plane composes the step text, not the command line tool.**
- Status: settled.
- Why. The console and the command line must send the same words. The system's own words belong in
  one place.

**A step is a resource the project owns.**
- Status: settled.
- Why. An exec is ephemeral and belongs to one session. A step outlives the session that took it, and
  a failed step is taken again by a different session.
- Rejected: putting the step on an exec.

**The design carries an approval flag beside the body, and any write to the body clears it.**
- Status: settled.
- Why. Migration 0050 states the first half. A design somebody sent back for a rewrite carries the
  same text as a design nobody read. Only a flag tells those two apart. The second half is new:
  approval is a statement about a specific text.

**A step's finish is what somebody declared, never what the system observed.**
- Status: settled.
- Why. Migration 0047 states it. Nothing can see inside a container, and a session that dies takes
  with it everything it did not write down.

**Empty string and false, never null, in every new column.**
- Status: settled.
- Why. Every existing migration in this repository does this. A reader that must tell null from empty
  is a reader with two cases where there is one.

**The word is "path", not "plan".**
- Status: settled.
- Why. `plan` is already a permission mode. Two meanings for one word in one tool is the fault the
  vocabulary migrations kept fixing.

**The words "job", "flow", "role", "stage" and "controller" do not appear.**
- Status: settled.
- Why. Each one names a thing that was removed. Reusing one would make the refusal tables lie.

**Nothing inside a sandbox can set the approval.**
- Status: settled.
- Why. A session may write a design body. Approval reaches the store only through the operator's own
  command. This is the boundary that makes the guard real.

**The dead role brief path is removed before anything is built on it.**
- Status: settled.
- Why. `internal/controlplane/server.go` lines 585 to 599 look exactly like a working mechanism for
  handing a session a brief. It is inert. `brief` is assigned the empty string and never reassigned,
  and an `if false` branch remains. Neither `go vet` nor `golangci-lint` reports it, because
  `staticcheck` is not in the enabled linter set.

**The riskiest assumption is named and is not yet proved.**
- Status: settled as an entry. The assumption itself is open.
- Assumption. A session starts holding a design, the project context and one atomised step. It then
  produces work the operator accepts more often than a session that starts with a line of text.
- Proved where. Nowhere yet.
- The proof is milestone 2 of the roadmap, and it sits before every milestone that costs real work.

## Proposed, waiting for the operator

Nothing is built on any of these.

**Decision 1. Where the design and the path live.**
- Options. A: the store only. B: the store, plus a file a session commits into the repository. C:
  repository files as the truth.
- My recommendation: A now, B recorded as deferred.
- Why. The interview names a third reader, a person who was not in the conversation. That person
  reads a forge, so B serves them. B also adds a commit, a push and a branch to the design stage,
  which is the machinery that made the last attempt expensive.

**Decision 2. How a step gets marked done.**
- Options. A: the operator says done, with one command and the result. B: the session writes a marked
  section into its memory file and the control plane reads it back. C: an ordinary session gets a
  narrow credential for one call.
- My recommendation: A.
- Why. It matches migration 0047. It needs no credential. B has a real defect: the read back happens
  on the next dispatch, so the record lags. C adds authentication surface.

**Decision 3. How the design reaches the model.**
- Options. A: a summary in the memory file plus a pointer to a file beside it. B: a pointer only. C:
  the whole design inlined in the memory file.
- My recommendation: A.
- Why. I checked this rather than assumed it. An import in a memory file loads eagerly and inlines,
  so splitting a file saves no context. C therefore taxes every unrelated session in the project on
  every exec.

**Decision 4. Does an unapproved design refuse the command that takes a step?**
- Options. A: refuse, and say to approve first. B: warn and dispatch anyway. C: no gate.
- My recommendation: A.
- Why. Your rule is that no code exists before you approve the path. A is the only option that makes
  it real. The refusal costs one line and starts nothing, which is what makes it different from the
  gate that failed. That gate refused a controller, which then rebuilt the world.

**Decision 5. One predecessor per step, or a full list.**
- My recommendation: one, with the full graph deferred.
- Marked as an inference, not an observation. I did not measure how many real paths need two
  predecessors, because no such path exists in this system yet.

## Settled on 2026-09-04

The operator answered decisions 1 to 4. Each one took the recommendation above. Status: accepted.

- Decision 1. The design and the path live in the store only. Accepted.
- Decision 2. The operator marks a step done, with one command and the result. Accepted.
- Decision 3. The design reaches the model as a summary of about 400 characters plus a pointer to
  `.krewe/design.md`. Accepted.
- Decision 4. An unapproved design refuses `krewe step take`. Accepted.
- Decision 5. Not put to the operator. It stays at one predecessor per step. Status: proposed.

## Deferred, recorded so it is not lost

- The design committed into the project's repository. Decision 1, option B.
- A full dependency graph on a step. It needs its own table and a cycle check.
- Project level and session level attachment for skills and hooks.
- A secret scanner over the design body. Nothing scans context either.
- Taking a step from the console with a key press.
- Visual acceptance evidence on a step. The hub decision of 2 September 2026 covers what that means.
- A path that spans two projects. Probably wrong: a path belongs to one project.

## Settled on 2026-09-04, after the operator read the first version

The operator read the design and said it answered the wrong question. The problem is not the fan out and it is not the dispatch. The
problem is trust. The operator must know what the tool builds, and that it built it right.

Four decisions followed. Each one is accepted. Do not reopen any of them.

**Decision 6. A session proves it understood before it builds.**
- Status: accepted.
- The session writes back what it understood of the step, then stops. It writes no code until the
  operator approves the restatement.
- This is a second command per step. The operator accepted that cost.

**Decision 7. Krewe checks the finished work against the proof the step promised.**
- Status: accepted.
- The step's proof must state the value the step delivers, and the scenario must describe that value.
- The operator judges that when the operator approves the design.
- Krewe checks only that the named scenario exists and passes. It never judges value.

**Decision 8. `krewe step done` refuses a step whose promised proof is absent or does not pass.**
- Status: accepted, then narrowed by decision 10 on the same day. It now refuses a step nothing checked. It no longer refuses a failing verdict, because the word done belongs to the operator.

**Decision 9. `krewe step take` refuses while the step before it is not done.**
- Status: accepted. The wording moved from "has no passing proof" to "is not done" with decision 10.
  Done is now the operator's word rather than the run's.
- The refusal lands on both commands, `take` and `done`.

## What follows from decisions 6 to 9

Each entry below is settled, and each one is a consequence rather than a new choice.

**A step's finish is checked, and its result is still what somebody wrote.**
- Status: settled. It supersedes the earlier entry that says a finish is only what somebody declared.
- Why. That entry read migration 0047 correctly about the container and wrongly about the work. The
  scenario, the run and its output sit outside the sandbox, so the control plane can run them.
- The result text on a step is still the operator's words. Decision 2 stands. What changed is that the
  word done now also requires a run that passed.

**The restatement travels through the memory file the session already writes.**
- Status: settled.
- Why. It needs no credential, no new mount and no new mechanism. The session writes a marked section
  into its own inner memory file. `syncContextExcept` reads it back.
- Rejected: a narrow credential so the session can write it through a remote procedure call. Rejected:
  a file the control plane polls.
- The mark must be named in the scope list `syncContextExcept` builds. A mark this build does not know
  is swept into the innermost level. That level stores it as though a person typed it, and the next
  exec renders it again underneath itself. `internal/sandbox/memory.go` records that defect for the
  skills index.
- `renderContext` renders the restatement back from the store while the step is taken, and stops once
  it is done or stopped. Without the render the next exec would overwrite the file and lose the text.

**The restatement is read back on demand, not only at the next dispatch.**
- Status: settled.
- Why. The operator must read the restatement before approving, and the approval is what dispatches.
  A read that waited for a dispatch would arrive after the moment it is needed.
- Reading a memory file from the host needs no container and no model.

**Approving the restatement dispatches the build, to the same session.**
- Status: settled.
- Why. It costs one extra command per step rather than two, and it reuses the session that did the
  restating. Reusing the session is the lesson of 3 September 2026.

**The proof runs in the session's sandbox, and krewe makes a container when the session was
reclaimed.**
- Status: settled by the operator on 2026-09-04. It supersedes the earlier entry, which refused a
  reclaimed session and left a step that nothing could close.
- Why. `Provider.Existing` answers with the running container. When there is none, krewe starts one
  and restores the working tree into it.
- The cost is one container start on a reclaimed session, and the command says so before it waits.
- The run costs no model tokens. It is an exec with a budget, set per project.

**The proof command carries a placeholder for the scenario name, and a command without it is refused.**
- Status: settled.
- Why. A command that runs the whole suite passes while proving nothing about this step.

**A run that reports zero scenarios is a failure.**
- Status: settled.
- Why. A name filter that matches nothing prints success in most runners, so an exit status alone is
  not evidence. Krewe reads the count of scenarios that ran as well as the status.
- Observed: `features/suite_test.go` already fails its own run when that count is zero.

**The four step state words do not grow.**
- Status: settled.
- Why. The state says who holds the step. The flags and the proof state say what the checks found. A
  phase a reader wants, such as waiting for the operator, is derived from the row and never stored.
  This keeps the guard against a stage machine honest.

**A step with no named predecessor waits for the previous number.**
- Status: settled.
- Why. Decision 9 is worth nothing when every step defaults to waiting for nothing. A numbered path is
  a chain unless the path document says otherwise.
- The way past a step nobody will finish is to rewrite the path so the later step no longer waits for
  it. There is no override flag.

**The dependency graph orders by meaning and not by file. This is a known limit.**
- Status: settled as an observation, and nothing is designed on it.
- Measured on `.greenlight/GRAPH.json`: fourteen pairs of slices share at least one file with no
  ordering between them. S-6 and S-8 share eight files. S-11 and S-12 share seven. S-16 and S-17 share
  three, including the file each is mostly about. `internal/manual/manual.go` appears in eight of the
  fourteen pairs, because it holds the usage text that every new command writes.
- Decision 14 acts on this. A take is refused when two steps name the same file in `touches`.
- The check reads what a step says it writes, never the diff. Section 5.1 states that limit.

## Settled on 2026-09-04, after the operator read the second version

The operator read the second version. The proof that it works comes from the operator at first, and
over time it balances to krewe. The reason is trust.

Three decisions followed. Each one is accepted. Do not reopen any of them.

**Decision 10. Krewe runs the check and the operator decides.**
- Status: accepted.
- Krewe prints the exit status, the scenario count and the output. The step closes on the operator's
  word.
- Krewe records whether the operator agreed with its verdict. Agreement is read from the row: done
  after a pass, or stopped after a fail. Anything else is a disagreement.
- Rejected in the same question: krewe running nothing and the operator testing by hand. That teaches
  krewe nothing.

**Decision 11. A run of agreements earns the next level, and krewe offers it.**
- Status: accepted.
- After the threshold, krewe offers to close a passing step itself. The operator accepts the offer
  with a command.
- Rejected in the same question: krewe raising its own level with no approval.
- The threshold is five consecutive agreements. That number is provisional and nothing measured it, because krewe
  never closed a step. The number that replaces it comes from the first project to
  reach ten closes.

**Decision 12. A wrong close lowers the level by one and restarts the run.**
- Status: accepted.
- The operator reopens a step krewe closed. That records a disagreement and drops the level.
- Rejected in the same question: falling to level 0 from any height, which no measurement supports.
- Rejected in the same question: leaving the level where it is, which lets a checker the operator does
  not believe keep the word.

**Two levels, and no third until one is measured.**
- Status: settled as a consequence.
- Level 0: krewe checks, the operator says done. Level 1: krewe closes a step its own check passed.
- A failing check closes nothing at any level.
- A level that skips the restatement is deferred. Nobody measured level 1 yet.

## Settled on 2026-09-04, on the fan out

The operator said a session builds one thing, and that slices which can run in parallel may start
several sessions.

**Decision 13. The operator takes each step, and many may be taken at once.**
- Status: accepted.
- Each take starts its own session. Each session builds one step. A step that finishes starts
  nothing.
- Rejected in the same question: one command that takes every ready step and dispatches for the
  operator.
- Rejected in the same question: krewe starting the next step when one finishes. That is the
  controller the design refuses.

**Decision 14. A take is refused when two steps write the same file.**
- Status: accepted.
- Krewe compares the `touches` field line by line against every step in state taken. The refusal
  names the file and the step that holds it.
- Rejected in the same question: a warning that starts the session anyway.
- Rejected in the same question: silence, which leaves the conflict to merge time.
- The limit: the check reads what a step says it writes, not what a session wrote. A file the
  `touches` field does not name goes through.

**Decision 15. A project setting caps the steps in flight, and the default is three.**
- Status: accepted.
- Provisional. Nobody measured how many restatements the operator reads at once.
- Rejected in the same question: no cap. A path with a wide wave would then put every ready step in
  flight at once.
- Rejected in the same question: a cap the trust level sets. The ladder governs the word done, not
  the fan out.

**The graph is measured, so the fan out has a number.**
- Status: settled as an observation, measured on the graph of 31 slices.
- The graph is 25 waves deep. Two waves hold three slices: S-21, S-22 and S-23 after S-20, and
  S-33, S-34 and S-35 after S-32. Five waves hold two.
- So a cap of three never refuses a take on this project's own path. The cap is a limit on what the
  operator reads at once, and this path does not reach it.
- An earlier reading of the graph of 17 slices said nine waves and a widest wave of five. That
  reading is replaced by this one.
- The file collision is the check that does bite. Of 154 independent pairs, 66 share at least one
  file.

## Settled on 2026-09-04, on the operator's own commands

The operator asked for the shape greenlight has: a slash command in the terminal, such as
`/krewe:init`. Greenlight is markdown, one file per command in a command directory, plus its agent
definitions. Three decisions followed. Each one is accepted.

**Decision 16. Krewe writes the files, so they ship with the product.**
- Status: accepted.
- The files live in `commands/` in this repository, and the binary carries them with `go:embed`. So
  the files and the build never disagree.
- Each file carries a marker naming the build that wrote it. An install replaces a file with the
  marker, and refuses a file without one. There is no flag that forces the write.
- Rejected in the same question: files held by hand in the hub, which the product never carries.
- Rejected in the same question: a copy in both places, because two copies drift.

**Decision 17. A command asks its questions in the operator's session, then dispatches to build.**
- Status: accepted.
- A slash command asks, runs the command line tool, and reads back. It never designs the product.
- Why. The design work belongs to a krewe session in a sandbox. A command that thinks about the
  product makes the terminal a second place the work happens, and nothing records what it did there.
- Rejected in the same question: a command that only dispatches and asks nothing.
- Rejected in the same question: a command that does all the work in the operator's session.

**Decision 18. Four commands ship: init, design, status and trust.**
- Status: accepted.
- The path and the step get none. Taking a step and approving a restatement are one line each on the
  command line tool. A slash command there adds a layer and no answer.
