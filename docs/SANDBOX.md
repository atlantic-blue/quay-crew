# The Claude sandbox

A session runs its tasks inside a sandbox: an isolated container the control plane starts per
session. The default sandbox image carries the Claude Code CLI, and each task runs `claude` inside
it. The image holds no credentials. The subscription token is injected at task time as the
`CLAUDE_CODE_OAUTH_TOKEN` environment variable, stored per workspace as a secret, so the same image is
safe to build and run anywhere.

For why the control plane starts these containers on the host daemon, see the Sandboxes section of
`docs/ARCHITECTURE.md`.

## Run a real task end to end

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

   This builds `quaycrew-sandbox-claude:local` from `deploy/sandbox/claude.Dockerfile` (Node, Go,
   the Claude Code CLI, git, and ripgrep, running as a non-root user).

3. Say which model and image the stack runs, once, then start it:

   ```
   make config
   make up
   ```

   `make config` writes `~/.quay/env` from `deploy/env.example`, and the stack reads it on every
   command, so `make upgrade` cannot bring the stack back as something else. The two variables can still be given on the command line for a one off. Without
   either, the stack uses a lightweight image and an echo backend, which is what continuous integration
   runs, because it has no subscription.

   Three keys decide what a task is. `QC_MODEL` is the backend: `claude-code` runs the real thing on
   your subscription, `echo` runs `echo` in the sandbox instead. `QC_CLAUDE_MODEL` is which model that
   backend runs against, either an alias for the newest of a tier (`opus`, `sonnet`) or a full name
   (`claude-opus-5`, which is what a crew gets when it says nothing). `QC_SANDBOX_IMAGE` is the
   container it all runs in.

   Say nothing about the model and the command line tool chooses for itself, and it chooses Sonnet.
   That is worth knowing, because a crew configured for Claude Code, holding an Opus subscription,
   was running every session on Sonnet and nothing anywhere said so.

4. Install the CLI, create a workspace, and give it the token:

   ```
   make install          # installs over the quay your shell runs
   quay workspace create demo
   quay project create house-bills
   quay secret set CLAUDE_CODE_OAUTH_TOKEN <token from step 1>
   ```

   Creating something moves you into it, so each line lands where the one above it left you, and
   `quay use` says where that is. The secret is scoped to the workspace, and a task runs inside a
   project. The control plane reads the secret when running a task and injects it into that
   session's sandbox; it is never part of the message or the event log.

5. Dispatch a task and get a real reply:

   ```
   quay dispatch "say pong"
   ```

   You are already in `demo/house-bills`, so nothing needs saying twice. To reach somewhere else for
   one task without moving, put the address first: `quay dispatch demo/gardening "order the bulbs"`.

   A new sandbox container (`quaycrew-<session id>`) starts on the first task and is reused for the
   rest of the session. A second dispatch on the same session continues the same conversation.

## The gated integration test

`internal/model/claudecode_integration_test.go` runs a real Claude task inside the sandbox image and
checks a reply and a resumable session id come back. It needs a subscription, so it **skips** unless
both are present:

- `CLAUDE_CODE_OAUTH_TOKEN` is set (from `claude setup-token`), and
- the sandbox image exists (`make sandbox-image`).

Run it locally with:

```
make sandbox-image
CLAUDE_CODE_OAUTH_TOKEN=<token> go test -tags=integration -run TestClaudeCodeRunnerRealTask ./internal/model/
```

`TestClaudeConversationSurvivesItsContainer` runs next to it, on the same two conditions. It tells the
model a number, destroys the container the conversation was running in, creates a new one for the same
session, and asks for the number back. Two tasks of your subscription for the one claim that cannot be
made with a substitute.

Continuous integration has no subscription, so this test skips there. The token delivery mechanism
itself, that a value in the sandbox env reaches the process inside the container, is covered by
`TestDockerProviderDeliversEnv`, which needs only Docker and does run in continuous integration.

## Getting inside a conversation

`quay dispatch` runs one task and returns. To sit inside the conversation, with its history, and keep
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
done. A task is not interactive and never meets either, but attaching is, and a sandbox is a fresh
container every time, so without this the operator lands in a theme picker instead of their
conversation. It reads exactly like a broken token, because nothing gets far enough to authenticate.

One consequence to know: a token set **after** a session's first task does not reach that session's
existing sandbox. Tasks still work, because a task also passes the environment, but attaching to that
session will not authenticate. Stopping the session and dispatching again gives it a fresh sandbox
that carries the token, and the conversation comes back with it, because the conversation is on the
host rather than in the container. Or reach the old container with the token on the command:

```
docker exec -it -e CLAUDE_CODE_OAUTH_TOKEN=<token> quaycrew-<session id> claude --resume <conversation id>
```

Pressing `s` instead gives you a shell in the same container. That shows you the room; attaching
shows you the conversation.

## What the image pins

Every tool the image installs names its version, as a build argument at the top of the step that
installs it: `CLAUDE_CODE_VERSION`, `GH_VERSION`, `TF_VERSION`, `AWS_CLI_VERSION`. Raising one is an
edit to `deploy/sandbox/claude.Dockerfile` with a commit behind it, so the same commit builds the
same image on any day and a session that starts behaving differently has a change to point at.

Two tests hold this. `TestNothingInTheSandboxImageFloats` refuses any global install in the image
that does not name a version, so the next tool added unpinned fails there rather than months later.
`TestTheImageRunsTheClaudeCodeItPins` asks the built image what it runs, because a pin the registry
quietly ignores reads exactly like a pin that works.

## A credential that is a file

Some credentials are not values. A git configuration, a private key, a cloud credentials file: a tool
opens each one by path, so there is nothing an environment variable can do for them.

So a secret says how it reaches a sandbox, which is the shape Kubernetes and Docker both settled on.
The store holds bytes under a name and knows nothing else about them; whether those bytes become an
environment variable or a file is a separate choice. A Kubernetes Secret is read through
`secretKeyRef` or mounted through a `secret` volume; a Docker secret is given a target and lands
under `/run/secrets`. Neither writes the presentation into the store.

`quay secret set` is the environment. `quay secret mount` is a file:

```
quay secret mount gitconfig ~/.gitconfig
cat ~/.gitconfig | quay secret mount gitconfig
```

It lands at `/run/secrets/<name>`, one file per secret, and `quay secret list` says so. The directory
is created with the container and is memory backed, owned by the sandbox user and shut to everybody
else, so a mounted value never reaches the container's writable layer or the host's disk.

A mounted secret is **not** also in the environment, and that is the second reason to mount one. The
trade recorded above for the subscription token is real: a container's environment is readable for
the life of that container, for example through `docker inspect`. A file in a memory backed directory
is not.

Two things to know. The value is written when the sandbox is made, so a session already running was
made before you mounted it: stop the session to get one that has it. And `quay secret set` trims,
because a token gains a newline from the tool that printed it, while `quay secret mount` does not,
because a file's bytes are the file.

## Who a session commits as

A session commits as you, and it has to be told who that is. Mount your own git configuration:

```
quay secret mount <workspace> gitconfig ~/.gitconfig
```

The image ships a git configuration holding one line, `[include] path = /run/secrets/gitconfig`, so
what you mount reaches every git process in the sandbox: your identity, your aliases, your settings,
from any shell rather than only the process a task runs in. A workspace that mounts nothing is
unchanged, because git ignores an include that is not there.

Signing is the one part the crew decides rather than you, and it has to. Most configurations that
sign have it on for everything, against a key your machine holds and a container does not, so left
alone it fails every commit a session makes. A workspace that mounts a signing key signs with it; a
workspace that mounts none is told not to sign. The crew writes its answer after the include, and git
takes the last value it reads.

```
quay secret mount <workspace> GIT_SSH_SIGNING_KEY ~/.ssh/id_ed25519
```

Mounted, not set. Setting it is refused, and the refusal says this instead. A private key in the
environment is readable through `docker inspect` for the life of the container, which is the exposure
mounting exists to avoid, and the key is the most sensitive thing this crew carries. Put the public
half on the account you push to as a signing key, alongside the one your own machine signs with: a
commit signed in a sandbox is signed by a different key from one signed on your laptop.

## Where a session's state lives

A sandbox is a container, and a container's filesystem is thrown away with it. So the three
directories that matter are mounted in from the host:

```
~/.quay/data/workspaces/<workspace>/claude    ->  /home/agent/.claude
~/.quay/data/workspaces/<workspace>/volume    ->  /home/agent/shared
~/.quay/data/workspaces/<workspace>/projects/<project>/sessions/<session>/workspace
                                              ->  /home/agent/workspace
```

The first is the model's own store: its settings, the transcripts `--resume` reads, and the
`CLAUDE.md` every session in that workspace reads. The second is the workspace's volume, shared by
every session in it, which is where something one session writes is there for the next one. The
third is one session's own working directory: its files and its own `CLAUDE.md`. A working directory
belongs to a session rather than to a project, because two conversations sharing one would each be
changing files under the other. All three are read write, and all three survive the container being
replaced.

That is also how you give a session context. Write it with an editor:

```
echo "Supplier is Octopus, account 123." >> ~/.quay/data/workspaces/<workspace>/projects/<project>/sessions/<session>/workspace/CLAUDE.md
```

That session reads it on the next task, because the model already looks for `CLAUDE.md` in its
working directory. Nothing is prepended to your message and nothing is charged for a task that does
not need it. An agent can also write these files, which is the trade for keeping the conversation in
the same place. `quay context set` is the same thing said as a command, and it holds what it writes
in the store, so a level survives a session being replaced. See
[`docs/WORKSPACE.md`](WORKSPACE.md).

`QC_DATA_HOST` moves the directory somewhere else, for example a disk with more room:

```
QC_DATA_HOST=/Volumes/work/quaycrew make up
```

Compose gives the control plane its own view of that directory and tells it the host path as well,
because sandboxes are started on the host daemon and only host paths mean anything to it.
