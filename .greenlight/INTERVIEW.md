# Project Interview

Recorded 2026-09-04. The answers come from Julian in the session that opened this worktree.

## Value Proposition

Krewe carries the ideation and the design stages itself. A project is designed before anybody
builds it. A session then starts already holding the design, the context, the rules of working
and the one change it is taking.

## Users

Julian is the operator. He opens a project, runs the design stage and dispatches the work.

The agent sessions are the second user, and they are the reason the capability exists. A session
today starts with a line of text. It must instead start with enough context to build something
meaningful without being told anything more.

A third reader is a person who was not in the design conversation. Each step of the path must be
buildable by that person.

## MVP Scope

Draft, in priority order. The design session confirms or replaces it.

1. An operator gives a project a brief, and krewe runs an ideation stage that ends in a written
   design the operator approved.
2. The approved design becomes a numbered path of atomised changes on the project. Each step is
   one intention and one reviewable change.
3. An operator dispatches a session against one step of the path. The session starts holding the
   design, the project context and that step, and nothing else needs to be typed.
4. An operator reads the state of the path. It says what is done, what is running and what is next.
5. A session finishes a step, and the path records the result, so the next session starts from
   what is true.

## Stack

Go 1.25. Postgres for the store, with numbered migrations under internal/store/migrations.
Protobuf with buf for the control plane interface, generated into gen/. A Bubble Tea terminal
console. Godog scenarios under features/. Docker and tmux run the sandboxes.

## Constraints

Tests never run on this machine. Running go test in this repository exhausts memory and the
process is killed, which ends the session. Continuous integration is the test runner. The safe
local gates are go build, go vet, gofmt and golangci-lint.

Every change ships with its scenarios. The promises gate refuses a behaviour change that carries
no scenario.

Anything removed from the command line or from the console must refuse by name and say what to
type instead. The existing mechanisms are Moved, Gone, movedViews, removedCommands and
removedFlags.

The design stage is the deliverable. No code exists before Julian approves the path.

## Deferred Ideas

None recorded yet. The design session records what it pushes past the MVP.

## Open Questions For The Design Session

1. Where the design and the path live. Files in the repository the project points at, records in
   the krewe store, or both.
2. Whether the ideation stage is a conversation krewe runs through a session, or a document the
   operator writes and krewe reads.
3. Whether a step of the path is a new resource, or an exec with more on it than a line of text.
4. What a session is given at birth once this exists, field by field.
