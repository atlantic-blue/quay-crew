# Multi stage build for any Quay Crew service. Pick the service with --build-arg SERVICE=<name>.
#
# Two runtime stages. Most services use "runtime", an unprivileged distroless image. The control
# plane uses "runtime-docker": it creates each session's sandbox as a container on the host daemon
# through a mounted Docker socket, so it needs the Docker client and the privileges to read that
# socket. Select the stage with `target:` in compose.
FROM golang:1.25 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG SERVICE=gateway
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/service ./cmd/${SERVICE}

# The hooks this build ships, built here so /hooks below carries an executable.
#
# A hook reaches a sandbox as files and the runtime runs one of them by path, so the entry point has
# to exist before anything reads the directory. Each hook is its own module, which is what makes it a
# plugin rather than part of the crew, so this builds each one where it stands. Static, because the
# result is mounted into whatever image a session runs rather than into this one.
RUN for dir in $(find hooks -maxdepth 2 -name go.mod -exec dirname {} \;); do \
        CGO_ENABLED=0 GOOS=linux go build -C "$dir" -trimpath -o bin/hook . || exit 1; \
    done

# The Docker client is a static binary, so it runs as is on distroless static.
FROM docker:28-cli AS dockercli

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/service /service
USER nonroot:nonroot
ENTRYPOINT ["/service"]

# Runs as root because the host's Docker socket is not readable by the nonroot user. Access to that
# socket is already equivalent to root on the host, so this grants nothing further.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime-docker
COPY --from=build /out/service /service
# The skills this build ships with, so a fresh crew can be given them without the operator importing
# each one by hand. They are read once, at startup, into a crew whose catalogue is empty.
COPY --from=build /src/skills /skills

# The hooks this build ships with, on the same terms as the skills above: in the image so a fresh crew
# can be put under them without the operator importing anything.
COPY --from=build /src/hooks /hooks
COPY --from=dockercli /usr/local/bin/docker /usr/local/bin/docker
USER root:root
ENTRYPOINT ["/service"]
