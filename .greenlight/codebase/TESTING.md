# Testing

Read only survey. Test binaries were never run (`go test` is banned in this repository session,
per instructions); all figures below come from reading the files and the CI workflow.

## Test tiers

**1. Go unit tests**, alongside the code they test, `<name>_test.go`, package internal to the
code under test. 173 test files under `internal/`, `cmd/` and `hooks/`, holding 1123
`func Test...` functions total (`grep -rh "^func Test"`). Test names are full sentences, for
example `internal/store/postgres_test.go`:`TestALongContextKeepsItsSizeThroughPostgres`.
Several packages (`internal/store`, `internal/session`) run the same table of behaviour, a
"conformance suite," against both the in memory store and Postgres, for example
`TestMemoryConformance` next to `TestALongContextKeepsItsSizeThroughPostgres`
(`internal/store/storetest/storetest.go` holds the shared assertions both stores run against;
`storetest.go:513` names one such assertion).

**2. Godog scenarios under `features/`**, the executable specification. 45 `.feature` files,
458 `Scenario:` lines total (`grep -rh "Scenario:" features --include='*.feature' | wc -l`).
Organisation is flat, one feature per capability, each `<name>.feature` paired with a
`<name>_steps_test.go` holding its step definitions (for example `addresses.feature` /
`address_steps_test.go`, `dispatching.feature` / `dispatching_steps_test.go`). A single
`features/suite_test.go` wires godog to the control plane over its real gRPC interface, with the
model runner and the sandbox provider substituted for doubles, so these scenarios need no Docker
daemon and no Claude subscription. `features/catalog.go` and `catalog_test.go` give
`krewe features` (and `internal/manual`) a way to read the specification back at runtime; the
package comment in `suite_test.go` states scope: "A behaviour that is better said as a Go table
test belongs in the package it tests, not here," and that these scenarios deliberately do not
prove a real exec executes, only routing, session identity, sandbox lifecycle and error
handling.

**3. Integration tests**, files tagged `//go:build integration`, 27 files found across the
repository (`grep -rl "go:build integration"`). These use `testcontainers-go` to run real
Postgres and Redpanda in Docker, proving things the unit tier's doubles cannot, for example a
real commit inside the sandbox or the control plane finding a broker. `go.mod` lists
`testcontainers-go`, `testcontainers-go/modules/postgres` and
`testcontainers-go/modules/redpanda` for this tier.

**4. A gated live test in `internal/model`**, `claudecode_integration_test.go`, that needs a real
Claude subscription rather than a container; `suite_test.go`'s comment names it as the tier that
proves an exec really runs against the live model.

**5. Container and end to end smoke in CI only** (not a Go test): the `containers` job in
`.github/workflows/ci.yml` composes the full stack (control plane, gateway, Postgres, Redpanda,
Grafana, Tempo, Loki, Prometheus, the otel collector) with `docker compose`, dispatches a real
exec through the CLI against it, restarts the control plane to prove session durability, destroys
a sandbox container to prove state survives it, and checks that OpenTelemetry spans actually
landed in the collector. This is not part of `go test ./...`; it exists only as CI shell steps.

**6. Repository level gates that are not test frameworks but do fail a build**: `make promises`
and `hooks/prose-gate` (see below), and `secrets` (`gitleaks`) in CI.

## The `make promises` gate

`Makefile:371` runs `go run ./cmd/promises -base origin/$(UPGRADE_BRANCH)`; CI runs it as the
`promises` job in `.github/workflows/ci.yml`, only on `pull_request` events (there is no PR body
to read anywhere else). The logic lives in `internal/promise` (`promise.go`, `changed.go`) and
the CLI entry point in `cmd/promises/main.go`.

**What it refuses.** It reads the diff between `base` (default `origin/main`) and `head`
(default `HEAD`). If any changed file is "behaviour", meaning a `.go` file that is not under
`gen/` and is not a `_test.go` file, or a `.proto` file (`promise.go`'s `isBehaviour`), the
change is required to carry a new or extended `.feature` file under `features/` (`isScenario`
requires the file to still exist after the change, i.e. deleting the last scenario does not
count as carrying one). If a behaviour change carries no such file, `Check` returns a `Finding`
and the command exits 1, printing the refusal, which names the files that made this a behaviour
change, what a scenario is wanted, and how to state an excuse instead.

**How somebody gets past it.** Either add a `.feature` scenario in the same change, or write a
line in the pull request body starting with `No scenario:` (case insensitive, markdown bullet or
bold markers stripped, fenced code blocks ignored so the rule's own documentation does not excuse
itself) followed by at least 3 words of reason (`reasonWords = 3` in `promise.go`). A change that
touches no behaviour (only tests, `gen/`, docs, CI config) is asked for nothing. An empty diff
(wrong base ref) is treated as an error, not a free pass: `cmd/promises/main.go` explicitly
refuses to say a check with 0 files read is a pass, because that is indistinguishable from
proving nothing.

## The prose gate, `hooks/prose-gate`

A Claude Code hook (its own Go module, `hooks/prose-gate/go.mod`), built to `bin/hook` via
`make hooks`, not committed as a binary. Fires on `PreToolUse` for two matchers: `Write`, `Edit`
and `MultiEdit` on files ending `.md`, `.markdown`, `.txt`, `.rst`; and `Bash`, where it reads
the prose argument out of specific flags (`--body`/`-b` on `gh pr`/`gh issue`/`gh release`,
`-m`/`--message` on the commit and tag subcommands of `git`, `--body-file`/`-F` on the same `gh`
commands, where it opens the file). It parses the shell command far enough to find the program
and its flags; it does not expand variables, globs, heredocs or command substitutions, and it
explicitly does not read `gh api`'s `-F` (a field, not a file).

**What it checks**, exactly and without judgement, against ASD-STE100 (Simplified Technical
English): a sentence over 25 words (`MaxWords`, `hooks/prose-gate/rules.go`), a paragraph over 6
sentences (`MaxSentences`), the present or past perfect tense (`has`/`have`/`had` plus a
participle), a continuous tense (a form of `be` plus an `-ing` word), and a dash used as
punctuation (em dash, en dash, or a hyphen with a space on either side; a hyphen inside a word,
such as `kebab-case`, is left alone). It explicitly does not attempt approved vocabulary or noun
cluster length, because those need a published word list or a parser it does not have.

**What corpus it is measured against**: `hooks/prose-gate/corpus_test.go` runs the gate live
against `README.md` and `hooks/prose-gate/README.md` (the corpus shrank from an earlier set that
included `docs/`, `CHANGELOG.md` and `roles/`, now removed) on every test run, and asserts a band
around the refusal rate rather than a fixed number, so an edit to those two files does not
silently flip the test red. A companion test,
`TestNoRefusalIsMadeOfTwoSentencesReadAsOne`, asserts none of the length refusals are actually
two real sentences misread as one (a joined sentence would still carry an inner full stop, which
this test greps for). The recorded reading from 31 August 2026 against commit `aafb6e8`: 83
documents, 4143 paragraphs, 1492 refused (36 per cent); length 1736, paragraph 78, tense 552,
dash 96.

**How it refuses**: exit code 2 with the reason on standard error, one `Finding` per problem,
each naming the file, line, sentence, what is wrong and what to do, capped so a document with
many refusals is not dumped whole. Anything the gate cannot read (an unopenable file, an
unparseable payload) exits 0, since a broken gate must not stop the system. It is attached per
workspace (`krewe hook attach <workspace> prose-gate`), not seeded by default, unlike
`merge-gate`.

## How to run tests

Documented from `Makefile` and `.github/workflows/ci.yml`; not executed in this session.

- `go test -count=1 ./...` — unit tests across the module (excludes `hooks/`, each a separate
  module).
- `go test ./features/... -v -count=1` — the godog scenarios, also runnable as `make features`
  (which depends on `make hooks` first, since the hook loader needs the built entry points).
- `go test -C hooks/<name> -count=1 ./...`, looped per hook directory — each hook module's own
  tests, since they are outside the root module.
- `go test -tags=integration -count=1 -v ./...` — the integration tier, run in CI with
  `QC_TEST_SANDBOX_IMAGE=krewe-sandbox-claude:local` set and the sandbox image already built
  (`make sandbox-image`), needing a Docker daemon.
- `make test` — root unit tests plus every hook module's tests, in one target.
- `make promises` — the promise gate against `origin/$(UPGRADE_BRANCH)` (default `main`).
- `make lint` — `buf lint`, then `golangci-lint run ./internal/... ./cmd/... ./features/...`,
  then the same per hook module.
- `make fmt` — `gofmt -w` over every `.go` file except `gen/`.
- CI's `containers` job composes the full stack with `docker-compose.yml` and drives it through
  the real `krewe` CLI end to end; there is no single `make` target for this in the files read.

## Test database and environment

- Unit and godog tiers use an in memory store and doubled model/sandbox providers; no database or
  Docker needed (`features/suite_test.go`'s package comment).
- Integration tier: `testcontainers-go` starts real Postgres and Redpanda containers per test
  run; CI sets `QC_TEST_SANDBOX_IMAGE` for sandbox backed tests.
- Container and CI smoke tier: `docker compose -f deploy/docker-compose.yml`, project name `qcci`
  in CI (`COMPOSE_PROJECT_NAME`), with a dedicated `qcci_sessions` network so sandbox containers
  can reach the control plane. The system mints its own auth token into
  `~/.krewe/data/system.token` on first start, which CI reads with `sudo` to authenticate later
  CLI calls.

## Coverage and untested areas

No coverage command or threshold was found in `Makefile`, `.golangci.yml` or the CI workflow;
`go test -cover` is not invoked anywhere read. Test volume is heavy relative to the codebase
(1123 unit test functions, 458 godog scenarios), and the `make promises` gate exists specifically
to keep new behaviour from shipping without a scenario, so a coverage gap is more likely to be
"missing a `.feature` scenario despite the gate" (whenever a change touches no `.go`/`.proto`
file, e.g. shell/YAML/Dockerfile changes) than "missing a unit test." Not independently verified
by running coverage tooling in this session.
