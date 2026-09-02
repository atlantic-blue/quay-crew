package controlplane_test

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

// A session that stops for a person, through the whole system: the store, the control plane and the
// controller that reads what the session answered.
//
// The incident these drive: four jobs stopped for a person and the record read running for every
// one, so nothing could say which of them needed anybody. What is proved here is that the record
// itself learns it, with nobody opening a conversation.

// theChoice is what a session writes when the work stops at something no measurement settles.
const theChoice = "Two roles read the plan, or one. Two costs a task each time it runs. Which?"

// aSystemWhoseSessionAnswers stands the control plane on the memory store, with a model that answers
// exactly what it is given.
func aSystemWhoseSessionAnswers(t *testing.T, reply string) (*controlplane.Server, store.Store) {
	t.Helper()
	kept := store.NewMemory()
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{Reply: reply, Exact: true},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	}), kept
}

// The whole of it. The session answers that a person has to decide, nobody opens anything, and the
// record says a person is needed: the phase every reader of what waits on you already reads, with
// the question the session wrote on the row beside it.
func TestAJobWhoseSessionStopsForAPersonReadsAsWaitingOnOne(t *testing.T) {
	server, _ := aSystemWhoseSessionAnswers(t,
		theChoice+"\n\n"+job.OutcomeMarker+" "+job.OutcomeDecide)
	ctx := context.Background()
	declared := declaredIn(t, server, "choose how many roles read a plan")

	asking := tickUntil(t, server, declared.GetId(), job.PhaseAsking)

	if asking.GetQuestion() != theChoice {
		t.Fatalf("the question on the row is %q, want what the session wrote", asking.GetQuestion())
	}
	// The read every surface makes. A job waiting on a person has to be findable without knowing
	// which job to ask about, because the person does not know either: that is the whole complaint.
	waiting, err := server.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Phase: job.PhaseAsking})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(waiting.GetJobs()) != 1 || waiting.GetJobs()[0].GetId() != declared.GetId() {
		t.Fatalf("%d jobs read as waiting on a person, want the one that stopped", len(waiting.GetJobs()))
	}
}

// And what happens next, which is the half that decides whether any of it is worth having: the
// person answers, and the answer arrives in the conversation that stopped.
func TestThePersonsAnswerReachesTheSessionThatStoppedForThem(t *testing.T) {
	server, _ := aSystemWhoseSessionAnswers(t,
		theChoice+"\n\n"+job.OutcomeMarker+" "+job.OutcomeDecide)
	ctx := context.Background()
	declared := declaredIn(t, server, "choose how many roles read a plan")
	tickUntil(t, server, declared.GetId(), job.PhaseAsking)

	answered, err := server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: declared.GetId(), Answer: "One role reads it. Two is a task we pay for twice.",
	})
	if err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	if answered.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("an answered job is %q, want pending so a controller starts it again",
			answered.GetJob().GetPhase())
	}

	server.TickJob(ctx)
	var sent []*quaycrewv1.Task
	waitFor(t, func() bool {
		sent = tasksOf(t, server, declared.GetId())
		return len(sent) == 2
	})
	carried := sent[1].GetPrompt()
	if !strings.Contains(carried, "One role reads it") {
		t.Fatalf("the second task does not carry the answer:\n%s", carried)
	}
	if !strings.Contains(carried, theChoice) {
		t.Fatalf("the second task does not restate the question, so the answer arrives at nothing:\n%s", carried)
	}
}

// The quiet case, through the same system: a job that answered its work reads as done and waits on
// nobody. A record that reported every landing as needing a person would be noise, and noise is what
// trains somebody to ignore the one that matters.
func TestAJobThatFinishedItsWorkWaitsOnNobody(t *testing.T) {
	server, _ := aSystemWhoseSessionAnswers(t,
		"The bill is due on the 14th.\n\n"+job.OutcomeMarker+" "+job.OutcomeProved)
	ctx := context.Background()
	declared := declaredIn(t, server, "read the electricity bill")

	tickUntil(t, server, declared.GetId(), job.PhaseDone)

	waiting, err := server.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Phase: job.PhaseAsking})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(waiting.GetJobs()) != 0 {
		t.Fatalf("%d jobs read as waiting on a person, and the work finished", len(waiting.GetJobs()))
	}
}

// tickUntil runs the controller until the job reaches a phase, so an assertion never reads a row
// while a task is still in flight behind a dispatch that let go of it.
func tickUntil(t *testing.T, server *controlplane.Server, id, phase string) *quaycrewv1.Job {
	t.Helper()
	ctx := context.Background()
	var found *quaycrewv1.Job
	waitFor(t, func() bool {
		server.TickJob(ctx)
		read, err := server.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		found = read.GetJob()
		return found.GetPhase() == phase
	})
	if found.GetPhase() != phase {
		t.Fatalf("the job is %q saying %q, want %q", found.GetPhase(), found.GetReason(), phase)
	}
	return found
}
