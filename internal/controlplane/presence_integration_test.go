//go:build integration

package controlplane_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// What a listing costs, and whether it is telling the truth, against a real daemon.
//
// The unit tests hold the rules with a double. This is the other half: the same question, asked of
// containers that really exist, on the machine continuous integration runs on. It is also where the
// cost figure in the pull request comes from, because one exec per row per listing is a real price
// and it has to be a measured number rather than an estimate.

// listingSessions is how many rows the measurement is taken over. About the size of a real crew: the
// measurement that opened this work read eighteen containers on one machine.
const listingSessions = 20

// aCrewOverRealContainers gives back a control plane whose sandboxes are containers the daemon is
// really holding, and the sessions they belong to.
func aCrewOverRealContainers(ctx context.Context, t *testing.T, count int) (
	*controlplane.Server, sandbox.DockerProvider, []*quaycrewv1.Session) {
	t.Helper()

	data := t.TempDir()
	provider := sandbox.DockerProvider{
		Image:   "busybox:latest",
		Storage: sandbox.Storage{Dir: data, Host: data},
	}
	held := store.NewMemory()
	server := controlplane.NewServer(controlplane.Config{
		Store: held, Runner: &model.FakeRunner{Reply: "done"},
		Provider: provider, Secrets: secrets.NewMemory(),
	})

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// The rows first, then a container for each, which is the state a crew is in after a restart: the
	// process holds no handles and every container is still running.
	sessions := make([]*quaycrewv1.Session, 0, count)
	for index := range count {
		made, _, err := held.FindOrCreateSession(ctx, project.GetProject().GetId(),
			fmt.Sprintf("measured-%02d", index), store.Birth{})
		if err != nil {
			t.Fatalf("make a session: %v", err)
		}
		if _, err := provider.Create(ctx, sandbox.Config{ID: made.GetId()}); err != nil {
			t.Fatalf("make a container: %v", err)
		}
		t.Cleanup(func() { _ = provider.Remove(context.Background(), made.GetId()) })
		sessions = append(sessions, made)
	}
	return server, provider, sessions
}

// TestWhatOneListingOfTwentySessionsCosts is the number the pull request quotes. It is logged rather
// than asserted: how long a docker exec takes belongs to the machine, and a suite that failed on a
// slow runner would say nothing about the crew.
//
// The answers are asserted, which is what makes the timing worth anything: a run that measured
// twenty questions nobody answered would be fast and meaningless.
func TestWhatOneListingOfTwentySessionsCosts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	server, provider, sessions := aCrewOverRealContainers(ctx, t, listingSessions)

	// One of them is holding a conversation. Adopting the container that is already there rather than
	// making one, which is what the provider does for a session whose sandbox this process holds no
	// handle to.
	box, err := provider.Create(ctx, sandbox.Config{ID: sessions[0].GetId()})
	if err != nil {
		t.Fatalf("reach the first session's container: %v", err)
	}
	if err := startsSomethingCalledTheRuntime(ctx, t, box); err != nil {
		t.Fatalf("start something that looks like the runtime: %v", err)
	}
	// Proved before anything is timed, because a measurement over a container that never started the
	// process would be fast, green on the clock, and worthless.
	waitUntilRunning(ctx, t, provider, sessions[0].GetId())

	began := time.Now()
	listed, err := server.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Presence: true})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	took := time.Since(began)

	if len(listed.GetSessions()) != listingSessions {
		t.Fatalf("the listing carries %d rows, want %d", len(listed.GetSessions()), listingSessions)
	}
	awake, idle := 0, 0
	for _, session := range listed.GetSessions() {
		switch display.SessionStatus(session) {
		case display.StatusAwake:
			awake++
		case display.StatusIdle:
			idle++
		default:
			t.Fatalf("session %s reads %q against a real daemon, and it is neither running a "+
				"conversation nor empty", session.GetId(), display.SessionStatus(session))
		}
	}
	if awake != 1 {
		t.Fatalf("%d of %d containers read as running a conversation, and exactly one is",
			awake, listingSessions)
	}
	if idle != listingSessions-1 {
		t.Fatalf("%d containers read as empty, want %d", idle, listingSessions-1)
	}

	// Two questions per row, because presence asks the process table and the tmux server both.
	t.Logf("a listing of %d sessions against a real daemon took %s, which is %s a row and %s a question",
		listingSessions, took.Round(time.Millisecond),
		(took / listingSessions).Round(time.Millisecond),
		(took / (2 * listingSessions)).Round(time.Millisecond))

	// And one question on its own, which is what the fan out is spending. Measured after the listing
	// so the images and the containers are warm, the way they are on a console's second redraw.
	began = time.Now()
	if _, err := provider.RuntimeRunning(ctx, sessions[0].GetId()); err != nil {
		t.Fatalf("RuntimeRunning: %v", err)
	}
	t.Logf("one question to one container took %s", time.Since(began).Round(time.Millisecond))
}

// TestARealContainerWithNoTmuxServerIsNotReportedAsAttached is the sad path a double cannot prove.
// busybox has no tmux in it at all, so the attachment question fails inside the container, and the
// crew has to read that as nobody being attached rather than as a daemon it could not reach.
func TestARealContainerWithNoTmuxServerIsNotReportedAsAttached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	server, _, sessions := aCrewOverRealContainers(ctx, t, 1)

	attached, err := server.SessionAttached(ctx, sessions[0].GetId())
	if err != nil {
		t.Fatalf("a container with no tmux in it came back as a failure: %v", err)
	}
	if attached {
		t.Fatal("a container with no tmux server says somebody is attached to it")
	}

	listed, err := server.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Presence: true})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := display.SessionStatus(listed.GetSessions()[0]); got != display.StatusIdle {
		t.Fatalf("an empty container reads %q against a real daemon, want idle", got)
	}
}

// TestASessionWithNoContainerAtAllReadsIdle. The daemon answers, the container is not there, and that
// is an empty session rather than a crew that could not tell. Unknown here would send an operator
// looking for a broken daemon.
func TestASessionWithNoContainerAtAllReadsIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := sandbox.DockerProvider{Image: "busybox:latest"}
	running, err := provider.RuntimeRunning(ctx, "a00d36d6454a3de66d02c6a3")
	if err != nil {
		t.Fatalf("asking about a container that is not there failed: %v", err)
	}
	if running {
		t.Fatal("a session with no container is running a model runtime")
	}
}

// startsSomethingCalledTheRuntime leaves a long lived process in the container whose command line
// reads the way the model runtime's does.
//
// A script rather than a copy of a binary. busybox decides which applet to be from the name it was
// run as, so a copy of `sleep` under the runtime's name exits at once with "applet not found", and
// the container would be empty while the test believed it was busy. A script gives the same shape
// the real runtime gives when npm installs it behind an interpreter: the interpreter first and the
// runtime's name after it.
//
// Its output goes to /dev/null. A background process holding the exec's own pipe open would keep the
// exec from ever finishing, and the test would hang here rather than measure anything.
func startsSomethingCalledTheRuntime(ctx context.Context, t *testing.T, box sandbox.Sandbox) error {
	t.Helper()
	at := "/tmp/" + sandbox.RuntimeBinary
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c",
		"printf '#!/bin/sh\\nsleep 240\\n' > " + at + "; chmod +x " + at + "; " + at + " >/dev/null 2>&1 &"}})
	if err != nil {
		return err
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("%w: %s", err, proc.Stderr())
	}
	return nil
}

// waitUntilRunning holds until the container says the runtime is up, so nothing after it is racing
// the process starting. It fails rather than carrying on, because everything below depends on it.
func waitUntilRunning(ctx context.Context, t *testing.T, provider sandbox.DockerProvider, session string) {
	t.Helper()
	for range 50 {
		running, err := provider.RuntimeRunning(ctx, session)
		if err != nil {
			t.Fatalf("ask the container what it is running: %v", err)
		}
		if running {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the container never reported the process this test started, so nothing below it means anything")
}
