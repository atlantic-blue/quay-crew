# process-gate

Refuses a command that signals or tears down a running process.

A session runs with a shell. Until this hook, nothing stopped it ending a process it did not start.
The control plane, the database, the message broker and the operator's terminal multiplexer all run
on the same machine as the sandboxes, and the state a person waits on lives in them.

At 13:41 on 1 September 2026 the operator's terminal multiplexer server restarted. Every console pane
and every conversation pane it held closed in the same moment, and the build under one of them came
back as exit code 137. The containers and the recorded sessions survived, so nothing was lost. The
cause was never traced, and no session was shown to have caused it. This gate stands on the harm
rather than on the blame: the panes were gone at once, and nothing asked first.

A signal is finished before the command returns. There is no review step, no revert and no partial
application, and everything under the target dies with it. That is the whole argument for a gate.

## What it refuses

It fires on `PreToolUse` for `Bash`, reads the command, and refuses it when a program that ends
things is the first word of any command on the line. At the start of the line, or after a semicolon,
a pipe, an `&&`, a substitution, a shell of its own, or a `do` in the middle of a loop.

- **`kill`, `pkill` and `killall`, in every signal form**, including the numeric one. `kill 4213`,
  `kill -9`, `kill -SIGTERM`, `kill -s KILL`. The signal decides how the target dies, never whether
  it dies, so reading the signal would only tell a session which spelling to try next.
- **The terminal multiplexer's teardown verbs**: the server, a session, a window and a pane. Any
  `tmux` verb beginning with `kill`, because tmux accepts an unambiguous abbreviation of its own
  commands, so `tmux kill-ses` is `tmux kill-session`.
- **The container runtime's own ending verbs**: `docker kill`, `docker stop`, `docker rm -f`,
  `docker compose down` and `docker system prune`. Also the compose file's `stop` and `kill`, the
  same verbs under `docker container`, any other `prune`, the separate `docker-compose` command, and
  `podman` under all of the above.
- **The two service manager equivalents**: `systemctl stop` and `systemctl kill`.
- **The older screen program's quit form**: `screen -X quit`, and its `kill`.
- **A signal sent into another container.** `docker exec <name> kill 1` ends a process inside the
  container that holds it, and the services on this machine are containers, so the rest of that line
  is read as the command line it is. `docker exec <name> ls` goes through.

**A polite stop is refused beside a rude one.** `docker stop` sends a signal and waits ten seconds.
`kill -9` waits for nothing. The difference is how long the work has to notice, and both end it, so
both are the operator's to decide. A gate that refused only the rude one would be a gate every
session walks around by being polite.

Reading is never refused. `docker ps`, `docker inspect`, `tmux list-sessions`, `systemctl status`,
`ps aux` and a grep for any of these words all go through.

## What it does not refuse

**This product's own verbs.** `krewe job stop` and `krewe flow stop` end the work in the record. They
signal nothing: the control plane closes the sandbox itself, in its own process, and writes down what
happened. That is the way through, and every refusal says so.

**The runtime tearing down a sandbox at the end of its life.** The control plane removes a container
from its own process, which runs no hook, so this gate never sees it and cannot stop it. That is the
shape rather than an exception in the table, and the difference matters: a `docker rm -f` typed in a
session **is** refused, even when it names a sandbox, because a session is not the control plane and
the container it names is another session's work.

**A tool that is not Bash.** The gate is bound to `Bash`, because that is where a session runs a
shell command.

**Anything, if the operator takes the hook off.** `krewe hook detach system process-gate` is how
somebody decides this system may end processes. That is the honest shape of it: the boundary is a
thing the system holds and an operator can remove deliberately, rather than a sentence a model may or
may not keep.

## The way through, for one command

`KREWE_MAY_END_A_PROCESS` lifts the gate. Set it to anything, and every command in that session goes
through.

It is read from the environment the session's runtime was started in, and from nowhere else. It is
never set in advance and never set in an image.

**A command line that sets it is refused**, whatever else that line does. `KREWE_MAY_END_A_PROCESS=1
kill -9 4213` is refused, and so is an `export` of it, and so is the same thing inside `bash -c`. A
session that can set the variable can lift its own gate, and a gate a session lifts is advice with
extra steps. So the lift is the operator's, in the environment they start a session with.

## The trap, which is real and will happen to you

The gate reads the text of the command, so **any command that merely writes about the gate is
refused**. A heredoc that carries this page has `kill -9` at the start of a line inside it, and the
reader cannot tell that line from a command. The command that adds this documentation is refused by
the documentation it adds.

The same thing happens with the tool's own flags gate. `krewe` takes no flags, so typing
`krewe version --force` is refused for the word `--force`, whether you meant it as a flag or as the
subject of a sentence.

The fix, both times: **write the prose to a file with an editor rather than through a shell string.**
The Write and Edit tools send the text as a payload, and this gate is bound to `Bash` alone.

Never reword a rule to get past its own guard. A rule that says `kill` and had to be rewritten to say
something else is a weaker rule, and the next reader will not know why it reads that way.

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
`git commit -m "the console no longer kills the pane it drew"`, and a refusal that is wrong costs the
operator an interruption, which is worse than no gate. Quoting is what tells those apart: the words
of a quoted argument are one token, and a token is never a command.

## Seeded, not offered

A fresh system is under it, beside the merge gate and the deploy identity gate, on the same argument:
a gate an operator has to remember to attach is off in every system nobody set up, which is where the
boundary matters most. It refuses one class of thing, no session here is meant to do that thing, and
the refusal names what to do instead. It declares no binary and names no secret, so no image and no
workspace can lose it.

## What it does not hold

**A process ended by something other than a command.** A program that calls the system's own
interface to send a signal is not a command line, and this gate reads command lines.

**A container runtime this table does not name.** `docker` and `podman` by name, with their compose
commands. Something else with the same verbs goes through, and that is a gap rather than a decision.

**A process ended from inside another program.** A shell script on disk, run by name, is one word to
this gate. It reads what the session typed, not what the file says. The same holds one level in:
`docker exec <name> sh -c "kill 1"` hands the shell a string this reader does not open, while the
direct form is refused.

## Building it

    make hooks

The entry point is `bin/hook`, and it is built rather than committed. One committed binary runs on
one processor type, and this image builds on both arm and amd machines. This is its own Go module and
needs the standard library only, so `go test ./...` at the root of the repository does not reach it.
`make test` runs it by name.
