package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// What a person reads back, on the surface they read it on. A brief, a step of the plan, a claim, a
// question and an answer are written at whatever length the work needs, and krewe job show prints
// every word of each one.
//
// These go through the tool rather than through the store, because the store never refused any of
// this. What refused it stands between the two, and a case that wrote straight to the store would
// pass today while a person still could not declare the job.

// atLength is prose of at least this many bytes, each paragraph numbered so no two lines are the
// same. A case that repeats one letter proves a length; this proves the words came back in the
// order they went in.
func atLength(atLeast int, one string) string {
	var built strings.Builder
	for at := 1; built.Len() < atLeast; at++ {
		fmt.Fprintf(&built, "Paragraph %d. %s\n\n", at, one)
	}
	return strings.TrimSpace(built.String())
}

func TestJobShowReadsALongBriefBackWordForWord(t *testing.T) {
	client := aSystemToJobIn(t)
	brief := atLength(job.BriefLimit*2, "The transcript page takes a link and gives back the text. "+
		"What the person types is the address of a video, and what they get back is the words in order.")

	said := mustRun(t, client, "job", "create", "--title", "the transcript page", "--brief", brief)
	id := strings.Fields(said)[1]

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, brief) {
		t.Fatalf("krewe job show prints %d bytes and the brief was written with %d, so it did not "+
			"come back word for word", len(shown), len(brief))
	}
}

func TestJobShowReadsALongClaimBackWordForWord(t *testing.T) {
	client := aSystemToJobIn(t)
	claim := "atlantic-blue/quay-krewe#647 " +
		strings.TrimSpace(strings.Repeat("the length cap that refuses work and stops the job ", 8))

	said := mustRun(t, client, "job", "create", "--title", "remove the cap",
		"--brief", "no length cap refuses text and no length cap stops a job", "--claim", claim)
	id := strings.Fields(said)[1]

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "claims "+claim) {
		t.Fatalf("krewe job show says:\n%s\nwant it to name the claim it was declared with", shown)
	}
}

// aJobWithATaskUnderWay is a system holding one job whose session is inside a task, which is the only
// moment a session can ask anything. The model is held open, so the job is still running when the
// question is put.
func aJobWithATaskUnderWay(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *controlplane.Server, string) {
	t.Helper()
	runner := &model.FakeRunner{
		Reply: "i asked a question and stopped", Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "choose where the transcripts are stored",
		"--brief", "pick the store and say what it costs at rest")
	// The whole identifier rather than the short one the tool prints, because the credential a session
	// carries names the job in full.
	held, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(held.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(held.GetJobs()), err)
	}
	id := held.GetJobs()[0].GetId()

	srv.TickJob(context.Background())
	<-runner.Started
	t.Cleanup(func() { close(runner.Gate) })
	return client, srv, id
}

// theLongQuestion is what a session has to be able to ask: the decision, and what each answer costs,
// which is more than four thousand bytes the moment there are three options with numbers under them.
func theLongQuestion() string {
	return atLength(job.QuestionLimit*2, "The store for the transcripts. Aurora Serverless version "+
		"two bills a minimum capacity continuously, about 43 dollars a month at rest, and DynamoDB "+
		"on demand bills nothing at rest. Which do you want, and why.")
}

func TestJobShowReadsALongQuestionBackWordForWord(t *testing.T) {
	client, srv, id := aJobWithATaskUnderWay(t)
	asked := theLongQuestion()

	if _, err := srv.AskJob(auth.WithGrant(context.Background(), auth.Grant{Job: id}),
		&quaycrewv1.AskJobRequest{Question: asked}); err != nil {
		t.Fatalf("a session asking a question of %d bytes was refused: %v", len(asked), err)
	}

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, asked) {
		t.Fatalf("krewe job show prints %d bytes and the question was asked with %d, so it did not "+
			"come back word for word", len(shown), len(asked))
	}
}

// What a person answered, read back on the surface they read it on. The row keeps it and the tool
// prints it, and between the two the wire drops it: the Job message carries a told field and nothing
// fills it in, so an answer of any length reads back as nothing at all.
func TestJobShowReadsALongAnswerBackWordForWord(t *testing.T) {
	client, srv, id := aJobWithATaskUnderWay(t)
	if _, err := srv.AskJob(auth.WithGrant(context.Background(), auth.Grant{Job: id}),
		&quaycrewv1.AskJobRequest{Question: "which store, and what does it cost at rest"}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}
	told := atLength(job.TellingLimit*2, "DynamoDB on demand, because nothing bills while nobody "+
		"uses it. The reads are by key, the writes come one at a time, and the table is empty between runs.")

	mustRun(t, client, "job", "answer", id, told)

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, told) {
		t.Fatalf("krewe job show prints %d bytes and the answer was written with %d, so it did not "+
			"come back word for word", len(shown), len(told))
	}
}

// A step of the plan, on the same surface. The plan travels a longer road than the rest: the session
// writes it, the system reads it back off the reply, and only a plan it could read reaches the row a
// person then reads. A step over the old ceiling was read as no plan at all.
func TestJobShowReadsALongPlanStepBackWordForWord(t *testing.T) {
	step := "read the design, then build the address that takes a link, " +
		strings.TrimSpace(strings.Repeat("and keep the reading of it beside the plan so a person can hold one against the other, ", 6))
	runner := &model.FakeRunner{Reply: "Here is the plan.\n\nStep 1: " + step}
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

	// The three stages in front of the plan, each answered the way a person answers it: what the job
	// understood, then the list of what it would build, then the requirements on that list becoming
	// failing tests.
	waitForPhase(t, srv, client, id, job.PhaseAsking)
	mustRun(t, client, "job", "answer", id, "1: on the command line first")
	waitForPhase(t, srv, client, id, job.PhaseAsking)
	mustRun(t, client, "job", "answer", id, "yes")
	// Asking or halted, rather than asking alone: a job whose plan nothing could read is asked once
	// more and stopped on the second reply, and a case that only waited for asking would report a
	// timeout instead of what happened to the job.
	waitForPhase(t, srv, client, id, job.PhaseAsking, job.PhaseStopped, job.PhaseFailed)
	found, err := client.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if phase := found.GetJob().GetPhase(); phase != job.PhaseAsking {
		t.Fatalf("the job is %q because %q, want it asking a person about the plan it wrote",
			phase, found.GetJob().GetReason())
	}

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "plan, not approved yet:") {
		t.Fatalf("krewe job show says:\n%s\nwant a plan waiting for a person to approve it", shown)
	}
	if !strings.Contains(shown, step) {
		t.Fatalf("krewe job show says:\n%s\nwant the step the session wrote, word for word", shown)
	}
}
