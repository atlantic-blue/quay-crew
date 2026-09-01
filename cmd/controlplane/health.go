package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// healthArg is what this binary is run with to ask the system beside it whether it is serving. It is a
// second mode of the same binary rather than a second tool, because the image the control plane runs
// in carries this binary and nothing else: no shell, no client, nothing to ask with.
const healthArg = "health"

// probeHealth asks the control plane on this machine whether it can still write, and is the process
// exit code a container health check reads. Zero is serving.
//
// The system guards every call, so this presents the same token the server minted, read from the same
// data directory the server keeps it in.
func probeHealth() int {
	ctx, giveUp := context.WithTimeout(context.Background(), healthProbeWait)
	defer giveUp()

	token, err := os.ReadFile(filepath.Join(os.Getenv("QC_DATA_DIR"), auth.TokenFile))
	if err != nil {
		fmt.Fprintf(os.Stderr, "health: the system's token could not be read: %v\n", err)
		return 1
	}

	conn, err := grpc.NewClient(dialAddr(envOr("QC_GRPC_ADDR", "127.0.0.1:50051")),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(auth.Credentials(strings.TrimSpace(string(token)))))
	if err != nil {
		fmt.Fprintf(os.Stderr, "health: the system could not be dialled: %v\n", err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	answer, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "health: the system did not answer: %v\n", err)
		return 1
	}
	if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		fmt.Fprintf(os.Stderr, "health: the system is %s: it answers reads and cannot write. "+
			"Its own log says which write did not land.\n", answer.GetStatus())
		return 1
	}
	fmt.Fprintln(os.Stdout, "serving")
	return 0
}

// healthProbeWait is how long the probe waits for the answer. Longer than the system's own budget for
// a write, so a system that has already given up on one answers this rather than being cut off mid
// sentence and reported as silent.
const healthProbeWait = 10 * time.Second

// dialAddr fills in the host the server left off. The stack tells the control plane to listen on
// ":50051", which says every interface to a listener and says nothing at all to a dialler.
func dialAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}
