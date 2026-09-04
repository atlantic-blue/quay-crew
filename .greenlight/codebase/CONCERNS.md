# CONCERNS

Survey of atlantic-blue/quay-krewe (Go 1.25), for the design session on a project carrying its
own context. Findings are ordered by relevance to that design, then by priority.

## Summary

The removal of jobs, flows and roles (`0d30392`, `#693`; `d4cd9d7`, `#694`) was clean at the
database and proto layer. Migration `0060` drops every orphaned table with a comment stating why.
Proto fields are `reserved`, not deleted, with a one line reason. No orphan tables, no orphan
protobuf messages, and no dead command line verbs were found.

One piece of dead code survived the removal inside the context rendering path itself, and it
matters for this design because it looks like a still working mechanism for handing a session a
brief. It is not. See finding 1.

## Finding 1: the role brief render path is dead code — HIGH

File: `internal/controlplane/server.go`, function `renderContext`, lines 585 to 599.

```go
brief := ""
for at, levels := range contextFiles(session) {
    ...
    if at == outerFile && brief != "" {
        sections = append(sections, sandbox.Section{Scope: sandbox.RoleScope, Body: brief})
    }
    ...
    told := levels
    if false {
        told = nil
    }
    for _, level := range told {
```

`brief` is declared and never assigned. The condition on line 591 is always false, so the
`RoleScope` section never renders. The `if false { told = nil }` on lines 597 to 598 is a stub
left from the role removal: it once read a session's role to decide whether to withhold context
from it, and the removal turned the condition into a constant.

`sandbox.RoleScope` (`internal/sandbox/memory.go:36`) and its read back handling
(`internal/controlplane/server.go:527`) still exist and still run, decomposing a memory file
that nothing writes a `RoleScope` section into any more.

`go vet` and `golangci-lint run` (standard linter set, `.golangci.yml`) both report clean on
this file. Neither catches an always false `if` branch or an assigned but never reused string.

This is the finding to read first for the design session: it proves that a role level brief is
not an existing mechanism to extend. It was removed, and what is left is inert.

## Finding 2: a session starts with layered memory files and nothing else — the actual context picture

This is not a defect. It is what a dispatch carries today, evidenced for the design session.

`Dispatch` (`internal/controlplane/server.go:1127`) accepts `project`, `handle`, `text`,
`permission_mode`, `title` and `detach` (`proto/quaycrew/v1/controlplane.proto:341`). `text` is
sent to the model exactly as the operator typed it (`internal/controlplane/server.go:1263`,
field `Text: text`). No dispatch carries a brief, a plan, or any structured instruction.

Everything else a session starts with comes from two composed memory files, both named
`CLAUDE.md`, read natively by the model (`internal/manual/manual.go:210` to 215):

- The outer file, in the workspace's conversation store: the system level context, the workspace
  level context, and a rendered index of the skills the session holds.
- The inner file, in the session's own working directory: the project level context and the
  session level context (its own notes).

Source: `contextFiles` (`internal/controlplane/server.go:704` to 711), `renderContext`
(`internal/controlplane/server.go:580` to 625), `internal/sandbox/memory.go`.

Project level context (`store.ContextProject`, `internal/store/store.go:329`) is not populated
automatically. `CreateProject` (`internal/controlplane/server.go:1014` to 1026) writes a name and
a workspace and nothing else. The only way a project's context is set is the operator running
`krewe context set project ...` (`cmd/krewe/quay.go:1249`, `runContextEdit`) by hand, after the
project exists. A project carries no context of its own until somebody writes it.

So an agent's starting context is: whatever the operator wrote at the four levels, layered, plus
whatever the repository itself contains once checked out, plus the raw sentence sent with this
exec. There is no per dispatch brief, no template, and no default project context.

## Finding 3: silent best effort writes on the context sync path — MEDIUM

File: `internal/controlplane/server.go`, `syncContextExcept`, lines 541 to 555, and
`renderContext`, lines 601 to 611.

`s.store.GetContext` and `s.store.SetContext` errors are discarded with `_ = ...` or a bare
`continue` on error, with no `slog` call. The comment above `syncContext`
(`internal/controlplane/server.go:497`) states the policy on purpose: "A failure here never
fails an exec." That is a reasonable choice for availability. But a `SetContext` failure during
sync is invisible: nothing is logged, so a memory file that silently stops agreeing with the
store cannot be told apart from one that is agreeing correctly. Add a `slog.WarnContext` at each
discard point so a sync failure is at least observable, without changing the "never fail the
exec" behaviour.

## Finding 4: `if false` and similar always constant branches are not caught by CI — LOW

`.golangci.yml` enables only the standard linter set plus `gofmt`. Standard does not include
`staticcheck`, which would flag `S1008`/`SA4006` style issues (an always false condition, an
assigned and unused value). Finding 1 is the concrete case. Worth enabling `staticcheck` in the
standard set before more removal work happens, since removal is exactly the kind of change that
leaves a stub condition behind.

## Clean areas checked

- No hardcoded secrets: grepped for API key and password literal patterns across `.go`, `.yml`,
  `.yaml`, `.env*`; none found. `internal/secrets/` reads credentials through its own store, not
  literals.
- No SQL string concatenation: every query in `internal/store/` uses parameterised statements;
  grep for `fmt.Sprintf` building `select`/`insert`/`update`/`delete` returned nothing.
- No orphan tables: migration `0060_remove_jobs_flows_and_roles.up.sql` drops every table the
  removed subsystems owned, `cascade`, with the reasoning written in the migration itself.
  Migration `0061_an_exec_is_called_an_exec.up.sql` renames `tasks` to `execs` idempotently.
  `go build ./...` is clean, so nothing still queries a dropped table.
- No orphan protobuf messages: every `message` in `proto/quaycrew/v1/controlplane.proto` traces
  to a live RPC or a live nested type. Removed fields are `reserved` with a comment naming what
  used to be there (`controlplane.proto:112`, `:360`, `:369`), not deleted silently.
  `RoleScope`/`SkillsScope` are the one exception at the Go layer, see finding 1.
- No orphan feature files: `features/*.feature` mentions of "job" or "flow" are the ordinary
  English words in prose (for example `dispatching.feature:4`), not references to the removed
  command verbs. No feature exercises a removed `krewe job`/`flow`/`role` command.
- No stale docs: the `docs/` directory was removed in the same commit as the subsystems it
  described (`0d30392`). `README.md` states "Eight resources" and lists exactly the eight that
  exist today (workspace, project, session, exec, skill, hook, secret, context).
  `changelog.d/` and `cmd/changelog/` were removed together in `3469029`; no leftover reference
  to either in `Makefile` or `.github/workflows/ci.yml`.
- `gofmt -l .` and `go vet ./...` are both clean. `golangci-lint run ./...` reports 0 issues.
- No TODO/FIXME/HACK markers in non test Go source.
