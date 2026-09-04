# The default sandbox image for a session: an isolated environment with the Claude Code CLI in it.
#
# The control plane starts one container from this image per session and execs `claude` inside it.
# The image carries no credentials. The subscription token is injected at exec time as
# CLAUDE_CODE_OAUTH_TOKEN (minted by `claude setup-token`, stored as a per project secret), so the
# same image is safe to build, share, and run anywhere.
# krewe itself, so a session can drive the system from inside its sandbox. Built here rather than mounted
# from the host, because the host's binary is built for the host: a darwin build does not run in a
# linux container, and the cloud has no host to mount from at all.
FROM golang:1.25 AS tool
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Which build this image was made from. The tool inside a sandbox reports it, and the image carries
# it as a label so the system can say when its sandboxes are running something older than itself.
ARG QC_VERSION=unknown
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$QC_VERSION" -o /out/krewe ./cmd/krewe

# The name the tool used to have, which refuses and says to type krewe. A session carries whatever it
# was told yesterday, and a role brief or a note written before the rename still says quay, so the
# refusal has to be in the sandbox as well as on the operator's machine.
RUN CGO_ENABLED=0 go build -o /out/quay ./cmd/quay

FROM node:22-slim

# Read by the control plane through `docker image inspect`, which is how a stale image is noticed at
# all: sessions run whatever this image holds, and an image from an older build holds an older krewe,
# or none.
ARG QC_VERSION=unknown
LABEL com.quaycrew.build=$QC_VERSION

# git and ripgrep are what an agent reaches for most; ca-certificates so it can talk to the network;
# curl because the api skills speak plain https and the gh install below needs it anyway.
#
# openssh-client is here for ssh-keygen, not for ssh. Git signs an ssh format signature by running
# ssh-keygen, so without this package a session with a signing key configured cannot commit at all:
# git fails with "cannot run ssh-keygen" before it reads the key.
#
# gnupg is the same thing for the other signature format. Git makes an OpenPGP signature by running
# gpg, so a workspace that mounts a gpg key gets "cannot run gpg" without it, which is the failure
# an earlier measurement recorded.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git gnupg openssh-client ripgrep tmux \
    && rm -rf /var/lib/apt/lists/*

# Where gpg keeps its keyring, which is a memory backed directory rather than the home one.
#
# A mounted key never reaches the disk: it lands in a memory backed /run/secrets and the container's
# writable layer never sees it. Importing it into the default keyring would undo that, because
# ~/.gnupg is on the writable layer and the daemon keeps that until the container is removed. /dev/shm
# is memory, per container, and gone with it.
#
# Set here rather than at sandbox birth because git runs gpg itself, in whatever environment the
# container gives it, and an attached terminal is a third process again. One value every process in
# the sandbox reads is the only shape that holds.
ENV GNUPGHOME=/dev/shm/gnupg

# gh, for the github skill. A pinned release rather than an apt repository, so the image builds the
# same binary everywhere and needs no keyring; the architecture is asked of dpkg because this image
# is built on both arm and amd machines.
ARG GH_VERSION=2.63.2
RUN arch="$(dpkg --print-architecture)" \
    && curl -fsSL -o /tmp/gh.deb "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${arch}.deb" \
    && dpkg -i /tmp/gh.deb \
    && rm /tmp/gh.deb

# terraform, for the terraform skill. Pinned the same way gh is, and unzip only lives long enough
# to unpack it. The stated cost is about ninety megabytes, the price of the skill existing at all;
# one image for now, revisited when sandbox tiers give skills their own.
ARG TF_VERSION=1.10.5
RUN arch="$(dpkg --print-architecture)" \
    && apt-get update && apt-get install -y --no-install-recommends unzip \
    && curl -fsSL -o /tmp/terraform.zip "https://releases.hashicorp.com/terraform/${TF_VERSION}/terraform_${TF_VERSION}_linux_${arch}.zip" \
    && unzip -q /tmp/terraform.zip -d /usr/local/bin \
    && rm /tmp/terraform.zip \
    && apt-get remove -y unzip && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

# The AWS command line, for the aws skill. Pinned like gh and terraform; the machine architecture
# is asked of uname because Amazon names its bundles x86_64 and aarch64 rather than amd64 and
# arm64. The stated cost is about a hundred and thirty megabytes, the heaviest thing a skill has
# asked of this image; one image for now, revisited when sandbox tiers give skills their own.
ARG AWS_CLI_VERSION=2.22.0
RUN arch="$(uname -m)" \
    && apt-get update && apt-get install -y --no-install-recommends unzip \
    && curl -fsSL -o /tmp/awscli.zip "https://awscli.amazonaws.com/awscli-exe-linux-${arch}-${AWS_CLI_VERSION}.zip" \
    && unzip -q /tmp/awscli.zip -d /tmp \
    && /tmp/aws/install \
    && rm -rf /tmp/awscli.zip /tmp/aws \
    && apt-get remove -y unzip && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

# Go, so a session can build and test the repository it is sitting inside: quay-crew is Go only,
# and `make fmt`, `make lint` and `go test` all need the toolchain. Copied from the stage that
# already built krewe rather than downloaded again, so the sandbox never carries a Go that
# disagrees with the one krewe itself was built with.
COPY --from=tool /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# The model runtime a session runs in, pinned like gh, terraform and the AWS command line are. It was
# the one thing here taken at whatever npm called latest on the day, so two builds of the same commit
# produced two different images and nothing recorded which. Raise this deliberately.
ARG CLAUDE_CODE_VERSION=2.1.233
RUN npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"

# A browser, so a session can look at what it built rather than delivering it on a passing build.
#
# Playwright rather than a bare chromium, because what a session needs to see is the whole page, at a
# viewport it chooses, in the colour scheme it chooses, after the page's own scripts have run. Pinned
# like gh, terraform and the AWS command line are.
#
# --only-shell takes the headless shell on its own, 340 megabytes against 641 for the full browser,
# and it is what gets run anyway: with nothing installed, `playwright screenshot` asks for
# chromium_headless_shell/chrome-linux/headless_shell by name. ffmpeg arrives beside it at 3.3
# megabytes and there is no way to decline it.
#
# The dependencies go first and the browser second, on purpose. They are 31 apt packages, fontconfig
# and seven font families among them, and without them the browser exits on
# "SkFontMgr_FontConfigInterface: Not implemented" rather than rendering anything. The browser
# download checks the host after it unpacks, so in this order a missing library fails the build here
# instead of failing a session months later.
#
# The browsers land in a directory of their own rather than under root's home, because a session runs
# as agent and a browser only root can read is a browser this image does not have. The stated cost is
# about 450 megabytes.
ARG PLAYWRIGHT_VERSION=1.62.1
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright
RUN npm install -g "playwright@${PLAYWRIGHT_VERSION}" \
    && apt-get update \
    && playwright install-deps chromium \
    && playwright install --only-shell --no-progress chromium \
    && chmod -R a+rX "$PLAYWRIGHT_BROWSERS_PATH" \
    && rm -rf /var/lib/apt/lists/*

# Reaching the control plane is a separate decision, made once in configuration: without a network
# that can reach it and an address to dial, this is a command that says it cannot connect.
COPY --from=tool /out/krewe /usr/local/bin/krewe
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

# The terminal an open conversation runs in. The configuration is a file in the repository rather than
# a printf here, so a test can read the same thing the image ships.
COPY deploy/sandbox/tmux.conf /home/agent/.tmux.conf

# How a conversation is opened, and what happens when it ends. In the image rather than built into a
# command line so the shape of it is readable, and so a test can read the same thing the image ships.
# Executable at copy time, because everything after the image drops to the sandbox's own user and
# that user cannot chmod a file it does not own.
COPY --chmod=0755 deploy/sandbox/open-conversation.sh /usr/local/bin/open-conversation

# Where a session's git configuration comes from. A file in the repository rather than a printf here,
# so a test can read the same thing the image ships. Owned by the sandbox user, because the system
# writes to it at sandbox birth.
COPY --chown=agent:agent deploy/sandbox/gitconfig /home/agent/.gitconfig

# Get the first run out of the way.
#
# An exec is non interactive and skips all of this, but attaching to a conversation is interactive, and
# a sandbox is a fresh container every time. Without this the operator lands in the theme picker and
# then the workspace trust prompt instead of their conversation, which reads as "the token is not
# working" because nothing ever gets far enough to authenticate. The runtime rewrites this file as it
# runs; these are only the starting values.
#
# The directory is made here, as the sandbox user, and nothing else is put in it. The system mounts the
# workspace's own directory over this path in every sandbox, so a file the image writes here is a file
# no session ever reads: what the runtime is told, beyond this first run, is rendered by the system and
# mounted read only somewhere the mount cannot hide. Everything the runtime keeps about a conversation
# lands in this directory, the transcripts among them, so it has to be the sandbox user's.
RUN mkdir -p /home/agent/.claude \
    && printf '%s\n' '{"hasCompletedOnboarding":true,"theme":"dark","projects":{"/home/agent/workspace":{"hasTrustDialogAccepted":true,"hasCompletedProjectOnboarding":true}}}' > /home/agent/.claude.json
