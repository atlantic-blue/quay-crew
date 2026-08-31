# The rename to blue quay

The owner decided the name. The tool becomes blue quay.

This file is the plan. Nobody approved it yet. Read the seven decisions below and answer each one.
The work starts when a person approves the plan. Nothing is renamed before then.

Issue 585 measured `fabric` and `blueprint` against the bar in `docs/NAME.md` and refused both. It
recommended that the tool keeps `krewe`. That recommendation is overtaken. The decision is settled
and this plan does not reopen it.

## The state this starts from

Measured at commit `bc62fac`. These are counts, not estimates.

- `krewe` appears 2,741 times in 600 files.
- `quaycrew` appears 5,077 times in 318 files.
- `quay` outside `quaycrew` appears 1,309 times.
- The address `atlantic-blue/quay-crew` appears 426 times in 76 files.
- 439 Go files import the module path `github.com/atlantic-blue/krewe`.
- `cmd/quay` and `cmd/krewe` both exist. `cmd/quay` refuses every call and names `krewe`.

Only piece 1 of issue 517 landed. The tree is half renamed.

One more fact, and it changes the order of the work. The module path does not resolve today. There
is no repository at `github.com/atlantic-blue/krewe`. The Go module proxy answers `not found: module
github.com/atlantic-blue/krewe`. Verified against `proxy.golang.org` on 31 August 2026. So nobody
outside this checkout can install the tool. This rename repairs that fault, because the new module
path will match the new repository address.

## Decision 1: the command a person types

**Recommendation. The executable is `bluequay`. A person types `bluequay`.**

The name is two words and an executable is one word, so the two words join. The console prompt, the
manual and every example say `bluequay`.

Checked on 31 August 2026. `bluequay` is free on npm, on the Python Package Index, on crates.io and
in Homebrew. GitHub holds no repository with the name. One thing is not free: a Docker Hub account
named `bluequay` exists, registered on 6 March 2026, with no public images. That blocks nothing
today, because the sandbox image is built locally. It does block publishing to
`docker.io/bluequay/...` later.

**Cost.** Eight characters to type instead of five. That is three more characters on every command,
for ever.

**Refused, and why.** `bq` collides with the Google BigQuery command line tool, and `bq` is taken on
npm. `blue` does not name the product. `quay` is the collision this whole rename exists to remove.
This plan ships no short alias, because the obvious short alias is `bq`.

## Decision 2: the Go module path

**Recommendation. The module path becomes `github.com/atlantic-blue/bluequay`, in all three `go.mod`
files.**

The module path resolves through the repository address, so it must equal the repository name from
decision 7. That is why the two are one decision in practice.

**Cost.** 439 Go files change their import lines. The change is mechanical and the compiler proves
it. The three hook modules are separate modules and each one changes its own `module` line.

## Decision 3: the data directory

Today the directory is `~/.quay`. It holds the two tokens, the key that seals every secret, and
every conversation.

**Recommendation. The directory becomes `~/.bluequay`. The tool refuses to start when `~/.bluequay`
is absent and `~/.quay` is present, and prints the one `mv` command. The tool never moves the data
itself. The environment variable `QUAY_HOME` becomes `BLUEQUAY_HOME`.**

This is not a new mechanism. `cmd/krewe/home.go` already does exactly this for the move that put
everything in one directory. The comment there gives the reason: what sits in that directory is a
gigabyte of transcripts, two tokens and the key that unseals every secret, and a tool that quietly
relocates those is a tool nobody can undo. Starting anyway is worse, because the system comes up
empty on a token nothing else holds, and every conversation reads as lost.

Issue 517 said to write no migration guard. That instruction answered a layout nobody had any more.
This layout is on the operator's machine right now, so the guard earns its place.

**Cost.** One manual step for the operator, once. One entry added to the `retired` list in
`cmd/krewe/home.go`, plus its test. The guard gets deleted when the operator confirms the move.

## Decision 4: the container name prefix

Today the prefix is `quaycrew-`, in `internal/sandbox/sandbox.go`.

**Recommendation. New containers get the prefix `bluequay-`. Every reader accepts both prefixes for
one release. Only the new prefix is ever written.**

This is the migration the earlier finding named. An operator has a build installed now. When the new
binary lands, the running containers keep the `quaycrew-` prefix. `sessionOf` in
`internal/headroom/daemon.go` cuts the prefix and then requires 24 hexadecimal characters, so it
stops matching those containers. The daemon then reports no sandboxes, and a drain leaves them
running with nothing tracking them.

Reading both prefixes is safe. The guard requires exactly 24 hexadecimal characters after the
prefix, so the stack's own services, such as `quaycrew-postgres-1`, still fail the check.
`SANDBOX_PATTERN` in the `Makefile` gets the same treatment.

**Cost.** One extra branch in the reader, and a test for each prefix. The legacy prefix comes out at
a stated point, which is when no session started under the old build is still running.

## Decision 5: the local sandbox image tag

Today the tag is `quaycrew-sandbox-claude:local`. It appears 14 times in 10 files, including the
`Makefile`, the continuous integration workflow and `deploy/env.example`.

**Recommendation. The tag becomes `bluequay-sandbox-claude:local`. The operator runs `make
sandbox-image` once. Before that, the upgrade instruction offers a one line retag, which costs
seconds instead of minutes.**

The retag is `docker tag quaycrew-sandbox-claude:local bluequay-sandbox-claude:local`.

**Cost.** One operator step. If the operator skips it, the first task fails because the image is
absent. The changelog entry names both the retag and the rebuild.

## Decision 6: the wordmark

Today the console header draws QUAY in four block letters, three rows tall and 36 columns wide, in
`internal/console/view.go`.

**Recommendation. The drawn wordmark stays QUAY. The header's own status text carries the whole
name, blue quay.**

Quay is the second word of the new name, so the mark is already correct. It is the short form of the
name, the way K9s is the short form of its own. Drawing BLUEQUAY needs eight letters at about 70
columns, which is twice the current width. `withLogo` hides the mark when the window cannot hold it
beside the status block, so a wider mark disappears on most windows. The header would then be blank
where the branding was.

**Cost.** The drawn mark does not say the first word. A reader sees blue quay in the header text and
QUAY in the mark. Nothing is drawn again and the width test does not move.

## Decision 7: the GitHub repository name

**Recommendation. `atlantic-blue/quay-crew` becomes `atlantic-blue/bluequay`.**

One word, so it matches the module path from decision 2. `atlantic-blue/bluequay` is free.

The rename is the owner's step and it goes last of all, after every code pull request has merged.

### What still works after the rename

GitHub keeps a permanent redirect from the old address. These keep working.

- Every link already written, in an issue, a pull request, a comment or a document.
- `git fetch` and `git push` from an existing clone, over both HTTPS and SSH.
- Every call to the interface at the old address.
- Every open pull request. A repository rename moves them, with their numbers and their branches.

### What does not work after the rename

- `git remote -v` still prints the old address, so every clone needs `git remote set-url origin
  https://github.com/atlantic-blue/bluequay.git`. The redirect hides this until somebody reads the
  remote and gets confused.
- The 426 written occurrences of `atlantic-blue/quay-crew` inside this repository become stale. They
  redirect, so nothing breaks, and they are still wrong. Pull request 6 rewrites them.
- The redirect dies the moment anybody creates a repository named `atlantic-blue/quay-crew` again.
  **Nobody creates that name again, in any account.** This is the one rule that keeps every old link
  alive.
- The pipeline holds no reference to the repository address, so nothing in
  `.github/workflows/ci.yml` changes for the rename. Verified by search at `bc62fac`.

## What must not break

Four hazards, from the earlier finding, plus two this plan found.

**The binary leaves the path.** `make tool` installs over whatever `krewe` or `quay` the shell
already runs. It gains a third name to look for, so the first build after the rename installs
`bluequay` in the same directory. `krewe` is left behind as a refusal, exactly as `quay` is today,
so an operator with the old name in their fingers gets one sentence naming the new one.

**The running containers keep the old prefix.** Decision 4 answers this.

**The image tag goes missing.** Decision 5 answers this.

**The data directory holds what cannot be regenerated.** Decision 3 answers this.

**The Postgres volume is named after the compose project, and this plan found it.** The compose
project is `quaycrew`. Compose names a volume `<project>_<volume>`, so the database lives in
`quaycrew_postgres-data` today. Renaming the compose project to `bluequay` creates a new empty
volume and orphans the old one. Every job, session, task, secret and role row is in that database.
Compose cannot refuse to start the way the tool can, so nothing warns the operator.

The recommendation is to pin the four named volumes with an explicit `name:` key holding their
current full names, and to leave the Postgres database name, user and password default as
`quaycrew`. Those three are internal and no person reads them. The compose project name still moves
to `bluequay`, so the container prefix and the project agree.

The cost is honest and it is real. Eleven occurrences of `quaycrew` stay in
`deploy/docker-compose.yml`, with a comment saying why: seven in the database name, the user and the
password default, and four in the pinned volume names. Only the Postgres volume is irreplaceable.
The other three hold the event log and the telemetry, which the store can produce again, and they
are pinned so that nothing goes missing without a word. So issue 517's acceptance criterion, that no
string `quay` remains in the code, is not met by this plan.

**The memory file mark stays `quay:`, and this plan found it too.** `internal/sandbox/memory.go`
opens each section of a session's `CLAUDE.md` with `<!-- quay: -->`. A session that is already
running wrote its file under that mark. A build that stopped recognising it would sweep every level
of that file into one. The mark does not change here. It gets its own issue, and it is the second
reason the string `quay` survives.

## The pull requests, in order

Seven, each revertable on its own, merged in this order.

1. **This plan.** Documents only. No code moves.
2. **The command and the module path.** `cmd/krewe` becomes `cmd/bluequay`. All three `go.mod` files
  move to `github.com/atlantic-blue/bluequay`. `cmd/krewe` becomes a refusal beside `cmd/quay`.
  `make tool` installs three names.
3. **The runtime names.** The container prefix, the compose project, the sandbox pattern and the
  image tag. Readers accept both prefixes. The compose volumes get pinned names.
4. **The data directory.** `~/.bluequay`, `BLUEQUAY_HOME`, and the guard that names the move.
5. **The wordmark and the header.** The mark stays. The header text carries the whole name.
6. **The documentation.** Every repository address, every command example, the README and every file
  under `docs/` and `flows/`. This goes after the code, so every example describes something that
  runs.
7. **The repository rename.** The owner renames it on GitHub. Every clone sets its remote.

Each of pull requests 2 to 6 ships its tests in the same change: unit tests for the naming and for
the refusal, and a scenario under `features/` a person can read. Each one tests the way off the old
name, not only the way on.

## What this does to issue 517

**This work supersedes issue 517. It does not close it as done.**

Issue 517 decided that the tool becomes `krewe`. That decision is overtaken, so its title is now
wrong. Its piece 1 shipped and its pieces 2 to 6 never did. Those five pieces are re-planned here,
under the new name, plus the two hazards this plan found.

Leaving 517 open means two plans for one rename. The two disagree about the name, about the
migration guard and about the acceptance criteria, and somebody else then has to find the
disagreements. So 517 gets closed as superseded, with a comment pointing here.

That is a decision for a person, and it is the eighth thing this plan asks.

## What nobody has checked

The trademark registers, for `blue quay` and for `bluequay`. `docs/NAME.md` already records why: the
United States Patent and Trademark Office search sits behind a challenge, and the United Kingdom
service did not answer from a sandbox. No name in this repository has been searched against a live
register. Treat the trademark position as unknown.

One task in session `32c272cc` measures blue quay against the bar in `docs/NAME.md`. It includes the
question of whether keeping the word quay carries the Red Hat collision forward. **As of 31 August
2026 that task has not answered.** Its answer belongs in this file as a recorded claim when it
arrives. It does not block this plan and it does not reopen the decision.


## The plan mechanism ran on this job

Pull request 581 shipped the gate that holds a job until a person approves its plan. This plan went
through it.

Observed on 31 August 2026, against a live control plane started with the in memory store, the echo
model and the local sandbox provider. Reproduce it with `go build -o /tmp/cp ./cmd/controlplane`,
then `QC_MODEL=echo QC_SANDBOX=local QC_DATA_DIR=/tmp/qdata QC_DATA_HOST=/tmp/qdata
QC_GRPC_ADDR=127.0.0.1:50051 /tmp/cp`. No container runtime is needed. Under the echo model the
session echoes what it was sent, so the seven steps came from the brief rather than from a model.
The gate, the reader, the row and the question are the real ones.

The job is `064b8a34` in `acme/rename`. It ran one task, wrote a seven step plan, and stopped. This
is `krewe job show 064b8a34`, cut to the part that matters:

```
064b8a34  rename the tool to blue quay
asking
for a person: An operator upgrades an installed build, types bluequay, and finds every session,
secret and conversation they had, under the new name.
plan, not approved yet:
  Step 1: write the plan and the decisions a person settles before any rename starts
  Step 2: rename the command and the module path to bluequay across all three go.mod files
  Step 3: rename the runtime names, and read the old container prefix for one release
  Step 4: move the data directory to ~/.bluequay behind a guard that names the move
  Step 5: keep the drawn wordmark and put the whole name in the header text beside it
  Step 6: rewrite every repository address and every command example in the documentation
  Step 7: rename the repository, set every remote, and never recreate the old name
answer it with krewe job answer 064b8a34 "..."
```

`krewe task list 1aaaec3a` reports one task in that session. So the gate did what it says: the job
asked for a plan, did no work, and holds.

That live job dies with the sandbox it ran in. This file is the plan that survives, and those seven
steps are the seven pull requests listed above.


