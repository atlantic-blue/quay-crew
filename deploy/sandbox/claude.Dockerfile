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
# The terminal an open conversation runs in. The configuration is a file in the repository rather than
# a printf here, so a test can read the same thing the image ships.
COPY deploy/sandbox/tmux.conf /home/agent/.tmux.conf

# How a conversation is opened, and what happens when it ends. In the image rather than built into a
# command line so the shape of it is readable, and so a test can read the same thing the image ships.
# Executable at copy time, because everything after the image drops to the sandbox's own user and
# that user cannot chmod a file it does not own.
COPY --chmod=0755 deploy/sandbox/open-conversation.sh /usr/local/bin/open-conversation

RUN mkdir -p /home/agent/.claude \
    && printf '%s\n' '{"theme":"dark"}' > /home/agent/.claude/settings.json \
    && printf '%s\n' '{"hasCompletedOnboarding":true,"theme":"dark","projects":{"/home/agent/workspace":{"hasTrustDialogAccepted":true,"hasCompletedProjectOnboarding":true}}}' > /home/agent/.claude.json
