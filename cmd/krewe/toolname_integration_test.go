//go:build integration

package main

import (
	"bytes"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc"
)

// The tool as an operator holds it: two binaries built the way the install target builds them, run as
// processes, against a control plane on a real address.
//
// Every other test in this package calls run directly over an in memory connection. That proves the
// routing and proves nothing about the name: a package tests itself under whatever name its directory
// has. What decides whether the rename worked is which file the shell finds, what it does, and what it
// exits with, and none of those exist inside the test process.

// aSystemOnAnAddress serves a control plane on a port a second process can dial, and answers with the
// address.
func aSystemOnAnAddress(t *testing.T) string {
	t.Helper()

	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	return listener.Addr().String()
}

// bothNames builds the tool and the name it used to have, into one directory, so a test runs the pair
// the way a machine holds them: side by side on the path.
func bothNames(t *testing.T) (krewe, quay string) {
	t.Helper()

	dir := t.TempDir()
	for name, from := range map[string]string{"krewe": ".", "quay": "../quay"} {
		built := filepath.Join(dir, name)
		if out, err := exec.Command("go", "build", "-o", built, from).CombinedOutput(); err != nil {
			t.Fatalf("building %s from %s: %v\n%s", name, from, err, out)
		}
	}
	return filepath.Join(dir, "krewe"), filepath.Join(dir, "quay")
}

// invocation is what came back from running one of them.
type invocation struct {
	stdout, stderr string
	exit           int
}

// runs one of the binaries against the system at address, with a home directory of its own: the tool
// keeps where the operator is standing on the machine it runs on, and a test must not write into the
// operator's.
func runs(t *testing.T, binary, address, home string, args ...string) invocation {
	t.Helper()

	command := exec.Command(binary, args...)
	command.Env = append(command.Environ(), "QC_GRPC_ADDR="+address, "KREWE_HOME="+home, "HOME="+home)
	var out, said bytes.Buffer
	command.Stdout, command.Stderr = &out, &said
	err := command.Run()

	ran := invocation{stdout: out.String(), stderr: said.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		ran.exit = exit.ExitCode()
	default:
		t.Fatalf("running %s %s: %v", binary, strings.Join(args, " "), err)
	}
	return ran
}

// TestTheNewNameDrivesTheSystemAndTheOldOneRefuses.
//
// The pair, in one test, because the half that matters is the one nobody writes: a test that the new
// command works passes whether or not the old one refuses, and an operator with the old name in their
// fingers meets the half that was never checked.
func TestTheNewNameDrivesTheSystemAndTheOldOneRefuses(t *testing.T) {
	address := aSystemOnAnAddress(t)
	krewe, quay := bothNames(t)
	home := t.TempDir()

	for _, args := range [][]string{
		{"workspace", "create", "me"},
		{"project", "create", "house-bills"},
		{"task", "when is the electricity bill due"},
	} {
		if ran := runs(t, krewe, address, home, args...); ran.exit != 0 {
			t.Fatalf("krewe %s exited %d: %s", strings.Join(args, " "), ran.exit, ran.stderr)
		}
	}

	listed := runs(t, krewe, address, home, "sessions")
	if listed.exit != 0 {
		t.Fatalf("krewe sessions exited %d: %s", listed.exit, listed.stderr)
	}
	if !strings.Contains(listed.stdout, "me/house-bills") {
		t.Errorf("krewe sessions lists nothing the system holds:\n%s", listed.stdout)
	}

	// The same command, under the name it used to have. It has to fail, and it has to say the word.
	refused := runs(t, quay, address, home, "sessions")
	if refused.exit == 0 {
		t.Fatalf("quay sessions exited 0, so a script carries on as though it worked:\n%s", refused.stdout)
	}
	if !strings.Contains(refused.stderr, "krewe") {
		t.Errorf("quay sessions says %q, and never names krewe", refused.stderr)
	}
	// The answer goes to standard error. Standard output is where a caller reads data, and a refusal
	// written there is read as the listing.
	if refused.stdout != "" {
		t.Errorf("quay sessions wrote %q to standard output", refused.stdout)
	}
}

// The old name refuses the whole surface, not the commands somebody remembered, and it refuses on its
// own as well: `quay` with no arguments opened the console, so it is the one an operator types most.
func TestTheOldNameRefusesWhateverFollowsIt(t *testing.T) {
	address := aSystemOnAnAddress(t)
	_, quay := bothNames(t)
	home := t.TempDir()

	for _, args := range [][]string{
		nil,
		{"sessions"},
		{"workspace", "list"},
		{"--help"},
		{"version"},
	} {
		ran := runs(t, quay, address, home, args...)
		if ran.exit == 0 {
			t.Errorf("quay %s exited 0", strings.Join(args, " "))
		}
		if !strings.Contains(ran.stderr, "krewe") {
			t.Errorf("quay %s says %q, and never names krewe", strings.Join(args, " "), ran.stderr)
		}
	}
}
