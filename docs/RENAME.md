# The rename to blue quay

The owner decided the name. The tool becomes **blue quay**.

That decision is settled and this document does not reopen it. `docs/NAME.md` holds the bar a name
must clear. It also holds the evidence for the names measured against it. This document holds what
the rename does: the eight decisions it needs, the migration for each thing that breaks, and the
order the pull requests land in.

Nothing here is built yet. This document is the plan, and it waits for a person to approve it.

## 1. The state it starts from

Measured at `bc62fac` on `main`, 31 August 2026, over the 963 files git tracks.

- `krewe` appears 2,741 times in 600 files.
- `quaycrew` appears 5,077 times in 318 files.
- `quay`, not counting `quaycrew`, appears 1,309 times in 154 files.

Those totals cover the whole tree, and on their own they mislead. 4,206 of the 5,077 `quaycrew`
occurrences are the protobuf package identifier. That is `quaycrew.v1`, the generated Go alias
`quaycrewv1`, and the path `quaycrew/v1`. Another 2,040 occurrences sit in `gen/` and in
`CHANGELOG.md`, which are generated code and a historical record.

Take out the protobuf identifier, the generated code and the changelog. The rename is then **3,885
occurrences in 652 files**. Section 2.7 says why the protobuf identifier stays.

Only piece 1 of [#517](https://github.com/atlantic-blue/quay-crew/issues/517) landed, so the tree is
half renamed today. `cmd/krewe` is the tool. `cmd/quay` is a binary that refuses every invocation and
names `krewe`. The module path is `github.com/atlantic-blue/krewe`. The container prefix, the compose
project, the sandbox image and the Postgres database are all still `quaycrew`. The data directory is
still `~/.quay`. The console still spells QUAY.

## 2. The eight decisions

Each one is a decision, not a detail. Each carries a recommendation and its cost.

### 2.1 The command a person types

**Recommendation: the executable is `bluequay`.** One word, all lowercase, no hyphen. A person types
`bluequay task "..."`. The two word form "blue quay" is the product name in prose. It never appears
in a command.

**Cost.** Eight characters, where `krewe` is five and `quay` was four. A person types it dozens of
times a day. There is no short alias, because the obvious ones are taken. `bq` is Google's BigQuery
command line tool, and it is already on many developer machines. `blue` is too generic to search for.
A person who reads "blue quay" must learn that the command closes the gap.

**The runner up, and why not.** `quay` alone is four characters, and it is the word a person says. It
costs the collision that #517 renamed away from: `brew install quay` and `npm i quay` land back on
Red Hat's registry. It also needs the refusal binary at `cmd/quay` deleted and its name reused. An
operator who did not upgrade then keeps a `quay` that says "quay is called krewe now". That sentence
is then false. So I recommend against it.

### 2.2 The Go module path

**Recommendation: `github.com/atlantic-blue/bluequay`, in all four `go.mod` files.**

There are four, not three. The root module is one. `hooks/prompt-analyser`,
`hooks/deploy-identity-gate` and `hooks/merge-gate` are the other three, each its own module by
design. Nothing outside `hooks/` imports them, so the three hook paths move with the root.

**Cost. None that is new.** The module path today is `github.com/atlantic-blue/krewe`, and the
repository is `atlantic-blue/quay-crew`. So the path stopped resolving when #517 landed. `go install`
from the address never worked. This changes the word, not the situation. It starts to matter on the
day the repository is renamed, which is decision 2.8 and is the owner's.

### 2.3 The data directory

**Recommendation: keep `~/.quay`, and keep `QUAY_HOME`.**

This one directory holds what nothing can regenerate: the system token, the driver token, the sealing
key that unseals every secret, and every conversation. `cmd/krewe/home.go` says so in its own words.

Under the name `krewe` the directory was wrong, and #517 piece 3 renamed it for that reason. Under
**blue quay** the word `quay` is in the product name again. So `~/.quay` is short for the product and
reads correctly. Keeping it removes the most dangerous migration in the rename, and loses nothing.

**Cost.** A person who knows the command only as `bluequay` will not guess the directory. The fix is
one line. The first run output already prints four commands, and it gains a line naming the
directory.

**If the answer is to move it anyway**, the migration already exists and is cheap.
`refuseTheOldLayout` in `cmd/krewe/home.go` refuses to start when the new directory is absent and the
old one is present. It prints the exact `mv`. It needs one more entry in its `retired` table. What it
must not do is start. A system that comes up on a fresh token, with a sealing key it has not seen,
reads as every conversation lost.

Piece 3 of #517 says "do not write a migration guard". That instruction is overtaken. The guard was
built after #517, and it is what makes the move safe.

### 2.4 The container name prefix

**Recommendation: `quaycrew-` becomes `bluequay-`, and the build reads both for one release.**

This is the largest break in the rename. It is the finding recorded at `a326ed0`. The prefix is one
constant, `sandbox.ContainerPrefix` at `internal/sandbox/sandbox.go:195`. Four things derive from it.

- `ContainerName` builds the name a sandbox starts under.
- `sandboxName`, the regular expression at `internal/sandbox/docker.go:163`, is what `Stranded` uses
  to list containers.
- `sessionOf` at `internal/headroom/daemon.go:230` decides which container belongs to a session.
- `SANDBOX_PATTERN` in the Makefile sweeps the containers an upgrade left behind.

Change the constant alone and every running container becomes invisible. `krewe drain` does not find
it. `Remove` targets a name that does not exist, and it treats absent as success. The Makefile sweep
skips it. The container keeps running and holds its memory. The next start of that session then makes
a second container beside it.

**The migration.** Write the new prefix, and read both. `Stranded` and `sessionOf` accept either
prefix. `Remove` tries the new name and then the old one. The Makefile pattern matches both. Ship a
test that the system still finds and still removes a container named `quaycrew-<24 hex>`. That is the
way off the old name, and rule 46 says it is the half that goes untested.

**Cost.** Two prefixes in the code, until the read of the old one is deleted. The changelog entry
names the release that deletes it, so it does not become permanent by default.

### 2.5 The local sandbox image tag

**Recommendation: `quaycrew-sandbox-claude:local` becomes `bluequay-sandbox-claude:local`. The build
tags the image under both names for one release.**

The tag sits in three places: `SANDBOX_IMAGE` in the Makefile, `QC_SANDBOX_IMAGE` in
`deploy/env.example`, and the two integration tests that fall back to it.

The trap is that an installed operator's own `~/.quay/env` pins the old tag. An upgrade does not
rewrite that file. So their stack names an image the new build no longer produces. The first task
then fails on a missing image. That failure reads as a broken system, not as a rename.

**The migration.** `make sandbox-image` builds the new tag. It then applies the old tag to the same
image, so both names resolve. `make env-check` gains a line that names a key whose value is the
retired tag. Today it reports only a key that is absent. It never reports a value that is stale.

**Cost.** One extra tag, and about five lines in `env-check`, carried for one release.

### 2.6 The wordmark

**Recommendation: keep the QUAY letters exactly as they are. Change the colour from green to blue.**

The wordmark is three rows at `internal/console/view.go:149`. The `mark` style at
`internal/console/style.go:52` draws it in `ansiGreen`. The product is blue quay, so the letters carry
one word and the colour carries the other. It costs no columns and no rows. No width test changes.

**Measured, not assumed.** I put an eight letter mark of the same idiom into the console package. I
then drew the header at every width from 60 to 220 columns.

```
four letters: 36 columns; eight letters: 73 columns
QUAY: drawn from 73 columns up
BLUEQUAY: drawn from 110 columns up
```

That is captured output from a throwaway test in `internal/console`. The test was deleted afterwards
and it is not in this change. To reproduce it, add a test in package `console` that assigns to the
package variable `logo` and calls `headerLines()` at each width.

`TestTheWordmarkSurvivesAConversationBesideIt` asserts the mark is drawn at 84 columns. That is what
a conversation beside the console leaves in a window of 168 columns. BLUEQUAY needs 110 columns, so
it is gone in exactly that case. Spelling the whole name hides the wordmark at the width the console
is used at most.

**Cost.** A colour then carries the word "blue". A screenshot pasted as text loses it. A person whose
terminal remaps its palette may not see blue at all.

### 2.7 What does not rename at all

This is less a decision than the list that keeps a running system working. Each entry is a name on a
wire, in a store, or inside a container that is already up. Rename any of them and something is
stranded. Nobody types any of them.

- **The protobuf package `quaycrew.v1`.** It is the gRPC method path,
  `/quaycrew.v1.ControlPlaneService/...`. Rename it and a new build cannot talk to a session the old
  build started. #517 makes the same argument for leaving `QC_TOKEN`, `QC_GRPC_ADDR`, `QC_SESSION_ID`
  and `QC_TRACEPARENT` alone. It is also 4,206 of the 5,077 occurrences. Leaving it is what makes the
  rest fit in small pull requests.
- **The Postgres database and user, both `quaycrew`.** Renaming them is a data migration on every
  installed stack, for a string no reader sees.
- **The metric names `quaycrew.tasks`, `quaycrew.tokens` and `quaycrew.cost.usd`.** The span
  attributes `quaycrew.job` and `quaycrew.workspace` go with them. Rename a metric and the history
  under it breaks. A dashboard that loses its past is worse than one that carries an old word.
- **The image label `com.quaycrew.build`**, which is read off images that already exist.
- **`AttachedSessionName`, the tmux session named `krewe` inside a running sandbox.** An operator's
  open conversation lives under that name. Rename it and `krewe attach` starts a second conversation
  beside the one they were in. `endConversation` then kills a name that is not there.
- **`QC_TOKEN`, `QC_GRPC_ADDR`, `QC_SESSION_ID` and `QC_TRACEPARENT`.** The memory file mark
  `<!-- quay:session -->` goes with them. #517 already names all five.

Each of these is a deliberate exception. The changelog states every one. A reader who searches for
`quaycrew` after the rename then finds a reason, not a miss.

### 2.8 The GitHub repository

**Recommendation: do not rename it. `atlantic-blue/quay-crew` stays.**

This step is the owner's. It is named here so nobody forgets it, not so anybody does it. GitHub
redirects an old address, so nothing written today breaks. Every worktree still needs its remote
updated, and every open pull request should land first. When it happens, the module path in decision
2.2 finally resolves.

## 3. What must not break

One operator has a build installed now. Four things break at the moment the new binary lands on their
path. Each one has a migration.

**The command leaves the path.** `krewe` stops existing. `cmd/krewe` becomes a binary that refuses
every invocation and names `bluequay`, exactly as `cmd/quay` does today for `krewe`. Both refusals
ship. `quay` gains a corrected sentence naming `bluequay`, and `krewe` gains a new one. That is three
binaries on the path, two of them refusals. The changelog names the release that deletes them.

**The running containers keep the `quaycrew-` prefix.** Decision 2.4 covers it. Read both prefixes,
write the new one, and test that the system still finds the old one.

**The image tag goes missing and the first task fails.** Decision 2.5 covers it. Tag the image under
both names for one release, and make `env-check` name a stale value.

**The data directory holds what nothing can regenerate.** Decision 2.3 covers it. The recommendation
is to leave it alone. If it moves, the guard refuses to start and prints the `mv`. It never starts
empty.

## 4. The order the pull requests land in

Each one is revertable on its own. Each ships its tests in the same change. Each tests the way off the
old name beside the way on to the new one.

1. **This plan.** `docs/RENAME.md`, and the decision recorded in `docs/NAME.md`. No code.
2. **The executable and the module path.** `cmd/krewe` becomes `cmd/bluequay`. The module path moves
   in four `go.mod` files, and every import follows. `cmd/krewe` and `cmd/quay` both become refusals
   that name `bluequay`. This is the largest pull request, and the only one that touches every
   package.
3. **The runtime names.** The container prefix and the sandbox image tag, both reading old and writing
   new, plus the compose project name. None of it is user facing. All of it decides whether a running
   stack is found.
4. **The console.** The wordmark colour, and the word `krewe` where the console says it to a person:
   the key hint that reads "run any krewe command", the help panel, and the manual.
5. **The words.** README, `docs/`, `features/`, `flows/`, `roles/` and `skills/`. Last, so the
   examples describe something that already works.

Pull request 2 conflicts with every open branch, so it lands when the board is as clear as it gets.
Pull requests 3 and 4 do not depend on each other. Either can land first after 2.

## 5. This supersedes issue 517

**Recommendation: #517 is superseded, not completed.** Its headline is "it becomes krewe", and that is
now false. Its pieces 2 to 6 are this same rename under a word we no longer use. Two open issues for
one rename will disagree in a few places, and somebody else must then find the differences.

Closing it is the owner's step, not mine. Three things in it are still true, and this document carries
all three. Section 2.7 carries the list of what not to change. Decision 2.8 carries the repository
rename as the owner's last step. Section 4 carries its instruction to ship the rename as separate
revertable pull requests.

One thing in #517 is overtaken, and this document says so at decision 2.3: "do not write a migration
guard". The guard exists now, and it is what makes the data directory safe to move.

## 6. Recorded claims, and what has not answered

**The plan step works on a real job.** This is captured output, not a rendering. It ran against a
control plane built from `bc62fac`, with the echo model, the local sandbox provider and the in memory
store. The job states a product sentence and has no parent, which is what `job.Planned` reads. So the
controller asked the session for a plan, and told it to do no work.

```
$ bluequay job show 5f303192
5f303192  the tool becomes blue quay
asking
for a person: an operator with a build installed today types the new command, and every session,
container and conversation they already had is still there
plan, not approved yet:
  Step 1: settle the eight decisions in docs/RENAME.md and record the answers
  Step 2: rename the executable to bluequay and the module path with it, leaving krewe and quay refusing
  Step 3: rename the container prefix and the sandbox image tag, reading both for one release
  Step 4: draw the wordmark in blue and leave the QUAY letters alone
  Step 5: keep the data directory at ~/.quay, and say so in the changelog
  Step 6: rename the words in the documentation, the flows, the roles and the skills
  Step 7: hand the repository rename to the owner and close issue 517
asking: This job has not started. Here is the plan for it, and here is the sentence it serves.
...
Does this plan get that sentence? Answer yes and the work starts against this plan.
```

To reproduce it, do four things. Build `cmd/controlplane` and `cmd/krewe`. Start the control plane
with `QC_MODEL=echo` and `QC_SANDBOX=local`. Raise the workspace `max-depth` above zero. Then declare
a job with `--product` and no parent. The capture writes the command as `bluequay`, which is the name
this plan recommends. The binary that produced it is called `krewe` today.

The first attempt is worth recording too, because it shows the check is not decorative. One session
answered with no readable plan. The system asked a second time, and then stopped the job. The reason
names what the system could not read. That job never started any work.

**One claim is outstanding.** A task in session `32c272cc` checks **blue quay** against the bar in
`docs/NAME.md`. It also asks whether the word `quay` carries the Red Hat collision forward. It did not
answer before this document was written. It does not block this work, and it does not reopen the
decision. When it answers, its finding lands in `docs/NAME.md` beside the entries for `fabric` and
`blueprint`, and a line here points at it.

The trademark position is unchanged and still unknown. `docs/NAME.md` says why. No name in this
repository was searched against a live register, and the two searches are a person's to run.
