# The rest of the rename

The product is **Quay Krewe**. The command a person types is `krewe`.

The owner decided that name. This document does not reopen it, and it does not screen alternatives.
The known risk is one line. Red Hat holds the mark QUAY, and a compound name that contains the word
can still collide with it. Nobody searched a live trademark register.
[Issue 585](https://github.com/atlantic-blue/quay-crew/issues/585) records that `fabric`, `blueprint`
and `blue quay` were each refused, and `docs/NAME.md` carries the bar and the evidence.

A person runs the register search before the name goes on a package or on a site.

This document holds the rest: eight decisions, the migration for each thing that breaks, and the
order the pull requests land in. Nothing here is built. The plan waits for a person to approve it.

## 1. Where the tree is today

Measured at `ccc9b0a` on `main`, 31 August 2026, over the 982 files git tracks.

Piece 1 of [#517](https://github.com/atlantic-blue/quay-crew/issues/517) landed on 30 August as
`4e3a2ad`. The command is already `krewe`. The module path is already
`github.com/atlantic-blue/krewe`. `cmd/quay` is a binary that refuses every invocation. The other
five pieces did not land.

The whole tree counts mislead, so here is the split.

- `krewe` appears 2,783 times in 610 files.
- `quaycrew` appears 5,107 times. 1,633 of those sit in generated code under `gen/`, which
  `buf generate` rewrites. 5 sit in `CHANGELOG.md`, which is a record. Another 3,211 are the protobuf
  package identifier, which stays for the reason in section 4.
- That leaves **257 occurrences of `quaycrew` in 59 files a person wrote**. They sit in `docs/` (94),
  `internal/` (82), `deploy/` (29), `features/` (17), `cmd/` (15), `.github/` (12), the Makefile (6)
  and `changelog.d/` (2).
- `quay`, not counting `quaycrew` and not counting `CHANGELOG.md`, appears **968 times in 155 files**.
  352 of those are the repository address `quay-crew`. 75 are `~/.quay` or `QUAY_HOME`. 6 are the
  memory file mark `<!-- quay:session -->`, which stays.

## 2. The eight decisions

Each one carries a recommendation and its cost.

### 2.1 The repository name, and the Go module path

These are one decision, not two. The Go module proxy needs the module path to equal the repository
address.

**Recommendation: the repository becomes `atlantic-blue/quay-krewe`. The module path becomes
`github.com/atlantic-blue/quay-krewe` in all four `go.mod` files.**

There are four modules, not one. The root is one. `hooks/prompt-analyser`,
`hooks/deploy-identity-gate` and `hooks/merge-gate` are the other three, each its own module by
design. Nothing outside `hooks/` imports them, so the three hook paths move with the root.

The repository is the product's address, and the product is Quay Krewe. `quay-crew` becomes
`quay-krewe`, which is a change of one letter, so a person who remembers the old address still types
the new one.

**What GitHub's redirect covers.** It carries `git clone`, `git fetch`, `git push` and the web
address from the old name to the new one. Every issue link, pull request link and commit link written
today still resolves.

**What it does not cover.** Three things.

- A clone keeps the old remote address. Each worktree needs `git remote set-url`.
- The module path stays broken until this pull request lands. It is broken today.
  `go install github.com/atlantic-blue/krewe/cmd/krewe@latest` answers `Repository not found`,
  because the module path says `krewe` and the repository says `quay-crew`. That is captured output,
  run from this tree on 31 August 2026.
- The redirect dies the day somebody creates a new repository at `atlantic-blue/quay-crew`.

Every open pull request retargets onto the renamed repository, and GitHub does that itself. Rule 4
applies to what comes back. A retarget rewrites a commit, so a person re-verifies the signatures on
the remote before calling any of those pull requests ready.

**Cost.** 1,347 import lines in 441 Go files move. The compiler catches every one, so the change
carries no runtime risk. It conflicts with every open branch, so it lands when the board is as clear
as it gets.

**The runner up, and why not.** `atlantic-blue/krewe` costs nothing at all, because the module path
already says `krewe`. It drops the product's first word from the address, so a person who reads
"Quay Krewe" and goes looking guesses wrong. I recommend against it. It is a one word answer if the
owner prefers the free option.

### 2.2 The data directory

**Recommendation: `~/.quay` becomes `~/.krewe`, and `QUAY_HOME` becomes `KREWE_HOME`.**

This directory holds what nothing can regenerate: the system token, the driver token, the sealing key
that unseals every secret, and every conversation.

The dotted directory matches the command a person types. That is what a reader expects, and it is
what other tools do. The command is `krewe`, so a directory called `~/.quay` names a word nobody
types.

**The migration, and how it proves the move.** `refuseTheOldLayout` in `cmd/krewe/home.go` already
exists. It refuses to start when the new directory is absent and an old one is present, and it prints
the exact `mv`. It needs one more entry in its `retired` table. What it must not do is start.

A system that comes up on a fresh token, with a sealing key it never saw, reads as every conversation
lost. `KREWE_HOME` is the new variable, and the tool reads `QUAY_HOME` for one release.

Ship three proofs in the same pull request. A test that a home holding `~/.quay` refuses to start and
names `~/.krewe`. A test that the guard passes once `~/.krewe` exists. A scenario in `features/` that
a person can read. `deploy/configuration_test.go` allows the directory name in exactly one file, and
that guard moves with it.

**Piece 3 of #517 is wrong on one point, and this is the correction.** It says "do not write a
migration guard. #507 removed the last one". #507 removed the guard in the Makefile. Its own
changelog entry says the tool keeps its own refusal, and that refusal is `refuseTheOldLayout`.

It predates #517. It is what makes this move safe, and it stays.

**Cost.** An operator runs one `mv`. This is the second most dangerous item in the rename, so it gets
a pull request of its own.

**The alternative, and why not.** Keep `~/.quay`, because the product's first word is Quay. It costs
no migration. It leaves the most valuable directory on the machine named after a word the person
never types. The command beside it spells the other half of the name. I recommend against it.

### 2.3 The compose project name

The compose project is `quaycrew`, at `deploy/docker-compose.yml:1` and `Makefile:5`.

**Recommendation: do not rename it.**

This is not in #517, and it is the most dangerous item in the whole rename. Docker compose prefixes
every named volume and every network with the project name. The database volume is
`quaycrew_postgres-data`. Rename the project and `docker compose -p krewe up` creates an empty
`krewe_postgres-data`.

Every job, session, task and secret then sits in a volume nothing mounts. The stack starts. The
database is empty. Nothing says why.

The session network moves the same way. It is `quaycrew_sessions`, derived at `Makefile:20`. A
sandbox container that is already up sits on a network the new stack does not use. It then loses its
route to the control plane.

**If the owner wants it renamed anyway**, the migration is a volume copy an operator runs once. The
stack then refuses to start on an empty database while the old volume exists. That is a pull request
of its own, and it is not in this plan.

**Cost.** `docker ps` keeps showing `quaycrew-postgres-1`. Nobody types it and nothing depends on the
word.

#517 piece 2 groups the compose project with the container prefix. The evidence separates them. The
prefix is safe to move. The project is not.

### 2.4 The container name prefix

The prefix is `quaycrew-`, one constant at `internal/sandbox/sandbox.go:195`.

**Recommendation: it becomes `krewe-`, and the code reads both prefixes for one release.**

An operator reads container names in `docker ps` and in error text, so this name is visible. Four
things derive from the constant.

- `ContainerName` builds the name a sandbox starts under.
- `sandboxName`, the expression at `internal/sandbox/docker.go:165`, is what `Stranded` lists by.
- `sessionOf` at `internal/headroom/daemon.go:230` decides which container belongs to a session.
- `SANDBOX_PATTERN` at `Makefile:42` sweeps what an upgrade left behind.

Change the constant alone and every running container becomes invisible. `krewe drain` does not find
it. `Remove` targets a name that is not there, and it treats absent as success. The Makefile sweep
skips it. The container keeps running and holds its memory. The next start of that session then makes
a second container beside it.

**The migration.** Write the new prefix and read both. `Stranded` and `sessionOf` accept either one.
`Remove` tries the new name, then the old one. `SANDBOX_PATTERN` matches both.

One trap sits underneath it. Decision 2.3 keeps the compose project at `quaycrew`, so the stack's own
services are still `quaycrew-postgres-1` and its friends. The read of the old prefix keeps the exact
shape of 24 hexadecimal characters, or a sweep by prefix takes the whole stack with it. `sandboxName`
has that shape today. Keep it, and give the old prefix the same shape.

**Test the way off, not only the way on.** Ship a test that the system still finds and still removes a
container named `quaycrew-` followed by 24 hexadecimal characters. Rule 46 says that is the half
nobody writes.

**Cost.** Two prefixes in the code, until the read of the old one is deleted. The changelog entry
names the release that deletes it, so it does not become permanent by default.

### 2.5 The local sandbox image tag

The tag is `quaycrew-sandbox-claude:local`.

**Recommendation: it becomes `krewe-sandbox-claude:local`, and `make sandbox-image` tags the image
under both names for one release.**

The trap is not in the repository. An installed operator's own `~/.quay/env` pins
`QC_SANDBOX_IMAGE`, and an upgrade does not rewrite that file. Their stack then names an image the
new build no longer produces. The first task after the upgrade fails on a missing image, and that
failure reads as a broken system rather than as a rename.

**The migration.** `make sandbox-image` builds the new tag, then applies the old tag to the same
image, so both names resolve. `make env-check` gains a check for a key whose value is a retired tag.
Today it reports only a key that is absent. It never reports a value that is stale. I read the target
at `Makefile:139` to confirm that.

The tag sits in eight places: `Makefile:46`, `Makefile:126`, `.github/workflows/ci.yml:113`,
`deploy/env.example:60`, `deploy/testdata/partial.env:2`, `deploy/pinned_integration_test.go:40`,
`internal/model/claudecode_integration_test.go` in three spots, and
`internal/sandbox/sandbox_test.go:116`.

**Cost.** One extra tag, and about five lines in `env-check`, carried for one release.

### 2.6 The wordmark

The mark is three rows of block letters spelling QUAY. It is `logo` at
`internal/console/view.go:148`, drawn in `ansiGreen` by `mark` at `internal/console/style.go:53`.

**Recommendation: draw KREWE, in the same idiom, at three rows.**

**Measured, not assumed.**

```
four letters: 36 columns wide, drawn from 73 console columns up
five letters: 44 columns wide, drawn from 81 console columns up
```

That is captured output from a throwaway test in package `console`, run against this tree. The test
is not in this change. To reproduce it, add a test in `internal/console`. It assigns to the package
variable `logo`, sets `model.width`, and calls `headerLines()` at each width from 40 upward. The five
letter mark in that run is a stand in of the correct width. It is not the finished letters, which the
pull request that ships them draws by hand.

`TestTheWordmarkSurvivesAConversationBesideIt` asserts the mark at 84 columns. That is half of a 168
column window, which is what a conversation beside the console leaves. 81 is under 84, so a five
letter mark survives exactly that case.

The four wordmark tests were run against a five letter stand in, and all four passed, so the width
test does not need loosening. A mark of 100 columns then turned two of them red, which is what proves
they read the width at all. The comment on that test says 35 columns and roughly 80. Both numbers
move, to 44 and to 81.

**Cost.** The mark disappears between 73 and 80 columns, where it is drawn today. That is a narrow
terminal, and the header still carries the build and the way to help there.

**The runner up, and why not.** Keep QUAY, because it is the product's first word. It costs no columns
and no test changes. It spells the half of the name a person never types, beside a command that
spells the other half. I recommend against it.

### 2.7 The words in the documents

**Recommendation: rename the words last, and never rewrite `CHANGELOG.md`.**

The words are the README, `docs/`, `features/`, `flows/`, `roles/` and `skills/`. `features/` holds
scenario prose that reaches the manual the tool prints. A word retired in one feature file can fail a
scenario in another, so run the whole suite rather than the file that changed.

`CHANGELOG.md` is the record of what happened. In August the tool was called `quay`, and a changelog
that says `krewe` makes the past false. Unshipped entries under `changelog.d/` are different. They
describe a release that did not happen yet, so they move.

**Cost.** A reader of the changelog meets three names. The entry for this rename says which is which,
and says when each one was true.

### 2.8 `cmd/quay`

**Recommendation: it stays exactly as it is, and this plan writes no second refusal.**

It already does what #517 asked. It refuses every invocation, whatever the arguments are, and it
prints one sentence naming `krewe`. Its test reads the command list out of the manual the tool
prints, so it covers the whole old surface rather than three remembered cases. The test fails when
the manual lists nothing, rather than passing on an empty list.

The command does not move in this plan, so the sentence it prints stays true. That is the one thing
piece 1 bought that the rest of the rename does not spend again.

**Cost.** Two binaries on the path, one of them a refusal. The changelog names the release that
deletes it.

## 3. What must not break

An operator has a build installed right now. Five things reach them, and each carries its migration.

- **The command.** It does not move. `krewe` stays `krewe`, and `quay` keeps refusing.
- **The data directory.** Decision 2.2. The guard refuses to start and prints the `mv`. It never
  starts empty.
- **The database and every row in it.** Decision 2.3. The compose project does not move, so the
  volume does not move.
- **The running containers.** Decision 2.4. Read both prefixes, write the new one, and test that the
  system still finds the old one.
- **The sandbox image.** Decision 2.5. Tag the image under both names, and make `env-check` name a
  stale value.

## 4. What does not rename at all

Each one is a name on a wire, in a store, or inside a container that is already up. Nobody types any
of them. To rename one strands something.

- **The protobuf package `quaycrew.v1`.** It is the method path,
  `/quaycrew.v1.ControlPlaneService/...`. A new build could not talk to a session an old build
  started. It is also most of the occurrences, and to leave it is what makes the rest fit in small
  pull requests.
- **The Postgres database and user, both `quaycrew`.** To rename them is a data migration on every
  installed stack, for a string no reader sees.
- **The metric names `quaycrew.tasks`, `quaycrew.tokens` and `quaycrew.cost.usd`**, and the span
  attributes beside them at `internal/job/controller.go:1160`. Rename a metric and the history under
  it breaks. A dashboard that loses its past is worse than one that carries an old word.
- **The image label `com.quaycrew.build`**, read off images that already exist.
- **`AttachedSessionName`**, the tmux session named `krewe` inside a running sandbox. An operator's
  open conversation lives under that name.
- **`QC_TOKEN`, `QC_GRPC_ADDR`, `QC_SESSION_ID` and `QC_TRACEPARENT`**, read by sessions that are
  already up. The memory file mark `<!-- quay:session -->` goes with them, because a session that is
  up wrote its file under that mark.

The changelog states every one of these. A reader who searches for `quaycrew` after the rename then
finds a reason rather than a miss.

## 5. The order the pull requests land in

Each one is revertable on its own. Each ships its tests in the same change. Each tests the way off the
old name beside the way on to the new one.

1. **This plan.** `docs/RENAME.md` and `docs/NAME.md`. No code.
2. **The runtime names.** The container prefix and the sandbox image tag, both reading old and
   writing new. The compose project is not touched.
3. **The data directory.** `~/.krewe` and `KREWE_HOME`, behind the guard that refuses to start empty.
4. **The console wordmark.** KREWE at three rows, and the two numbers in the width test.
5. **The words.** README, `docs/`, `features/`, `flows/`, `roles/` and `skills/`.
6. **The repository and the module path.** The owner renames `atlantic-blue/quay-crew` to
   `atlantic-blue/quay-krewe`, and this pull request moves the module path to match.

Pull requests 2 and 3 are the ones that reach a running system, and they are the smallest, so a
revert is cheap. 4 depends on nothing before it. 5 goes after 2, 3 and 4, so the examples describe
something that already works. 6 conflicts with every open branch, so it goes last, when the board is
as clear as it gets.

The owner renames the repository first, then pull request 6 lands. Between the two, the module path
and the repository disagree. They disagree today, so that window costs nothing new.

The seven pull requests that the `pull-request-land` flow lands go before all of this.

## 6. This closes issue 517

**Recommendation: #517 is completed, not superseded.**

Its headline is "it becomes krewe", and that is true. The tool is `krewe` and it stays `krewe`.

Its six pieces map onto this plan. Piece 1 landed. Piece 2 is pull request 2. Piece 3 is pull request
3. Piece 4 is pull request 5. Piece 5 is pull request 4. Piece 6 is pull request 6.

Three things in it changed, and this document carries each one with its evidence. Piece 2 groups the
compose project with the container prefix, and decision 2.3 separates them. Piece 3 says the data
directory moves with no guard, and decision 2.2 keeps the guard that #507 deliberately left in place.
Piece 6 names the repository `atlantic-blue/krewe`, and decision 2.1 recommends
`atlantic-blue/quay-krewe`, because the product gained its first word back.

To close the issue is the owner's step. Pull request 6 is the one that closes it.

## 7. This replaces the blue quay plan

An earlier version of this document landed on `main` as
[#592](https://github.com/atlantic-blue/quay-crew/pull/592) on 31 August 2026. It planned a rename to
**blue quay**, which the owner then refused. This document replaces it, and `docs/NAME.md` records
the refusal.

[#591](https://github.com/atlantic-blue/quay-crew/pull/591) is the same document on a second branch
and it is still open. It carries the refused name, so it wants closing rather than landing. That is
the owner's step.

Four things in the blue quay plan survive the change of name, and they are here. They are the split
of the `quaycrew` count, the container prefix migration, the image tag migration, and the list in
section 4.

Three things changed. The command does not move at all, so the largest pull request in that plan is
gone. The data directory moves rather than stays, because the command is `krewe`. The compose project
is now a decision of its own, and the recommendation is to leave it alone.

## 8. Recorded claims

**The plan step works on a real job.** This is captured output, not a rendering. It ran against a
control plane built from `bc62fac`, with the echo model, the local sandbox provider and the in memory
store. The job states a product sentence and has no parent, which is what `job.Planned` reads. So the
controller asked the session for a plan and told it to do no work. It then read the plan back off the
reply, put it on the row, and stopped the job for a person.

```
$ krewe job show 3da13ba0
3da13ba0  the rest of the rename to Quay Krewe
asking
for a person: an operator with a build installed today upgrades, types krewe, and every session,
container, conversation and secret they already had is still there
plan, not approved yet:
  Step 1: settle the seven decisions in docs/RENAME.md and record the answers
  Step 2: rename the container prefix and the sandbox image tag, reading both names for one release
  Step 3: move the data directory to ~/.krewe behind the guard that refuses to start empty
  Step 4: draw the wordmark as KREWE and hold the header to three rows
  Step 5: rename the words in the documents, the flows, the roles and the skills
  Step 6: rename the repository and move the module path with it
  Step 7: close issue 517
asking: This job has not started. Here is the plan for it, and here is the sentence it serves.
...
Does this plan get that sentence? Answer yes and the work starts against this plan.
answer it with krewe job answer 3da13ba0 "..."
```

To reproduce it, do five things. Build `cmd/controlplane` and `cmd/krewe`. Start the control plane
with `QC_MODEL=echo`, `QC_SANDBOX=local` and a data directory. Create a workspace and a project.
Give the project a repository. Declare a job with `--product`, with `--mode dangerous` and with no
parent.

**One thing that run does not prove, and it is worth saying.** The echo driver has no model behind it.
It echoes the task text back, so the seven steps above came out of the brief rather than out of a
model's reasoning. What the run proves is the mechanism. The controller asks for a plan and for no
work. The system reads the plan off the reply, the row carries it, and the job stops for a person.

**The check is not decorative.** The first attempt is worth recording. A job declared with the same
sentence, and a brief carrying no steps, was asked twice. It answered with no readable plan both
times, and the system stopped it. It never started any work. The reason names what the system could
not read.

```
this job serves a sentence, so a person approves its plan before any work starts, and the session was
asked twice and answered with no plan the system could read
```

**What is verified, and what is not.** The counts in section 1 and the widths in section 2.6 are
measured in this tree at `ccc9b0a`. The `go install` failure in section 2.1 is captured output. The
findings on the container prefix, the image tag and `env-check` are read out of the code, at the
lines named beside each one. The whole suite passes here: 41 packages, and 900 scenarios in
`features/`.

Two things are not verified. The trademark position, which the top of this document names. And
section 2.3, because this workspace has no docker daemon, so no part of it ran against a real compose
stack. The volume prefix claim comes from compose's documented behaviour and from the named volumes
at the foot of `deploy/docker-compose.yml`. A person with a running stack confirms it in one command:
`docker volume ls | grep postgres-data`.
