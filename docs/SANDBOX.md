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

3. Say which model and image the stack runs, once, then start it:

   ```
   make config
   make up
   ```

   `make config` writes `~/.quay/env` from `deploy/env.example`, and the stack reads it on every
   command, so `make upgrade` cannot bring the stack back as something else. The two variables can still be given on the command line for a one off. Without
   either, the stack uses a lightweight image and an echo backend, which is what continuous integration
   runs, because it has no subscription.

4. Install the CLI, create a workspace, and give it the token:

   ```
   make install          # installs over the quay your shell runs
   quay workspace create demo
   quay project create house-bills
   quay secret set CLAUDE_CODE_OAUTH_TOKEN <token from step 1>
   ```

   Creating something moves you into it, so each line lands where the one above it left you, and
   `quay use` says where that is. The secret is scoped to the workspace, and a turn runs inside a
   project. The control plane reads the secret when running a turn and injects it into that
   session's sandbox; it is never part of the message or the event log.

5. Dispatch a turn and get a real reply:

   ```
   quay dispatch "say pong"
   ```

   You are already in `demo/house-bills`, so nothing needs saying twice. To reach somewhere else for
   one turn without moving, put the address first: `quay dispatch demo/gardening "order the bulbs"`.

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

`TestClaudeConversationSurvivesItsContainer` runs next to it, on the same two conditions. It tells the
model a number, destroys the container the conversation was running in, creates a new one for the same
session, and asks for the number back. Two turns of your subscription for the one claim that cannot be
made with a substitute.

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
session will not authenticate. Stopping the session and dispatching again gives it a fresh sandbox
that carries the token, and the conversation comes back with it, because the conversation is on the
host rather than in the container. Or reach the old container with the token on the command:

```
docker exec -it -e CLAUDE_CODE_OAUTH_TOKEN=<token> quaycrew-<session id> claude --resume <conversation id>
```

Pressing `s` instead gives you a shell in the same container. That shows you the room; attaching
shows you the conversation.

## Where a session's state lives

A sandbox is a container, and a container's filesystem is thrown away with it. So the two directories
that matter are mounted in from the host:

```
~/.quay/data/workspaces/<workspace>/claude                       ->  /home/agent/.claude
~/.quay/data/workspaces/<workspace>/projects/<project>/workspace  ->  /home/agent/workspace
```

The first is the model's own store: its settings, the transcripts `--resume` reads, and the
workspace's `CLAUDE.md`, shared by every project in that workspace. The second is one project's
working directory: its files and its own `CLAUDE.md`. Both are read write, and both survive the
container being replaced.

That is also how you give a project context. Write it with an editor:

```
echo "Supplier is Octopus, account 123." >> ~/.quay/data/workspaces/<workspace>/projects/<project>/workspace/CLAUDE.md
```

Every thread in that project reads it on the next turn, because the model already looks for
`CLAUDE.md` in its working directory. Nothing is prepended to your message and nothing is charged for
a turn that does not need it. An agent can also write these files, which is the trade for keeping the
conversation in the same place.

`QC_DATA_HOST` moves the directory somewhere else, for example a disk with more room:

```
QC_DATA_HOST=/Volumes/work/quaycrew make up
```

Compose gives the control plane its own view of that directory and tells it the host path as well,
because sandboxes are started on the host daemon and only host paths mean anything to it.
