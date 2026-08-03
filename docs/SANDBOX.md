# The Claude sandbox

A session runs its turns inside a sandbox: an isolated container the control plane starts per
session. The default sandbox image carries the Claude Code CLI, and each turn runs `claude` inside
it. The image holds no credentials. The subscription token is injected at turn time as the
`CLAUDE_CODE_OAUTH_TOKEN` environment variable, stored per workspace as a secret, so the same image is
safe to build and run anywhere.

For why the control plane starts these containers on the host daemon, see the Sandboxes section of
`docs/ARCHITECTURE.md`.

## Run a real turn end to end

You need Docker and a Claude subscription.

1. Mint a long lived subscription token on your machine:

   ```
   claude setup-token
   ```

   This prints a token. Treat it like a password: it can spend your subscription.

2. Build the sandbox image:

   ```
   make sandbox-image
   ```

   This builds `quaycrew-sandbox-claude:local` from `deploy/sandbox/claude.Dockerfile` (Node, the
   Claude Code CLI, git, and ripgrep, running as a non-root user).

3. Start the stack pointed at that image and the real model backend:

   ```
   QC_SANDBOX_IMAGE=quaycrew-sandbox-claude:local QC_MODEL=claude-code make up
   ```

   Without these two variables the stack uses a lightweight image and an echo backend, which is what
   continuous integration runs (no subscription there).

4. Install the CLI, create a workspace, and give it the token:

   ```
   make install
   quay workspace create demo
   quay project create --workspace demo "house bills"
   quay secret set --workspace demo CLAUDE_CODE_OAUTH_TOKEN <token from step 1>
   ```

   `--workspace` takes the workspace id or its name, so you never need to copy the id around.
   The secret is scoped to the workspace, and a turn runs inside a project. The control plane reads it when running a turn and injects it
   into that session's sandbox; it is never part of the message or the event log.

5. Dispatch a turn and get a real reply:

   ```
   quay dispatch --project "house bills" "say pong"
   ```

   A new sandbox container (`quaycrew-<session id>`) starts on the first turn and is reused for the
   rest of the session. A second dispatch on the same thread continues the same conversation.

## The gated integration test

`internal/model/claudecode_integration_test.go` runs a real Claude turn inside the sandbox image and
checks a reply and a resumable session id come back. It needs a subscription, so it **skips** unless
both are present:

- `CLAUDE_CODE_OAUTH_TOKEN` is set (from `claude setup-token`), and
- the sandbox image exists (`make sandbox-image`).

Run it locally with:

```
make sandbox-image
CLAUDE_CODE_OAUTH_TOKEN=<token> go test -tags=integration -run TestClaudeCodeRunnerRealTurn ./internal/model/
```

Continuous integration has no subscription, so this test skips there. The token delivery mechanism
itself, that a value in the sandbox env reaches the process inside the container, is covered by
`TestDockerProviderDeliversEnv`, which needs only Docker and does run in continuous integration.

## Getting inside a conversation

`quay dispatch` runs one turn and returns. To sit inside the conversation, with its history, and keep
typing:

```
quay sessions
quay attach 5d013d07
```

or press `a` on a session in the console.

This runs `claude --resume <conversation id>` inside that session's sandbox, and needs nothing from
your shell. The control plane sets the workspace's environment on the sandbox when it creates it, so
everything started inside is already authenticated and no tool has to carry the token around.

The trade is that the token is readable for the life of that container, for example through
`docker inspect`. It was already reachable from inside the sandbox, which runs the model, so this
widens who can see it rather than whether it is there at all. The alternative, handing the value back
through the control plane API on request, was rejected: a secret the backend holds should not become
readable by any client that asks.

The image also ships past the CLI's first run: onboarding and the workspace trust prompt are marked
done. A turn is not interactive and never meets either, but attaching is, and a sandbox is a fresh
container every time, so without this the operator lands in a theme picker instead of their
conversation. It reads exactly like a broken token, because nothing gets far enough to authenticate.

One consequence to know: a token set **after** a session's first turn does not reach that session's
existing sandbox. Turns still work, because a turn also passes the environment, but attaching to that
session will not authenticate. Attaching to that session will not authenticate. Do **not** stop the session to fix it: stopping runs
`docker rm -f`, and the conversation transcript lives at `/home/agent/.claude/projects/` inside that
container, so stopping destroys the conversation you were trying to reach. Until the sandbox state
sits on a volume, reach an older session with the token passed on the command instead:

```
docker exec -it -e CLAUDE_CODE_OAUTH_TOKEN=<token> quaycrew-<session id> claude --resume <conversation id>
```

Pressing `s` instead gives you a shell in the same container. That shows you the room; attaching
shows you the conversation.
