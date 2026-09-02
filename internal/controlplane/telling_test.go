package controlplane_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/telling"
)

// The one read every surface asks: what waits for a person.
//
// On 1 September 2026 four jobs stopped for a person and nothing told him. The transition wrote
// job.asked and nothing read it, so these drive the read itself: what it names, what it redacts,
// what it stays quiet about, and the record it leaves behind.

// The incident, in one test: a job enters asking and the read names it, with what it wants. Nobody
// typed a command to get this.
func TestAJobThatEntersAskingIsNamedByTheRead(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	waiting := waitingNow(t, system.server, "a test")
	if len(waiting) != 0 {
		t.Fatalf("a running job reads as waiting for a person: %v", waiting)
	}

	if _, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: theQuestion}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}

	waiting = waitingNow(t, system.server, "a test")
	if len(waiting) != 1 {
		t.Fatalf("%d jobs wait for a person, want the one that just asked", len(waiting))
	}
	if waiting[0].GetJob() != system.job.GetId() {
		t.Errorf("the telling names %q, and the job that asked is %q", waiting[0].GetJob(), system.job.GetId())
	}
	if waiting[0].GetWhy() != job.WaitingAsking {
		t.Errorf("the wait reads as %q, want asking", waiting[0].GetWhy())
	}
	if !strings.Contains(waiting[0].GetWant(), "Aurora Serverless") {
		t.Errorf("the telling does not say what the job wants: %q", waiting[0].GetWant())
	}
	if waiting[0].GetTitle() != system.job.GetTitle() {
		t.Errorf("the telling does not carry the title, so a person reads an identifier alone: %q",
			waiting[0].GetTitle())
	}
}

// A question can quote whatever the session had in front of it, and the telling is drawn on a screen
// and printed above a command. So it goes through the same redactor a record goes through.
func TestASealedValueInAQuestionIsNotInTheTelling(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	const token = "ghp_thisisthetokenthatmustnotbeprinted"
	if _, err := system.server.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace: system.job.GetWorkspace(), Key: "GITHUB_TOKEN", Value: token,
	}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if _, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: "the forge refused " + token + ", do i open a new one?"}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}

	waiting := waitingNow(t, system.server, "a test")
	if len(waiting) != 1 {
		t.Fatalf("%d jobs wait for a person", len(waiting))
	}
	if strings.Contains(waiting[0].GetWant(), token) {
		t.Fatalf("the telling prints a sealed value: %q", waiting[0].GetWant())
	}
	if !strings.Contains(waiting[0].GetWant(), "do i open a new one?") {
		t.Errorf("redacting took the question with it: %q", waiting[0].GetWant())
	}
}

// A wait under the limit says the job. A wait over it says the job and how long it has been, because
// a job that stopped one second ago and one that stopped an hour ago are not the same thing.
func TestTheAgeIsNamedOnlyOnceTheWaitPassesTheLimit(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	if _, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: theQuestion}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}

	fresh := waitingNow(t, system.server, "a test")[0]
	if fresh.GetOverLimit() {
		t.Errorf("a wait of a moment is already past the limit of %s", job.DefaultWaiting)
	}
	if said := telling.Line(fresh); strings.Contains(said, "waited") {
		t.Errorf("the line names an age on a wait nobody has been kept by: %q", said)
	}

	// The limit, rather than the clock: the workspace says a wait lasts one second here, which is
	// the same reading a wait of an hour gets against fifteen minutes.
	if _, err := system.server.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: system.job.GetWorkspace(), WaitingSeconds: 1},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)

	old := waitingNow(t, system.server, "a test")[0]
	if !old.GetOverLimit() {
		t.Fatalf("a wait past the workspace's limit does not read as past it: %d seconds", old.GetWaitedSeconds())
	}
	if said := telling.Line(old); !strings.Contains(said, "waited") {
		t.Errorf("the line does not name the age of a wait past the limit: %q", said)
	}
}

// The limit reads back off the workspace, so an operator who set one can see what they set.
func TestTheWaitingLimitReadsBack(t *testing.T) {
	server := newServer(&model.FakeRunner{})
	workspace, _ := newProject(t, server)
	ctx := context.Background()

	if _, err := server.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, WaitingSeconds: 300},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	held, err := server.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("GetWorkspaceLimits: %v", err)
	}
	if held.GetLimits().GetWaitingSeconds() != 300 {
		t.Fatalf("the limit reads back as %d seconds", held.GetLimits().GetWaitingSeconds())
	}
}

// The moment the telling went out is on the record, once for each wait. A surface that redraws every
// three seconds must not write a record every three seconds: the gap from job.asked would then be
// the time since the last redraw rather than the time a person spent not knowing.
func TestTheTellingIsRecordedOnceHoweverManySurfacesDrawIt(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	if _, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: theQuestion}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}

	waitingNow(t, system.server, "console")
	waitingNow(t, system.server, "command line")
	waitingNow(t, system.server, "console")

	listed, err := system.kept.ListJobEvents(ctx, system.job.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	var raised []*job.Event
	for _, one := range listed {
		if one.Kind == job.EventRaised {
			raised = append(raised, one)
		}
	}
	if len(raised) != 1 {
		t.Fatalf("%d records of the telling for one wait: %v", len(raised), kindsOf(listed))
	}
	if raised[0].Detail != "console" {
		t.Errorf("the record does not name the surface that carried it: %q", raised[0].Detail)
	}

	// And both moments read back off the job, so the gap between them is a number somebody can print.
	found := system.reading(t)
	if found.GetAskedAt() == nil || found.GetRaisedAt() == nil {
		t.Fatalf("the row does not carry both moments: asked %v, told %v",
			found.GetAskedAt(), found.GetRaisedAt())
	}
	if found.GetRaisedAt().AsTime().Before(found.GetAskedAt().AsTime()) {
		t.Errorf("the telling reads as older than the question it carried")
	}
}

// A caller that will not say who it is has told nobody, so nothing is recorded. Without this every
// read of the shape, including a test of it, would claim somebody had been told.
func TestAReadThatNamesNoSurfaceRecordsNothing(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	if _, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: theQuestion}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}
	if _, err := system.server.GetWaiting(ctx, &quaycrewv1.GetWaitingRequest{}); err != nil {
		t.Fatalf("GetWaiting: %v", err)
	}

	if raised := system.reading(t).GetRaisedAt(); raised != nil {
		t.Fatalf("a read that named no surface recorded a telling at %s", raised.AsTime())
	}
}

// Nothing waiting means nothing said. A telling that fires when nothing waits is worse than none,
// because the next real one is read as noise.
func TestAJobNobodyWaitsForProducesNoTelling(t *testing.T) {
	system := aJobUnderWay(t)

	waiting := waitingNow(t, system.server, "a test")
	if len(waiting) != 0 {
		t.Fatalf("%d jobs wait for a person while one is running: %v", len(waiting), waiting)
	}
	if said := telling.Count(waiting); said != "" {
		t.Errorf("a system with nothing waiting says %q", said)
	}
}

// waitingNow is what the system says waits for a person, read as one surface.
func waitingNow(t *testing.T, server interface {
	GetWaiting(context.Context, *quaycrewv1.GetWaitingRequest) (*quaycrewv1.GetWaitingResponse, error)
}, surface string) []*quaycrewv1.Waiting {
	t.Helper()
	answer, err := server.GetWaiting(context.Background(), &quaycrewv1.GetWaitingRequest{Surface: surface})
	if err != nil {
		t.Fatalf("GetWaiting: %v", err)
	}
	return answer.GetWaiting()
}
