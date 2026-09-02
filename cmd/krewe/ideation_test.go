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

// What an operator sees on the command line about the stage in front of the plan: what the job
// thinks it was asked for, what it does not know, what it assumed, and what a person then wrote.
//
// It is one of the two surfaces this has to be readable on, and the one a person is already in when
// they declare a job.

// aJobWaitingToBeAnswered is a system holding one job that has said what it understood and is
// waiting for a person.
func aJobWaitingToBeAnswered(t *testing.T) (quaycrewv1.ControlPlaneServiceClient,
	*controlplane.Server, string) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "the transcript page",
		"--brief", "build what the design describes",
		"--product", "you paste a link and get the text back")

	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(listed.GetJobs()), err)
	}
	id := listed.GetJobs()[0].GetId()
	waitForPhase(t, srv, client, id, job.PhaseAsking)
	return client, srv, id
}

// The reading, on the surface a person is already standing in. Every heading reaches them, and the
// line that says the answer is theirs to write.
func TestJobShowSaysWhatItUnderstoodAndWhatItIsWaitingFor(t *testing.T) {
	client, _, id := aJobWaitingToBeAnswered(t)

	shown := mustRun(t, client, "job", "show", id)

	for _, want := range []string{
		"what it understands", "in your own words",
		"Understood:", "Not:", "Told:", "Assumed:", "Unknown:", "Confidence:", "Question 1:",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
	if strings.Contains(shown, "plan, approved") || strings.Contains(shown, "plan, not approved") {
		t.Errorf("krewe job show says there is a plan before anybody answered: %q", shown)
	}
}

// And after a person answers: their words, and the question their answer left alone. A record that
// showed the answer and dropped what it did not cover would read as agreement with all of it.
func TestJobShowSaysWhatThePersonAnsweredAndWhatIsStillUnknown(t *testing.T) {
	client, _, id := aJobWaitingToBeAnswered(t)

	mustRun(t, client, "job", "answer", id, "the surface is the command line, nothing else changes")

	shown := mustRun(t, client, "job", "show", id)
	for _, want := range []string{
		"you answered", "the surface is the command line", "still unknown", "Assumed:",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
}
