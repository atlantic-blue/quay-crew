**A session can no longer end a running process.** A session runs with a shell, and nothing stopped
it signalling or tearing down something it did not start. The control plane, the database, the
message broker and the operator's terminal multiplexer all run on the same machine as the sandboxes,
and the state a person waits on lives in them. A signal is finished before the command returns, so
there is no review step, no revert and no partial application.

At 13:41 on 1 September 2026 the operator's terminal multiplexer server restarted. Every console pane
and every conversation pane closed in the same moment, and the build under one of them came back as
exit code 137. Nothing was lost, the cause was never traced, and no session was shown to have caused
it. The gate stands on the harm rather than on the blame: the panes were gone at once, and nothing
asked first.

[hooks/process-gate/](hooks/process-gate/) is the check. It reads each Bash command and refuses one
whose first word, at the start of the line or after a shell separator, ends something: `kill`,
`pkill` and `killall` in every signal form including the numeric one, the terminal multiplexer's
teardown verbs for the server, a session, a window and a pane, the container runtime's own ending
verbs, the two service manager equivalents, and the older screen program's quit form. A polite stop
is refused beside a rude one, because the difference is only how long the work has to notice.

This product's own verbs go through. `krewe job stop` and `krewe flow stop` end the work in the
record and signal nothing, and every refusal names them. The runtime still tears a sandbox down at
the end of its life, because the control plane does that from its own process, which runs no hook.

`KREWE_MAY_END_A_PROCESS` lifts it, and the operator sets it in the environment they start a session
with. A command line that sets the variable itself is refused: a session that can lift its own gate
has no gate.

A fresh system is under it, beside the merge gate and the deploy identity gate, on the same argument.
A gate an operator has to remember to attach is off in every system nobody set up. It declares no
binary and names no secret, so no image and no workspace can lose it.
`krewe hook detach system process-gate` is how somebody decides otherwise.

One trap, and it is in the hook's own page: the gate reads the text of the command, so a command that
merely writes about the gate is refused. Write that prose to a file with an editor rather than
through a shell string.
