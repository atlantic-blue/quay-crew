# Conventions

Quay Krewe is a Go 1.25 module, `github.com/atlantic-blue/quay-krewe`. Evidence below is file
paths and greps, read on 2026-09-04.

## Naming

- **Package names are single lowercase words**: `console`, `promise`, `session`, `store`,
  `sandbox`, `skill`, `workspace`. No underscores, no `pkg` prefix.
- **Test functions read as full sentences describing the behaviour**, not `TestFoo`:
  `TestASessionOnAFreshSystemIsGivenTheDeployIdentityRule`,
  `TestNoRefusalIsMadeOfTwoSentencesReadAsOne`,
  `TestALiveSessionAndAnArchivedOneShareOneOrder` (`internal/store/*_test.go`,
  `hooks/prose-gate/corpus_test.go`). 173 test files, 1123 `func Test...` functions.
- **Command and flag removal is a named table, not a code branch.** `cmd/krewe/quay.go` holds
  `removedFlags map[string]string` and `removedCommands map[string]string`, each value is the
  full sentence to print, with what to type instead. `internal/console/console.go` holds the
  same idea for a console view under `movedViews map[string]string`. A key that is refused
  reads "X is gone: Y" or "there is no X command: Y", never a silent fallthrough. Comments at
  each table state the design rule: a removed word must error loudly, never be swallowed into
  the next argument or reused for something else.
- **Doc comments narrate the reason for a type or function, not just its shape.** Nearly every
  exported symbol in the codebase carries a multi paragraph comment explaining why the shape is
  what it is, often citing a bug it fixes (see `internal/promise/promise.go`,
  `internal/console/console.go`, `cmd/krewe/quay.go`). This is heavier than typical Go doc
  comments; it follows the house rule that comments explain a constraint the code cannot show.

## Error handling

- **Wrapped with `fmt.Errorf("<context>: %w", err)`** throughout, short lowercase context
  prefixes: `internal/store/postgres.go` has `"parse database url: %w"`, `"connect: %w"`,
  `"ping: %w"`, `"create workspace: %w"`. Consistent across the store, sandbox and console
  packages.
- **No empty catch equivalents found** in production code. Bash based checks in `.github/workflows/ci.yml`
  use `set -euo pipefail` throughout; the one deliberate swallow found is documented, for
  example `internal/console/console.go`'s `GetUsage` call: "A system that cannot answer still
  has a header worth drawing, so this failure is swallowed where the one above is not."
- **Refusals from CLI and hooks are user facing sentences**, not generic Go errors: see the
  `removedCommands`/`removedFlags` tables above and `hooks/prose-gate/rules.go`'s `Finding.String()`,
  which always names the file, line, rule, what is wrong, and what to do.

## Logging

- `log/slog` is used across 21 files (`grep -rl "log/slog" internal cmd`). No raw
  `fmt.Println` or `log.Print` calls were found in `internal/` or `cmd/` production code; all
  console output goes through explicit `io.Writer` parameters (`out io.Writer`) or `slog`.
  `fmt.Fprint*` is explicitly excluded from `errcheck` in `.golangci.yml` since these calls
  rarely fail and checking them is noise.

## Formatting and linting

- `gofmt -l .` (excluding `hooks/`, each hook is its own module) returns clean.
- `.golangci.yml`: `version: "2"`, `linters.default: standard`, one custom setting
  (`errcheck.exclude-functions` for `fmt.Fprint`/`Fprintf`/`Fprintln`), `formatters.enable: [gofmt]`.
- Each directory under `hooks/` (`deploy-identity-gate`, `merge-gate`, `process-gate`,
  `prompt-analyser`, `prose-gate`, `test-gate`) is its own Go module with its own `go.mod`, so
  `go vet ./...` and `golangci-lint run ./...` at the root do not reach them; CI and the
  Makefile loop over `find hooks -maxdepth 2 -name go.mod` to vet, test and lint each one.

## Imports

Standard library first, then third party, then this module's own packages
(`github.com/atlantic-blue/quay-krewe/...`), each group blank line separated, gofmt/goimports
ordered. Seen consistently in `cmd/krewe/quay.go`, `features/suite_test.go`.

## Comment style and house rules the code itself states

- `README.md`: "Anything not in `features/` does not exist." The product's behaviour contract
  is the feature files, not the code.
- `internal/promise/promise.go` and `cmd/promises/main.go` describe a repository level promise:
  any change that touches behaviour (a `.go` file outside `gen/` and not a `_test.go`, or a
  `.proto` file) must carry a new or extended `.feature` scenario, or the pull request body must
  state why not, with at least 3 words after "No scenario:". Enforced by `make promises`
  (see TESTING.md for the mechanics).
- `hooks/prose-gate/README.md` states the prose standard house rule directly: this repository
  writes prose to ASD-STE100 (Simplified Technical English), and the hook enforces only the
  four rules a program can check without judgement (sentence length over 25 words, paragraph
  over 6 sentences, present/past perfect tense, continuous tense, and a dash used as
  punctuation). It explicitly does not attempt vocabulary or noun cluster rules, calling those
  "a guess at the standard."
- Deliberate absence of abbreviation and terse naming: identifiers read as sentences
  (`movedViews`, `removedCommands`, `TestASessionOnAFreshSystemIsGivenTheDeployIdentityRule`),
  matching the project's global writing rules (no acronyms, plain words).

## Anti patterns / notable design choices

- No `any`/`interface{}` heavy code observed in the sampled files; `internal/model` uses a
  `runner.go` interface (`ClaudeCode`, `Echo`, `Fake` implementations) for dependency
  substitution in tests, consistent with the double based test design described in
  `features/suite_test.go`.
- Deliberately no cross cutting alias or fallback for removed CLI surface: every removed
  command/flag is refused by name (see Naming section) rather than silently accepted, which the
  code comments call out as a considered anti pattern avoidance ("ignoring a flag is worse than
  not having it").
