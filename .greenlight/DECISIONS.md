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
