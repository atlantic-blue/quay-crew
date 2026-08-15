# prompt-analyser

Reads the message a session was sent, asks a small model to restate it, and hands the session the
message and that restatement together. It never replaces what was typed: the runtime does not allow
that, and it should not, because a reading of a message is a guess and the words are not.

It is the first hook because it cannot be wrong in the expensive direction. Every other hook worth
having refuses something, and a hook that refuses wrongly blocks the work. This one only adds.

What it reads inside a sandbox differs from what the same hook reads on the operator's machine, which
is why the paths are configuration and not code:

- the skills at `/home/agent/skills`, which is where the crew mounts what a session holds
- what the session was told, at `/home/agent/.claude/CLAUDE.md`, which the crew renders

It fails open in every direction. A missing model, a timeout, an empty answer, a broken config file:
each one ends with the hook printing nothing and exiting 0, so a message always gets through.

`MAX_THINKING_TOKENS=0` is what makes it fast enough to run on every message. With extended thinking
left on, one analysis cost 3,855 thinking tokens and 42 seconds. With it off the same call takes about
1.5 seconds.
