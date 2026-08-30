# merge-gate

Refuses the command that merges. It is the first hook in this system that refuses anything.

Every role brief here ends a slice the same way: commit, push the branch, open a pull request, and
never merge. A push applies nothing. A merge runs the pipeline, and the pipeline is what spends money
and changes infrastructure, so the merge is the operator's gate.

Until this hook, that gate was a sentence in a brief. What a role `may` do is a list of the verbs a
session calls on the system, and merging is not one of them: it is a github action a session takes with
a credential a skill gave it. So the one boundary the whole shape rests on was the one thing nothing
checked, while smaller boundaries were held by a credential. This is the check.

## What it refuses

It fires on `PreToolUse` for `Bash`, reads the command, and refuses it if it merges:

- `gh pr merge`, however it is spelled, including after another command, under `sudo`, inside
  `bash -c`, inside a substitution, and with a repository named first.
- The same merge asked for over the interface underneath that command:
  `repos/<owner>/<repo>/pulls/<number>/merge` written to with `gh api`, with `curl` or with `wget`,
  and the `mergePullRequest` mutation through `gh api graphql`. A gate that knows one spelling is a
  gate the next spelling walks through.
- `git push` onto `main` or `master`. Landing a commit on the branch a pull request merges into runs
  the same pipeline, so a gate that refuses the button and leaves the command is not a gate.

Reading is never refused. `gh pr view`, `gh pr list`, `gh pr checks`, and a `GET` of the merge
endpoint, which asks whether a pull request is merged rather than merging it, all go through.

## What it does not refuse

**A default branch called something else.** The two names it knows are `main` and `master`. A
repository whose default branch is called `trunk` can be pushed to directly, and this is a gap rather
than a decision: reading the real default branch means asking the remote, and a hook that makes a
network call is a hook on the critical path of every command a session runs.

**A bare `git push` from a checkout that is already on the default branch.** The gate reads the
command, not the repository, so it cannot see which branch you are standing on. Refusing every
`git push` with no arguments would refuse the push every role makes on every slice, which is the
trade this hook exists on the right side of.

**A merge from anywhere that is not the Bash tool.** The gate is bound to `Bash`, because that is
where a session runs `gh`. A tool that reaches github another way is not covered.

**Anything, if the operator takes the hook off.** `quay hook detach system merge-gate` is how somebody
decides this system may merge. That is the honest shape of it: the boundary is now a thing the system
holds and an operator can remove deliberately, rather than a sentence a model may or may not keep.

## How it refuses

Exit code 2, with the reason on standard error, which the runtime hands to the session. The runtime
also takes a refusal as a document on standard output; this one uses the exit code because that
contract is the older and simpler of the two, and a refusal a runtime does not understand is a gate
that quietly opens.

Everything else exits 0, including a payload it cannot read. It fires on every Bash command every
session runs, so a gate that refuses what it does not understand refuses the work, and a broken hook
must not be able to stop a system.

## Reading a command line

It reads the command the way a shell would, far enough to know which programs run and with what
arguments, and no further. Nothing here expands a variable, resolves a glob or runs anything.

Reading it at all is the point. A gate that matched text would refuse
`git commit -m "merge the two lists"`, and a refusal that is wrong costs the operator an
interruption, which is worse than no gate. Quoting is what tells those apart: the words of a quoted
argument are one token and can never be a command.

## Building it

    make hooks

The entry point is `bin/hook`, and it is built rather than committed, for the reasons the analyser's
README gives. This is its own Go module and needs the standard library only, so `go test ./...` at
the root of the repository does not reach it. `make test` runs it by name.
