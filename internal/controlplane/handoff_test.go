package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The one call a session at the context ceiling makes: what it leaves behind, written over the
// credential the system minted for its job.
//
// The refusals come first, because the record is what decides whether the gate helps. A fresh session
// given an empty handoff pays for every discovery the last one made, which is more than the session
// at eighty per cent it replaced would have cost.

// TestAHandoffThatSaysNothingIsRefusedRatherThanWritten.
func TestAHandoffThatSaysNothingIsRefusedRatherThanWritten(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	_, err := system.server.RecordJobHandoff(ctx, &quaycrewv1.RecordJobHandoffRequest{
		Left: "   ", Tried: "the third rebase",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a handoff carrying nothing was written: %v", err)
	}
	if !strings.Contains(err.Error(), "what is left") {
		t.Fatalf("the refusal says %q, want it to say what is missing", err)
	}
	if handoffs := system.reading(t).GetHandoffs(); len(handoffs) != 0 {
		t.Fatalf("the job carries %d handoffs, want none", len(handoffs))
	}
}

// A session hands over the job it is doing and no other. The identifier in the request is checked
// against the credential rather than trusted, the way a step already is.
func TestASessionCannotHandOverSomebodyElsesJob(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.RecordJobHandoff(asJobCredential(context.Background(), system.job.GetId()),
		&quaycrewv1.RecordJobHandoffRequest{Left: "finish it", Id: "0123456789abcdef01234567"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("naming another job was accepted: %v", err)
	}
}

// A person is doing no job, so what they leave behind is nobody's handoff.
func TestACallerRunningNoJobCannotHandOver(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.RecordJobHandoff(context.Background(),
		&quaycrewv1.RecordJobHandoffRequest{Left: "finish it"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a caller doing no job handed one over: %v", err)
	}
}

// A handoff against a job that has already ended is a note about work nobody is doing, and no fresh
// session is ever given one.
func TestAHandoffIsRefusedOnceTheJobHasEnded(t *testing.T) {
	system := aJobThatFailed(t, "the sandbox went away")

	_, err := system.server.RecordJobHandoff(asJobCredential(context.Background(), system.job.GetId()),
		&quaycrewv1.RecordJobHandoffRequest{Left: "finish the migration"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a handoff was written on a job nobody is doing: %v", err)
	}
}

// And the write itself. What is left is kept whole, what was tried is kept beside it, and the record
// names the conversation that wrote it, which is what tells a handoff waiting to be taken up from one
// a fresh session already holds.
func TestAHandoffIsKeptWholeAndNamesTheConversationThatWroteIt(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	written, err := system.server.RecordJobHandoff(ctx, &quaycrewv1.RecordJobHandoffRequest{
		Left:  "the index is written, the query still reads the old one: branch 539-feat-index",
		Tried: "adding the index inside the renaming migration, which deadlocks",
	})
	if err != nil {
		t.Fatalf("RecordJobHandoff: %v", err)
	}
	handoffs := written.GetJob().GetHandoffs()
	if len(handoffs) != 1 {
		t.Fatalf("the job carries %d handoffs, want the one that was written", len(handoffs))
	}
	one := handoffs[0]
	if !strings.Contains(one.GetLeft(), "539-feat-index") {
		t.Fatalf("what is left reads back as %q", one.GetLeft())
	}
	if !strings.Contains(one.GetTried(), "deadlocks") {
		t.Fatalf("what was tried reads back as %q", one.GetTried())
	}
	if one.GetSession() != system.reading(t).GetSession() {
		t.Fatalf("the handoff names conversation %q, want the one doing the job, %q",
			one.GetSession(), system.reading(t).GetSession())
	}
	// The task the fresh session would be given, built from the record rather than from anything held
	// in a process. A test that a second session starts passes whether or not the handoff carries
	// anything, so this reads what it would actually be handed.
	carried := job.HandedOver(&job.Job{
		Brief: "choose where the transcripts are stored",
		Handoffs: []job.Handoff{{
			Left: one.GetLeft(), Tried: one.GetTried(), Session: one.GetSession(),
		}},
	})
	if !strings.Contains(carried, "539-feat-index") || !strings.Contains(carried, "deadlocks") {
		t.Fatalf("the fresh session would be handed:\n%s", carried)
	}
}

// A workspace declares its own ceiling, and one it could not act on is refused while the operator is
// looking rather than hours later inside a run.
func TestAContextCeilingIsAShareAndAWorkspaceThatSaysNothingTakesTheSystemsOwn(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()
	workspace := system.job.GetWorkspace()

	held, err := system.server.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("GetWorkspaceLimits: %v", err)
	}
	if got := held.GetLimits().GetContextCeilingPercent(); got != 0 {
		t.Fatalf("a workspace nobody configured carries a ceiling of %d on the row, want none", got)
	}

	asked := held.GetLimits()
	asked.ContextCeilingPercent = 140
	if _, err := system.server.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: asked,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a ceiling of 140 per cent of a window was accepted: %v", err)
	}

	asked.ContextCeilingPercent = 55
	written, err := system.server.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: asked,
	})
	if err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	if got := written.GetLimits().GetContextCeilingPercent(); got != 55 {
		t.Fatalf("the ceiling reads back as %d, want 55", got)
	}
}
