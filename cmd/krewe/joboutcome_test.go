package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// What the operator sees, driven all the way through: a session answers with a word, and the word is
// on the listing, on the job, and in the filter. A test that stopped at the row would prove the field
// is written and nothing about whether anybody can read it.

func aSystemAnswering(t *testing.T, reply string, exact bool) (quaycrewv1.ControlPlaneServiceClient, *controlplane.Server) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: reply, Exact: exact},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client, srv
}

// declared runs a job to its end and answers with its identifier.
func declared(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, srv *controlplane.Server,
	title, phase string) string {
	t.Helper()
	mustRun(t, client, "job", "create", "--title", title, "--brief", "open it and say when it is due")
	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(listed.GetJobs()) == 0 {
		t.Fatalf("the system holds no jobs (%v)", err)
	}
	id := listed.GetJobs()[0].GetId()
	ctx := context.Background()
	srv.TickJob(ctx)
	waitForJob(t, client, id, job.PhaseRunning)
	// Ticked until the row says what it ended as, rather than once: the dispatch is detached, so the
	// answer arrives on a goroutine and a single tick can read a task that has not landed. A test that
	// asserted on the row after one tick would pass or fail on how fast the machine is.
	deadline := time.Now().Add(10 * time.Second)
	for {
		srv.TickJob(ctx)
		found, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err == nil && found.GetJob().GetPhase() == phase {
			return id
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job never reached %s", phase)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The refusal first, and the whole of what a person is left with: the job did not settle, the screen
// says which line was missing, and what the session said is still there to read.
func TestJobShowSaysWhenAnAnswerStatedNoOutcome(t *testing.T) {
	client, srv := aSystemAnswering(t, "I read the bill and it is due on the 14th", true)
	id := declared(t, client, srv, "read the electricity bill", job.PhaseStopped)

	shown := mustRun(t, client, "job", "show", id)
	if strings.Contains(shown, "outcome: ") {
		t.Fatalf("krewe job show names an outcome on a job that stated none: %q", shown)
	}
	if !strings.Contains(shown, job.OutcomeMarker) {
		t.Fatalf("krewe job show says %q, want it to say which line was missing", shown)
	}
	if !strings.Contains(shown, "I read the bill") {
		t.Fatalf("krewe job show says %q, want what the session said", shown)
	}
	// The listing says nothing was stated rather than leaving a hole in the row.
	if listing := mustRun(t, client, "job", "list"); !strings.Contains(listing, "stopped") {
		t.Fatalf("krewe job list says %q", listing)
	}
}

func TestJobShowSaysTheWordTheJobEndedOn(t *testing.T) {
	client, srv := aSystemAnswering(t,
		"the credential ran out\n\n"+job.OutcomeMarker+" "+job.OutcomeBlocked, false)
	id := declared(t, client, srv, "read the electricity bill", job.PhaseDone)

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "outcome: "+job.OutcomeBlocked) {
		t.Fatalf("krewe job show says %q, want the word the job ended on", shown)
	}
	// What the word means, beside it, because four words a reader has to look up are four words.
	if !strings.Contains(shown, job.OutcomeMeans(job.OutcomeBlocked)) {
		t.Fatalf("krewe job show says %q, want it to say what the word means", shown)
	}
	// Above the answer, because the answer is the explanation and this is the signal.
	if strings.Index(shown, "outcome: ") > strings.Index(shown, "\nanswer:") {
		t.Fatalf("krewe job show puts the outcome below the answer: %q", shown)
	}
	if listing := mustRun(t, client, "job", "list"); !strings.Contains(listing, job.OutcomeBlocked) {
		t.Fatalf("krewe job list says %q, want the word in the row", listing)
	}
}

func TestJobListNarrowsByOutcome(t *testing.T) {
	client, srv := aSystemAnswering(t,
		"it is done\n\n"+job.OutcomeMarker+" "+job.OutcomeBlocked, false)
	declared(t, client, srv, "read the water bill", job.PhaseDone)

	listing := mustRun(t, client, "job", "list", "--outcome", job.OutcomeBlocked)
	if !strings.Contains(listing, "read the water bill") {
		t.Fatalf("krewe job list --outcome blocked says %q", listing)
	}
	empty := mustRun(t, client, "job", "list", "--outcome", job.OutcomeDecide)
	if strings.Contains(empty, "read the water bill") {
		t.Fatalf("krewe job list --outcome decide says %q, and nothing ended that way", empty)
	}
}

// A word nothing ends on is refused with the four offered back, because an empty listing reads
// exactly like a system holding no such jobs.
func TestJobListRefusesAWordThatIsNotAnOutcome(t *testing.T) {
	client, _ := aSystemAnswering(t, "ok", false)

	var out bytes.Buffer
	err := run(context.Background(), client, []string{"job", "list", "--outcome", "complete"}, &out, "")
	if err == nil {
		t.Fatal("krewe job list --outcome complete was answered")
	}
	for _, word := range job.Outcomes() {
		if !strings.Contains(err.Error(), word) {
			t.Fatalf("the refusal says %q, want it to offer %q", err, word)
		}
	}
}
