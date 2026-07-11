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
COPY --from=dockercli /usr/local/bin/docker /usr/local/bin/docker
USER root:root
ENTRYPOINT ["/service"]
