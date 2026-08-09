# The default sandbox image for a session: an isolated environment with the Claude Code CLI in it.
#
# The control plane starts one container from this image per session and execs `claude` inside it.
# The image carries no credentials. The subscription token is injected at turn time as
# CLAUDE_CODE_OAUTH_TOKEN (minted by `claude setup-token`, stored as a per project secret), so the
# same image is safe to build, share, and run anywhere.
# quay itself, so a session can drive the crew from inside its sandbox. Built here rather than mounted
# from the host, because the host's binary is built for the host: a darwin build does not run in a
# linux container, and the cloud has no host to mount from at all.
FROM golang:1.25 AS tool
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Which build this image was made from. The tool inside a sandbox reports it, and the image carries
# it as a label so the crew can say when its sandboxes are running something older than itself.
ARG QC_VERSION=unknown
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$QC_VERSION" -o /out/quay ./cmd/quay

FROM node:22-slim

# Read by the control plane through `docker image inspect`, which is how a stale image is noticed at
# all: sessions run whatever this image holds, and an image from an older build holds an older quay,
# or none.
ARG QC_VERSION=unknown
LABEL com.quaycrew.build=$QC_VERSION

# git and ripgrep are what an agent reaches for most; ca-certificates so it can talk to the network.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git ripgrep tmux \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

# Reaching the control plane is a separate decision, made once in configuration: without a network
# that can reach it and an address to dial, this is a command that says it cannot connect.
COPY --from=tool /out/quay /usr/local/bin/quay

# A session clones in conversation, so git has to find its credential itself. The helper reads
# GH_TOKEN from the environment at the moment git asks, which keeps the token out of every argument
# list and every transcript. A file in the repository rather than a printf here, so a test can read
# the same thing the image ships. Registered as system configuration while this stage still runs as
# root; the sandbox user cannot write it.
COPY --chmod=0755 deploy/sandbox/git-credential-env /usr/local/bin/git-credential-env
RUN git config --system credential.helper /usr/local/bin/git-credential-env

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
