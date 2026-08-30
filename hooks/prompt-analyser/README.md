# prompt-analyser

Reads the message a session was sent, asks a small model to restate it, and hands the session the
message and that restatement together. It never replaces what was typed: the runtime does not allow
that, and it should not, because a reading of a message is a guess and the words are not.

It is the first hook because it cannot be wrong in the expensive direction. Every other hook worth
having refuses something, and a hook that refuses wrongly blocks the work. This one only adds.

## Building it

    make hooks

The entry point is `bin/hook`, and it is built rather than committed. A hook is an executable, an
executable is a build artifact, and one committed binary runs on one processor type while this
repository's image is built on both arm and amd machines. The image build runs the same command, so
the binary a sandbox mounts is built for the machine it runs on.

This is its own Go module. A hook is a plugin: a thing somebody reviews, versions and hands to
another system, so it does not share the system's dependencies and cannot import its internals. It needs
the standard library and nothing else, which is why `go.mod` has no requirements.

Because it is a separate module, `go test ./...` at the root of the repository does not reach it.
`make test` runs it by name.

## What it reads

What it reads inside a sandbox differs from what the same hook reads on the operator's machine, which
is why the paths are configuration and not code:

- the skills at `/home/agent/skills`, which is where the system mounts what a session holds
- what the session was told, at `/home/agent/.claude/CLAUDE.md`, which the system renders

`hook.config.json` is found beside the running binary rather than in the working directory, because
the runtime runs the hook from wherever the session happens to be.

## When it stays quiet

It fails open in every direction. A missing model, a timeout, an empty answer, a broken config file:
each one ends with the hook printing nothing or a single line for the terminal, and exiting 0, so a
message always gets through.

`CLAUDE_PROMPT_ANALYSER_DEBUG=1` reports what was sent, what came back, and how long it took.
`lastRunFile` in the config is overwritten with one line per run, which is how you tell a hook that
fired and stayed quiet from a hook that never fired at all.

`MAX_THINKING_TOKENS=0` is what makes it fast enough to run on every message. With extended thinking
left on, one analysis cost 3,855 thinking tokens and 42 seconds. With it off the same call takes
about 1.5 seconds.
