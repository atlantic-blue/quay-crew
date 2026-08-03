# The default sandbox image for a session: an isolated environment with the Claude Code CLI in it.
#
# The control plane starts one container from this image per session and execs `claude` inside it.
# The image carries no credentials. The subscription token is injected at turn time as
# CLAUDE_CODE_OAUTH_TOKEN (minted by `claude setup-token`, stored as a per project secret), so the
# same image is safe to build, share, and run anywhere.
FROM node:22-slim

# git and ripgrep are what an agent reaches for most; ca-certificates so it can talk to the network.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git ripgrep tmux \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

# Run as a non-root user. The session's work lives under this user's home, which is thrown away with
# the container.
RUN useradd --create-home --shell /bin/bash agent
USER agent
WORKDIR /home/agent/workspace

# Get the first run out of the way.
#
# A turn is non interactive and skips all of this, but attaching to a conversation is interactive, and
# a sandbox is a fresh container every time. Without this the operator lands in the theme picker and
# then the workspace trust prompt instead of their conversation, which reads as "the token is not
# working" because nothing ever gets far enough to authenticate.
#
# The CLI rewrites this file as it runs; these are only the starting values.
# The terminal an open conversation runs in, so the operator can leave without ending it.
#
# Two prefixes, because this one is very often nested. The operator's own machine is full of tmux
# sessions, and opening a thread from inside one of them means ctrl-b is eaten by the outer server and
# never reaches this one. ctrl-o is not taken by anything out there, so it always lands here; ctrl-b
# stays as the second prefix because it is what the fingers already do when nothing is nested.
#
# The status bar is off: this is a way out of a conversation, not a terminal the operator is meant to
# manage, and a green bar across the bottom of somebody else's screen only asks questions.
RUN printf '%s\n' \
    'set -g prefix C-o' \
    'set -g prefix2 C-b' \
    'bind C-o send-prefix' \
    'set -g status off' \
    'set -g mouse on' \
    > /home/agent/.tmux.conf

RUN mkdir -p /home/agent/.claude \
    && printf '%s\n' '{"theme":"dark"}' > /home/agent/.claude/settings.json \
    && printf '%s\n' '{"hasCompletedOnboarding":true,"theme":"dark","projects":{"/home/agent/workspace":{"hasTrustDialogAccepted":true,"hasCompletedProjectOnboarding":true}}}' > /home/agent/.claude.json
