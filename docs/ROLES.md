# Roles

A role is a named way of working a session is given: a brief the model reads, the model it runs on,
and the material it is allowed to receive.

The design is [`quay-crew#354`](https://github.com/atlantic-blue/quay-crew/issues/354). This page is
what has shipped of it.

## What is built today

A role is imported, pinned to a version, and attached at a level. That is the whole of it. Nothing
runs as a role yet, so attaching one changes what the crew may be asked for later and changes nothing
about a session already open.

```
quay role import <directory>
quay role list [<workspace>]
quay role attach [<workspace>|crew] <name>
quay role detach [<workspace>|crew] <name>
```

## Why the boundary, not the persona

A flow sends work to one session, and every step lands in the same conversation. The session that
writes the code has already read everything the session that planned it said, so it agrees with the
plan. A second opinion that read the first opinion is not a second opinion.

The fix is not a personality. It is what the session is given. A role that writes the tests must not
receive the code. A role that writes the code may receive the test names and not the test bodies. So
`receives` is a declaration the crew can hold a session to, rather than a paragraph in a brief asking
the model nicely.

## What a role declares

A role is a directory holding two files, the shape a skill and a hook already have.

`role.yaml` says what it is:

```yaml
name: test-writer
version: 1
summary: writes the tests for a piece of work, from the work alone
model: opus
receives:
  - work
  - context
```

`ROLE.md` is the brief. It is the whole instruction of a session running as this role.

`version` exists so a session is pinned to the version it started with. Editing a role in its
repository makes a new version, so it cannot change under a session already running as it. A
workspace moves by attaching again.

`model` is a tier such as `opus` or a full name such as `claude-opus-5`. It is declared per role
because the work differs: naming a team is worth the larger model and writing one file to a
specification is not. The crew does not check the name against a list, because a tier the model's own
tool stops accepting would otherwise fail every task with nothing an operator could configure around
it.

`receives` is the boundary. It is one of three words today, and a fourth is refused at import by
name:

- `work` is the piece of work the role was given. Every role receives it, because a session with no
  work to do is not a task.
- `context` is what the crew, the workspace and the project know, as the memory files carry it.
- `skills` are the skills the workspace holds.

Three, because those are what the crew puts in front of a session today. A word for material nobody
assembles yet would be a boundary that means nothing, and a boundary that means nothing looks exactly
like one that holds.

## How long a brief may be

A brief may be 16,384 bytes, which is about four pages. A skill's brief may be 4,096.

The difference is who pays. A skill's summary reaches every session on every conversation, so it is
held to a sentence, and the measurement behind that is in [`SKILLS.md`](SKILLS.md): one crew reached
51,727 bytes of context per session. A role's brief reaches one session, once, and that session
exists to do this one job. The ceiling is still here because a brief nobody reads to the end is a
brief nobody follows.

## The two levels

A role attaches at the crew or at one workspace, which is the outer two of the four levels context
has. Skills stop in the same place, and nothing has wanted the inner two yet.

`quay role attach crew <name>` gives it to every workspace, including the ones made after today.
`quay role attach <workspace> <name>` gives it to one. The two are separate statements: taking a role
off the crew leaves a workspace's own attachment alone.

## What is not built

- No session runs as a role. Nothing reads `receives` at dispatch yet.
- No product manager role, so nothing chooses a team.
- No sub tasks, so nothing runs one session per role.
- No event starts a flow.

The scenarios that hold up what is built are in
[`features/roles.feature`](../features/roles.feature). If a behaviour is not there, it is not built.
