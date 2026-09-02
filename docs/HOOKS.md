# Hooks

A system gives every session its rules as context. Context is advice, and the model decides whether to
take it.

The evidence is one working session on 13 August 2026. The system context held 100 kilobytes of rules.
The session broke three of them. It did not break the one about committing without approval, because
that one is not advice on the operator's machine: a hook refused the command and named how to ask.

A krewe sandbox had no such gate, so the rule that mattered most was the rule nothing checked. It has
one gate now, on the merge, and the rest of this document is the shape every other one takes.

## What a hook is, and is not

A hook is a constraint: what a session **may not** do, checked at the moment it tries, able to refuse
and say what to do instead. It can also add to what the session was told, which is the same machinery
pointed the other way.

Three entities, and keeping them apart is the useful part:

- A **skill** is a capability. What a session **can** do, and what it needs to do it. Passive.
  Nothing happens because a skill exists. Designed in [`SKILLS.md`](SKILLS.md).
- A **workflow** is a plan. What **should** happen, in what order, on what trigger. It has control
  flow, state, and a run that survives a restart. Designed in [`ARCHITECTURE.md`](ARCHITECTURE.md).
- A **hook** is a constraint. It has no run and no state. It fires, decides, returns, and nothing of
  it survives. It cannot choose what happens next; it can only allow, refuse, or add context to the
  thing already about to happen.

A hook is triggered by the session's own tool use. A workflow is triggered by a schedule or by the
operator. Fold a hook into a workflow and a constraint gains control flow, which means the thing that
decides whether an action is allowed can now take actions of its own.

**A hook is not in the model's context.** A skill's brief is prose the model reads. A hook is
invisible until it fires. That is the point: moving a checkable rule out of the context shrinks the
prompt, and a shorter prompt is followed more closely, so the advice that remains gets stronger.

## Where a hook lives

The same three layers as a skill, for the same reasons, and this is deliberate rather than
convenient. A hook was once built as `krewe hook <name>`, compiled into the command line tool. That
made it impossible to write a hook for one workspace, impossible to change one without releasing the
tool, and impossible to hand one to another system.

```mermaid
flowchart LR
    A["a repository of hooks<br/>files, reviewed, versioned"] -->|"krewe hook import"| B["the system's store<br/>pinned to a version"]
    B -->|"krewe hook attach"| C["a workspace, or the whole system"]
    C --> D["every session in it<br/>files mounted read only,<br/>a settings file rendered"]
    D --> E["the model runtime<br/>runs the hook on its event"]
```

**Files are the authoring format**, so a hook is code somebody reviews and versions. **The store is
the runtime**, so a system on a pod still has its hooks and a hook cannot change under a session using
it. **The sandbox gets files again**, because the thing that runs a hook takes a path to an
executable.

## The shape of a hook

```
hooks/prompt-analyser/
  hook.yaml         what it is, and what event it fires on
  bin/hook          the executable the runtime calls, built rather than committed
  go.mod            its own module, because a hook is not part of the system
  *.go              the source, beside the thing it builds
  hook.config.json  what it reads, so the paths are not compiled in
```

The entry point is built by `make hooks` and by the image build, and it is not in the history. A hook
is an executable, an executable is a build artifact, and one committed binary would run on one
processor type: this repository's image is built on both arm and amd machines.

`hook.yaml` is data. No expressions, no conditionals, nothing that runs on the host.

```yaml
name: prompt-analyser
version: 1
summary: Reads every message and hands the session a short brief beside it.
events:
  - on: UserPromptSubmit
    entry: bin/hook
    timeoutSeconds: 20
binaries:
  - claude
secrets: {}
```

### The fields

`name` is what it is called and the directory it lives in. Lowercase letters, digits and dashes only,
because it is also a directory name inside a sandbox.

`version` is an integer of 1 or more. A session is pinned to the version it started with, so editing
a hook never changes one that is already running.

`summary` is one line of at most 200 bytes. It is what a listing shows. Unlike a skill's summary, no
session pays for it, because a hook is not in the context.

`events` is one or more bindings, and a hook with none does nothing. Each binding has:

- `on`, the event name, which must be one the runtime actually raises: `PreToolUse`, `PostToolUse`,
  `UserPromptSubmit`, `Stop`, `SubagentStop`, `SessionStart`, `SessionEnd`, `Notification` or
  `PreCompact`. An unknown name is refused rather than accepted and never fired, because a hook that
  is quietly never called looks exactly like a hook that approves of everything.
- `matcher`, which tools this fires for, as the runtime's pattern, for example `Bash` or
  `Write|Edit`. Empty means every tool. It is refused on any event that is not `PreToolUse` or
  `PostToolUse`, because a matcher there is a mistake that silently does nothing.
- `entry`, the executable to run, relative to the hook's own directory. It has to be a file the hook
  actually carries, and it has to be executable. A hook whose entry point arrives without its
  executable bit fails inside a container with nothing pointing back at the import.
- `timeoutSeconds`, how long the runtime waits. Zero means the runtime's own default.

`binaries` are the commands the hook cannot work without, declared so a session missing one is
refused with a sentence rather than discovering it halfway through. The same rule a skill uses.

`secrets` names workspace secrets by name, never by value, with a line saying what each is for. A
name starting `QC_` or `CLAUDE_` is refused: those are the system's own.

## How a hook reaches a sandbox

Two things land, and neither is baked into the image.

**The files** are mounted read only at `/home/agent/hooks/<name>`, out of the workspace's own
directory on the host, exactly as a skill's files are mounted at `/home/agent/skills/<name>`.

**A settings file** is rendered to `/home/agent/hooks/settings.json`, listing every held hook against
its event. Every invocation of the model is given `--settings /home/agent/hooks/settings.json`.

The settings file is a separate file the system owns entirely, rather than `/home/agent/.claude/settings.json`,
and that is the one non obvious decision here. `/home/agent/.claude` is the workspace's conversation
directory, bind mounted from the host, written by the runtime and editable by the operator. Rendering
the system's hooks into it would mean merging with whatever else is in there on every task, and losing
an operator's edit the first time the merge is wrong. `--settings` loads additional settings, so the
operator's own file still applies and the system's file is never a place anybody else writes.

```mermaid
flowchart TD
    A["the store<br/>hooks the workspace holds"] --> B["&lt;data&gt;/workspaces/&lt;ws&gt;/hooks/&lt;name&gt;"]
    A --> C["&lt;data&gt;/workspaces/&lt;ws&gt;/hooks/settings.json"]
    B -->|"read only mount"| D["/home/agent/hooks/&lt;name&gt;"]
    C -->|"read only mount"| E["/home/agent/hooks/settings.json"]
    E -->|"--settings"| F["claude, on every task and every attach"]
    D --> F
```

Both the task and an attached conversation get the flag. A hook that guards commits and only runs on
dispatched tasks is a hook the operator walks around by opening the conversation.

## What a hook deliberately does not do

- **It holds no state.** Nothing survives a firing. A constraint that remembers is a workflow.
- **It does not choose what happens next.** Allow, refuse, or add context to the thing about to
  happen. Nothing else.
- **It is not given to the model to read.** A hook that has to be explained in the prompt to work is
  advice with extra steps.
- **It cannot ask for the system's own secrets.** `QC_` and `CLAUDE_` are refused at validation, the
  same rule a skill lives under.

## The hooks this build ships

Each one is a rule the system already carries and nothing else checks.

- **prompt-analyser.** Reads the message, asks a small model to restate it, and hands the session
  both. Adds context, never refuses.
- **merge-gate.** Reads each Bash command and refuses one that merges. `gh pr merge` in every
  spelling, the same merge over `gh api`, `curl` or `wget`, the `mergePullRequest` mutation, and a
  `git push` onto `main` or `master`. It is designed in
  [`hooks/merge-gate/README.md`](../hooks/merge-gate/README.md).
- **deploy-identity-gate.** Reads each Bash command and refuses one that opens a pull request over
  infrastructure the deploy identity was never asked about, or over an action that came back denied.
  It is designed in
  [`hooks/deploy-identity-gate/README.md`](../hooks/deploy-identity-gate/README.md).
- **process-gate.** Reads each Bash command and refuses one that ends a running process. `kill`,
  `pkill` and `killall` in every signal form, the terminal multiplexer's teardown verbs for the
  server, a session, a window and a pane, the container runtime's own ending verbs, the two service
  manager equivalents, and the older screen program's quit form. This product's own verbs stay open,
  because `krewe job stop` and `krewe flow stop` end the work in the record and signal nothing. It is
  designed in [`hooks/process-gate/README.md`](../hooks/process-gate/README.md).
- **test-gate.** Reads each write and each Bash command of a session that builds against failing
  tests, and refuses one that changes a test. It reads the write tools and the shell alike: a
  redirect, an in place edit, a move, a copy, or a checkout of another revision. Reading a test is
  allowed, on purpose: a build that cannot read the test cannot tell a failing assertion from a
  broken one. It is off in every session but a worker of the build stage, because the stage before
  that one writes the tests. It is designed in
  [`hooks/test-gate/README.md`](../hooks/test-gate/README.md).
- **prose-gate.** Reads prose written for a person and refuses what Simplified Technical English
  refuses, for the part of it a program can measure: a sentence of more than 25 words, a paragraph of
  more than 6 sentences, the perfect and the continuous tenses, and a dash used as punctuation. It
  reads a markdown or a text file about to be written, and the prose a command carries as an
  argument, which is a pull request body, an issue body or a commit message. The approved vocabulary
  and the ban on idiom are not measurable and are not guessed at: they stay in a brief, and every
  refusal says so. It is designed in
  [`hooks/prose-gate/README.md`](../hooks/prose-gate/README.md).

The first five are seeded, so a fresh system is under them without anybody attaching anything. The
prose gate is offered rather than attached, because prose is what a role produces all day and the
rules it holds are a style somebody chooses. `krewe hook attach <workspace> prose-gate` is how a
workspace takes it.

A seeded hook used to mean a hook that cannot refuse. The merge gate refuses and is seeded anyway,
because it holds the boundary the whole shape of this system rests on: every role pushes and opens a
pull request, and no role merges, since a push applies nothing and a merge runs the pipeline that
spends money. A gate an operator has to remember to attach is off in every system nobody set up, which
is where the boundary matters most. So the rule is not that a seeded hook never refuses. It is that a
seeded hook refuses something no session is ever meant to do, exactly, and says what to do instead.
`krewe hook detach system merge-gate` is how somebody decides otherwise.

The deploy identity gate is seeded on that same rule. Opening a pull request that creates
infrastructure without saying whether the identity applying it may create anything hands the failure
to whoever merges, and the merge is the one step this system does not take back. It reads only the
command and the change, so it needs no credential, and it declares no binary, so no image can refuse
a task over it.

The process gate is seeded on it too. Ending a process this session did not start is never a
session's to do, and a signal is finished before the command returns, so there is no review step and
nothing to revert. The machine holding the sandboxes also holds the control plane, the store, the
broker and the operator's terminal. `KREWE_MAY_END_A_PROCESS` lifts it for a session the operator
starts with it set, and a command line that sets the variable itself is refused, because a session
that lifts its own gate has none.

The test gate is seeded on it as well, and it is the one that refuses least. The build stage hands a
worker a red suite and asks it to make the tests pass. The shortest way to a green suite is to change
the assertion. From inside the session a failing test looks exactly like a wrong test, and nothing
there tells the two apart.

The system sets `KREWE_BUILDING` on the task of a worker in that stage and on nothing else. Every
other session is refused nothing. A command line that sets the variable itself is refused, because a
session that decides its own boundary has none. The refusal names the file. It says why the file
reads as a test, and it says to answer that the test is wrong rather than change it.

### How a hook refuses

Exit code 2, with the reason on standard error, which the runtime hands to the session. The runtime
also takes a refusal as a document on standard output. The exit code is what this system uses, because
that contract is the older and simpler of the two, and a refusal a runtime does not understand is a
gate that quietly opens.

A hook fires on every command a session runs, so anything it cannot read exits 0 and lets the command
through. A gate that refuses what it does not understand refuses the work, and a broken hook must not
be able to stop a system.

### Still worth writing

- Refuse a `git commit` or a `git push` without approval, and name how to approve it.
- Refuse `git add .` and `git add -A`, and name the form that stages files by name.
- Refuse a force push and any history rewrite.
- Check the commit message format, and refuse an attribution line.

A hook that refuses wrongly blocks the work and costs the operator an interruption, which is worse
than no hook. A rule stays advice until its check is exact. That is why the merge gate reads a
command line the way a shell would rather than matching text: `git commit -m "merge the two lists"`
has to go through.

## What is not settled

Hooks, skills and documents are three instances of one shape: files the system holds, pinned to a
version, attached at a level, rendered into a sandbox. They do not share machinery today. Whether
they should is a real question and it is open, recorded here rather than answered by building the
third one the same way and calling it a pattern.

A hook is a compiled executable, so what it was written in is its own business and the sandbox does
not need a runtime for it. The one shipped here is Go, built to a static binary, which is why its
`binaries` list names only the commands it shells out to. That says what a hook may be written in. It
says nothing about where a hook lives.

The layout is the system's own and predates the Claude Code plugin format, which is
`.claude-plugin/plugin.json` beside a `hooks/hooks.json` that resolves paths through
`${CLAUDE_PLUGIN_ROOT}`. Whether to move onto it is open, and it is the same question as the one
above about skills, hooks and documents sharing machinery.
