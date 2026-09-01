# Twenty open issues, read against main on 1 September 2026

## Why this exists

Eighty two issues are open. At least thirteen of them carry a merged commit on main that names
them. A commit subject is not evidence. One commit changes a comment and leaves the defect. One
records a decision in a document and builds no mechanism. One adds a scenario for a flow that does
not exist.

So this audit asks one question of each issue. Does main today carry the behaviour the issue asks
for? It does not ask who worked on it.

## How each issue was decided

Read the issue in full, including its acceptance criteria. Then read the code, the tests and the
scenarios on main that would carry that behaviour. Search for the behaviour. Do not trust a commit
message or an issue reference.

Where the issue names a command, run the command and read the output. Close the issue only when main
meets every criterion it states. Partly done stays open.

The slice for this audit is twenty issues. They are 237, 238, 239, 243, 245, 246, 248, 255, 266,
269, 272, 281, 302, 314, 331, 332, 333, 334, 345 and 347.

## Closed

None. No issue in this slice met every criterion it states.

## Left open

**237, import a skill from a url.** `runSkillImport` in `cmd/krewe/skills.go` refuses anything but
a directory. The command prints `usage: krewe skill import <directory>`. No code fetches a url, and
nothing records where a skill came from.

**238, listing sessions times out.** `ListSessions` in `internal/controlplane/server.go` still calls
`withUsage` per session inside the request. That reads the host filesystem through
`Storage.transcript`, which runs a glob and a stat for each row. `withStaleness` still makes one
`SessionSkills` query per session.

**239, the system reports its own errors.** `grpc.NewServer` in `cmd/controlplane/main.go` takes the
telemetry options and the auth options only. No interceptor logs a call that fails or runs long.
`krewe doctor` answers `unknown command "doctor"`.

**243, an attached conversation leaves no history.** `ListTasks` in
`internal/controlplane/server.go` reads the store. The store holds only what the dispatch path
writes, through `AppendTask` in `internal/controlplane/events.go`. Nothing on the attach path
records a task, and nothing reads the transcript back as history.

**245, no skills view in the console.** `internal/console/resources.go` and
`internal/console/jobs.go` build ten views: workspaces, projects, context, secrets, sessions, tasks,
archived, stats, room, keys and jobs. None of them is skills.

**246, attach asks for a secret that is already set.** `runSkillAttach` in `cmd/krewe/skills.go`
prints `it needs <name>` for every secret the skill declares. It reads no secret, so it says the
same thing whether the workspace holds the secret or not. The issue asks for three answers and the
command gives one.

**248, the product is a repository you clone.** Two slices landed: the system keeps one directory,
and configuration lives outside the checkout. The rest did not. `krewe up` answers `unknown
command`, `.github/workflows/` holds `ci.yml` alone, and the quick start in `README.md` is `make
install` from a checkout.

**255, one clone per workspace and a working tree per session.** Three parts of four landed. The
brief in `skills/git/SKILL.md` names the shared clone and the working tree. `QC_SESSION_ID` reaches
the sandbox, and `features/sandboxes.feature` carries the two session scenario.

Nothing prunes a working tree or its branch when a session ends. `CHANGELOG.md` says so itself at
line 1513.

**266, a system has no backup.** `krewe export` answers `unknown command`. The compose file at
`deploy/docker-compose.yml` keeps Postgres in the named volume `postgres-data`. `README.md` says
nothing about where the state lives or which Docker command destroys it.

**269, a session skill listing answers the wrong question.** `ListSkills` in
`internal/controlplane/server.go` answers a session request from `capabilityOf`, which reads the
workspace's current attachments. It does not read `skills_fingerprint`, and it marks no row the
session does not hold. The second half is open too. The skills mount is `/home/agent/skills`, which sits outside the
working directory. `buildArgs` in `internal/model/claudecode.go` adds no extra directory, and
`skill.Index` writes a path rather than the brief.

**272, one search over everything.** The service in `proto/quaycrew/v1/controlplane.proto` declares
seventy two calls and none of them searches. `krewe find` answers `unknown command "find"`.

**281, a project holds one blob of context.** The `contexts` table in
`internal/store/migrations/0006_contexts.up.sql` still carries one `body` column. No later migration
adds a document. No command adds or removes one.

**302, what a small local web view still needs.** The smallest slice landed as `krewe web`, with
`features/web.feature` behind it. The slice the issue describes also renders a reply as formatted
text with coloured code, and `internal/web/templates/session.html` writes the reply into a `pre`
element. The issue is also the parent of 331, 332, 333 and 334, which are all open.

**314, two sources of git identity.** `turnEnv` in `internal/controlplane/server.go` still sets
`GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` and `GIT_COMMITTER_EMAIL`.
`cmd/controlplane/main.go` still reads `QC_GIT_AUTHOR_NAME` and `QC_GIT_AUTHOR_EMAIL`, and
`deploy/docker-compose.yml` and `deploy/env.example` still carry both. Nothing says at startup that
the variables no longer apply.

**331, the web view looks like a browser default.** `docs/design/DESIGN.md` does not exist.
`internal/web/static/style.css` holds tokens, and no document defines them.
`internal/web/templates/layout.html` draws a header and one main element, so there is no tree and no
three column layout. The header carries no model, no mode and no usage.

**332, a reply renders as plain text.** `internal/web/templates/session.html` writes
`<pre class="reply">{{.Reply}}</pre>`. `internal/web` holds no renderer and no code colouring, so a
heading, a list and a fenced block all read as plain characters.

**333, a task does not say what it cost.** The `Task` message in
`proto/quaycrew/v1/controlplane.proto` carries id, session, prompt, reply, status, failure, a
timestamp and a trace identifier. It carries no duration and no usage. `runTaskList` in
`cmd/krewe/task.go` prints neither.

**334, the web view does not follow a running session.** The proto declares no `StreamTasks` call.
`internal/web/templates/layout.html` refreshes the whole page with a meta element, so nothing streams
and nothing says that the page lost the system.

**345, a trace stops at the edge of the control plane.** `telemetry.Record` has two callers, both in
`internal/job/controller.go`, and both record a job. No span covers a task, a sandbox or the model
call. `cmd/krewe` never calls `telemetry.Init`, so the command line tool starts no trace.

**347, dashboards and a cost ceiling alert.** `deploy/grafana/` holds `datasources.yaml` and nothing
else. There is no dashboard and no alert rule.

## Could not tell

None.

## Duplicates

None inside this slice. Three pairs overlap and each pair is two different pieces of work.

Issue 238 hands points three and four to 239 by name, so the two divide one investigation. Issue 266
names 248 as the place a bind mount decision may close it, and 248 covers distribution rather than
backup. Issues 331, 332, 333 and 334 each say "Part of #302", so 302 is their parent.

## Totals

Closed: 0. Left open: 20. Could not tell: 0.
