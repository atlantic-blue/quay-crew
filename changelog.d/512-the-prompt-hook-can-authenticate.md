**The prompt hook can authenticate, and says why when it cannot.** Every message sent to a session in
a sandbox went unanalysed, and nothing anywhere said so. A real task on 30 August 2026 left one line
in a file in `/tmp`: `no answer  770ms`. The hook fired, started a small model to do the analysis, and
that child exited 1 in under a second with nothing to authenticate with.

Claude Code removes `CLAUDE_CODE_OAUTH_TOKEN` from the environment of every process a session starts,
by that name and no other. A hook is one of those processes. Two live processes in one sandbox showed
it: the task's own `claude` held the token, and a process that same `claude` started held
`AWS_ACCESS_KEY_ID`, `GH_TOKEN`, `QC_TOKEN` and `QC_GRPC_ADDR`, and not the token. Testing the hook by
hand with `docker exec` passed every time, because `docker exec` gives the container's own
environment, which does hold it. That is why it looked healthy for as long as it did.

So the crew writes the same value a second time, as `QUAY_MODEL_TOKEN`, beside `QC_TOKEN` and
`GH_TOKEN` which already survive. The hook hands its child the name the command line reads, from
whichever of the two carried a value, and a value a person set on their own machine wins. Nothing new
is stored: it is one value under two names, and no credential is ever written to a file, a log or a
settings file.

The other half is the silence. The hook still exits 0 whatever happens, because an exit code the
runtime reads as a refusal would block the message. It now writes the reason at the end of its last
run line rather than the single word `no answer`, so the record says which failure it was and what to
do about it. `no answer` reads the same whether the model was slow, said nothing, or was never logged
in, and it was the only sign anywhere that this hook had never once worked.
