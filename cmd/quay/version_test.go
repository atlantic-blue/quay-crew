package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// A system is three parts, each built on its own: the tool, the control plane and the image every
// session runs in. Three defects on 27 August 2026 were investigated as live and were all fixed
// already, because the tool in use was thirteen minutes older than the first fix and nothing said so.

// aSystemBuiltFrom stands up a control plane that reports the builds it is given.
func aSystemBuiltFrom(t *testing.T, system, sandboxImage string) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	return testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Info: controlplane.Info{Version: system, SandboxBuild: sandboxImage},
	})
}

// unreachableClient is a client dialling an address with nothing on it, which is what a system that is
// down looks like to the tool.
func unreachableClient(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	t.Setenv(HomeEnv, t.TempDir())
	conn, err := grpc.NewClient("passthrough:///127.0.0.1:1",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}

// All three, because an operator chasing a defect has to know which of them is behind.
func TestVersionNamesTheToolTheSystemAndTheSandboxImage(t *testing.T) {
	client := aSystemBuiltFrom(t, "cafe1234", "01dimage")

	printed := mustRun(t, client, "version")

	// Each on a line of its own, labelled, because a build named only inside a sentence about a
	// difference disappears the moment there is no difference to report.
	for label, want := range map[string]string{
		"tool": version, "system": "cafe1234", "sandbox image": "01dimage",
	} {
		if !hasLine(printed, label, want) {
			t.Errorf("quay version has no %q line saying %q: %q", label, want, printed)
		}
	}
}

// hasLine says whether one line of the output labels a part of the system and gives its build.
func hasLine(printed, label, build string) bool {
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(line, label) && strings.TrimSpace(strings.TrimPrefix(line, label)) == build {
			return true
		}
	}
	return false
}

// Naming both builds is the whole value: "behind" without the two builds is a fact nobody can act on.
func TestVersionSaysWhenTheToolAndTheSystemAreDifferentBuilds(t *testing.T) {
	client := aSystemBuiltFrom(t, "cafe1234", "")

	printed := mustRun(t, client, "version")

	if !strings.Contains(printed, "the tool and the system are different builds") {
		t.Fatalf("quay version does not report the difference: %q", printed)
	}
	if strings.Count(printed, "cafe1234") < 2 || strings.Count(printed, version) < 2 {
		t.Fatalf("the sentence about the difference does not name both builds: %q", printed)
	}
}

func TestVersionSaysWhenTheSandboxImageIsADifferentBuild(t *testing.T) {
	client := aSystemBuiltFrom(t, version, "01dimage")

	printed := mustRun(t, client, "version")

	if !strings.Contains(printed, "sandbox image") || !strings.Contains(printed, "different build") {
		t.Fatalf("quay version does not report the image being a different build: %q", printed)
	}
	if strings.Count(printed, "01dimage") < 2 {
		t.Fatalf("the sentence about the difference does not name the image's build: %q", printed)
	}
}

// A system all of one build must say nothing, or the warning is noise and stops being read.
func TestVersionSaysNothingAboutADifferenceWhenEveryPartMatches(t *testing.T) {
	client := aSystemBuiltFrom(t, version, version)

	printed := mustRun(t, client, "version")

	if strings.Contains(printed, "different build") {
		t.Fatalf("quay version reports a difference between parts of one build: %q", printed)
	}
}

// A system from before this field existed answers with nothing. That is an old system, not a fault.
func TestASystemTooOldToSayWhichBuildItIsIsNotAnError(t *testing.T) {
	client := aSystemBuiltFrom(t, "", "")

	printed := mustRun(t, client, "version")

	if !strings.Contains(printed, "unknown") {
		t.Fatalf("quay version does not say the system's build is unknown: %q", printed)
	}
	if !strings.Contains(printed, firstSystemBuildThatSays) {
		t.Fatalf("quay version does not name the build that first reports it: %q", printed)
	}
	if strings.Contains(printed, "different build") {
		t.Fatalf("an unknown build was reported as a difference: %q", printed)
	}
}

// A system that cannot be reached at all is not a build difference either, and the version of the tool
// is still worth printing.
func TestVersionAnswersWhenTheSystemCannotBeReached(t *testing.T) {
	client := unreachableClient(t)

	printed, err := runQuay(t, client, "version")
	if err != nil {
		t.Fatalf("quay version against a system that is down: %v", err)
	}
	if !strings.Contains(printed, version) {
		t.Fatalf("quay version does not name the tool's own build: %q", printed)
	}
	if strings.Contains(printed, "different build") {
		t.Fatalf("a system nobody reached was reported as a different build: %q", printed)
	}
}

// The warning belongs on standard error. Standard output is where a caller reads data, and one extra
// line there is a value nobody asked for in the middle of a value they did.
func TestTheDriftLineGoesToStandardError(t *testing.T) {
	client := aSystemBuiltFrom(t, "cafe1234", "")

	var said bytes.Buffer
	reportDrift(context.Background(), client, &said)

	if !strings.Contains(said.String(), "the tool and the system are different builds") {
		t.Fatalf("no drift line on standard error: %q", said.String())
	}
	for _, want := range []string{version, "cafe1234"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the drift line does not name %q: %q", want, said.String())
		}
	}
	if lines := strings.Count(strings.TrimRight(said.String(), "\n"), "\n"); lines != 0 {
		t.Fatalf("the drift line is %d lines, want one: %q", lines+1, said.String())
	}
}

func TestNothingIsSaidWhenTheToolAndTheSystemAreTheSameBuild(t *testing.T) {
	client := aSystemBuiltFrom(t, version, "")

	var said bytes.Buffer
	reportDrift(context.Background(), client, &said)

	if said.String() != "" {
		t.Fatalf("standard error says %q about a tool and a system of one build", said.String())
	}
}

// An old system says nothing about its build, and warning on every command against one would be a line
// nobody can act on, on every command, forever.
func TestNothingIsSaidWhenTheSystemCannotSayWhichBuildItIs(t *testing.T) {
	client := aSystemBuiltFrom(t, "", "")

	var said bytes.Buffer
	reportDrift(context.Background(), client, &said)

	if said.String() != "" {
		t.Fatalf("standard error says %q about a system that reports no build", said.String())
	}
}

// The check must never stop a command. A system that is down is the command's own business to report.
func TestTheDriftCheckSaysNothingWhenTheSystemCannotBeReached(t *testing.T) {
	client := unreachableClient(t)

	var said bytes.Buffer
	reportDrift(context.Background(), client, &said)

	if said.String() != "" {
		t.Fatalf("standard error says %q about a system nobody reached", said.String())
	}
}

// The command runs whatever the check found, so a command's own answer is never held back by it.
func TestACommandStillAnswersWhenTheSystemIsADifferentBuild(t *testing.T) {
	client := aSystemBuiltFrom(t, "cafe1234", "")
	mustRun(t, client, "workspace", "create", "me")

	listed, err := runQuay(t, client, "workspace", "list")
	if err != nil {
		t.Fatalf("quay workspace list: %v", err)
	}
	if !strings.Contains(listed, "me") {
		t.Fatalf("the command did not answer: %q", listed)
	}
	if strings.Contains(listed, "different build") {
		t.Fatalf("standard output carries the drift line: %q", listed)
	}
}
