package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// What a job says it would build, on the surface a person reads it on, and what accepting it does.

// listing is a system with one job in it, standing at the design gate, and the model behind it, so a
// case can say what the second list comes back as.
type listing struct {
	client quaycrewv1.ControlPlaneServiceClient
	server *controlplane.Server
	runner *model.FakeRunner
	id     string
}

// aJobWaitingToAcceptItsList walks a job through the reading and up to the list, so a person is
// standing in front of the question the design stage asks.
func aJobWaitingToAcceptItsList(t *testing.T) listing {
	t.Helper()
	runner := &model.FakeRunner{}
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "the transcript page",
		"--brief", "build what the design describes",
		"--product", "you paste a link and get the text back")

	held, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(held.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(held.GetJobs()), err)
	}
	id := held.GetJobs()[0].GetId()
	waitForPhase(t, srv, client, id, job.PhaseAsking)
	mustRun(t, client, "job", "answer", id, "1: on the command line first")
	waitForPhase(t, srv, client, id, job.PhaseAsking)
	return listing{client: client, server: srv, runner: runner, id: id}
}

func TestJobShowSaysWhatItWouldBuildAndWhatItIsWaitingFor(t *testing.T) {
	one := aJobWaitingToAcceptItsList(t)

	shown := mustRun(t, one.client, "job", "show", one.id)

	for _, want := range []string{
		"what it would build, waiting for you to accept the list:",
		"Vertical 1:", "Shown 1:",
		"Does this list get that sentence?",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
	// Nothing about a plan, because there is none: the plan is written from the list a person accepts,
	// and a plan here would mean the crew wrote one before anybody chose the deliverables.
	if strings.Contains(shown, "plan, approved") || strings.Contains(shown, "plan, not approved") {
		t.Errorf("krewe job show says there is a plan before the list was accepted: %q", shown)
	}
}

func TestJobShowSaysTheListWasAccepted(t *testing.T) {
	one := aJobWaitingToAcceptItsList(t)

	mustRun(t, one.client, "job", "answer", one.id, "yes")

	shown := mustRun(t, one.client, "job", "show", one.id)
	if !strings.Contains(shown, "what it builds, accepted:") {
		t.Errorf("krewe job show does not say the list was accepted: %q", shown)
	}
	if strings.Contains(shown, "waiting for you to accept") {
		t.Errorf("an accepted list still reads as waiting: %q", shown)
	}
}

// The mark a person's own vertical carries, on the surface. Once both are on the row, a list a
// person changed and a list the machine proposed read the same, and this is where they stop.
func TestJobShowMarksTheVerticalsThePersonPutOnTheList(t *testing.T) {
	one := aJobWaitingToAcceptItsList(t)

	// Sent back, and the session writes the list again with the person's own vertical marked. The
	// double answers what this case sets, so the second list is the one it means.
	one.runner.Reply = "Yours 1: a person exports the transcript as a file they can keep\n" +
		"Shown 1: the file lands in the folder the person chose, named after the link"
	mustRun(t, one.client, "job", "answer", one.id, "the export is the one I actually need")
	waitForPhase(t, one.server, one.client, one.id, job.PhaseAsking)

	shown := mustRun(t, one.client, "job", "show", one.id)
	for _, want := range []string{
		"Yours 1: a person exports the transcript as a file they can keep",
		"one of these is yours, opening with Yours",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
}
