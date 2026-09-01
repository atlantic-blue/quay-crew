package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/store"
)

// The line above the blocks, and the two things about the page itself that decide whether an operator
// can trust what is on it: that it says when it was drawn, and that it draws itself again.

// TestAFigureNothingMeasuredReadsAsUnknownRatherThanAsHealthy is the sad case first, and it is the
// one that matters. A system nothing has sampled has no memory figure and has never probed itself, and
// a header that filled either gap with a number or a colour would be claiming a reading nobody took.
func TestAFigureNothingMeasuredReadsAsUnknownRatherThanAsHealthy(t *testing.T) {
	client := aSystem(t)

	body, status := get(t, client, "/")
	if status != http.StatusOK {
		t.Fatalf("the briefing answered %d", status)
	}
	for _, want := range []string{"unknown", display.HealthNotChecked} {
		if !strings.Contains(body, want) {
			t.Errorf("the header does not say %q, so it claims a reading nobody took:\n%s", want, body)
		}
	}
	if strings.Contains(body, display.HealthServing) {
		t.Errorf("a system that has never probed itself reads as serving:\n%s", body)
	}
}

// TestTheHeaderSaysHowMuchIsRunning is the one figure that replaces a listing. What is running is
// answered three times already, so the header says how many and the block below says which.
func TestTheHeaderSaysHowMuchIsRunning(t *testing.T) {
	held := store.NewMemory()
	client := systemOver(t, held, &model.FakeRunner{Reply: "ok"})
	workspace, project := placeOf(t, client)
	for _, phase := range []string{job.PhaseRunning, job.PhaseRunning, job.PhaseDone} {
		writeJob(t, held, &job.Job{
			Workspace: workspace, Project: project, Title: "a piece of work", Phase: phase,
			FinishedAt: finishedIf(phase),
		})
	}

	body, _ := get(t, client, "/")
	if !strings.Contains(body, "2 running") {
		t.Errorf("the header does not count what is running:\n%s", body)
	}
}

// TestAPartThatIsDownNamesItselfInTheHeader holds the other end of the health figure. A part that is
// down is the part somebody has to go and look at, so the header names it rather than saying the
// system is unwell.
func TestAPartThatIsDownNamesItselfInTheHeader(t *testing.T) {
	line, degraded := healthOf(&quaycrewv1.GetHealthResponse{
		Components: []*quaycrewv1.HealthComponent{
			{Name: "store", State: display.HealthServing},
			{Name: "events", State: display.HealthDown, Detail: "the event log did not take a record"},
		},
	}, nil)
	if !degraded {
		t.Error("a system with a part that is down does not read as degraded")
	}
	if !strings.Contains(line, "events") {
		t.Errorf("the header says %q, and it does not name the part that is down", line)
	}
}

// TestThePageDrawsItselfAgainAndSaysWhenItWasDrawn is staleness. A page that sits in a tab and looks
// current is the failure the briefing exists to end, and the moment it was drawn is only half of it.
func TestThePageDrawsItselfAgainAndSaysWhenItWasDrawn(t *testing.T) {
	client := aSystem(t)

	briefing, _ := get(t, client, "/")
	if !strings.Contains(briefing, `<meta http-equiv="refresh"`) {
		t.Errorf("the briefing never draws itself again, so it goes stale in silence:\n%s", briefing)
	}
	if !strings.Contains(briefing, "Drawn at") {
		t.Errorf("the briefing does not say when it was drawn:\n%s", briefing)
	}

	// The pages that are read once do not redraw. A conversation that jumped under the operator every
	// fifteen seconds would be a page nobody could read a long reply on, which is what it is for.
	listing, _ := get(t, client, "/sessions")
	if strings.Contains(listing, `<meta http-equiv="refresh"`) {
		t.Errorf("the session listing redraws itself, and nothing asked it to:\n%s", listing)
	}
}

// TestAJobCarryingAFlowRunTakesTheRunsAnswerCommand is the half of block one that decides whether the
// command on the page works. AnswerFlowRun refuses anything that is not a run, so a row that offered
// krewe job answer for a run's own job would hand the operator a refusal.
func TestAJobCarryingAFlowRunTakesTheRunsAnswerCommand(t *testing.T) {
	held := store.NewMemory()
	client := systemOver(t, held, &model.FakeRunner{Reply: "ok"})
	workspace, project := placeOf(t, client)

	// A job that asked for itself, first: it keeps the job command, and nothing about a run reaches it.
	alone := &job.Job{
		Workspace: workspace, Project: project, Title: "choose where the transcripts are stored",
		Phase: job.PhaseAsking, Question: "on demand, or a cluster that bills at rest?",
	}
	writeJob(t, held, alone)

	carrier := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project, Version: 1,
		Title: "review the pull request", Brief: "read it for security",
		Phase: job.PhaseAsking, Question: "the review found nothing. Post it?",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	run := &flow.Run{
		ID: store.NewID(), Workspace: workspace, Project: project,
		GraphName: "review", GraphVersion: 1, Node: "permit", Status: flow.StatusAsking,
		Question: "the review found nothing. Post it?",
		State:    map[string]string{}, Attempts: map[string]int{},
	}
	if err := held.CreateFlowRun(context.Background(), run, carrier,
		[]*job.Event{declaredRecordFor(carrier)}, ""); err != nil {
		t.Fatalf("CreateFlowRun: %v", err)
	}

	body, _ := get(t, client, "/")
	if !strings.Contains(body, "krewe job answer "+display.ShortID(alone.ID)) {
		t.Errorf("a job that asked for itself lost the command that answers it:\n%s", body)
	}
	if !strings.Contains(body, "krewe flow answer "+display.ShortID(run.ID)) {
		t.Errorf("the job carrying a run is not answered with the run's own command:\n%s", body)
	}
	if strings.Contains(body, "krewe job answer "+display.ShortID(carrier.ID)) {
		t.Errorf("the job carrying a run offers a command the system would refuse:\n%s", body)
	}
}

// finishedIf is the moment a job in a terminal phase ended, and nothing for one still going.
func finishedIf(phase string) *time.Time {
	if phase != job.PhaseDone {
		return nil
	}
	ended := time.Now().UTC().Add(-time.Hour)
	return &ended
}

// writeJob puts one job in the store in the phase the case needs. The phases this page reads are the
// controller's to assign, and a case about what a page draws must not wait for a model.
func writeJob(t *testing.T, held store.Store, one *job.Job) {
	t.Helper()
	if one.ID == "" {
		one.ID = store.NewID()
	}
	if one.Brief == "" {
		one.Brief = "one piece of work"
	}
	one.Version = 1
	if one.CreatedAt.IsZero() {
		one.CreatedAt = time.Now().UTC()
	}
	one.UpdatedAt = one.CreatedAt
	if err := held.CreateJob(context.Background(), one, declaredRecordFor(one)); err != nil {
		t.Fatalf("CreateJob %q: %v", one.Title, err)
	}
}

func declaredRecordFor(one *job.Job) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: one.ID,
		Workspace: one.Workspace, Project: one.Project, Detail: one.Title,
		OccurredAt: time.Now().UTC(),
	}
}

// placeOf is the workspace and project the test system was built with, so a written job lands
// somewhere the page can name.
func placeOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) (string, string) {
	t.Helper()
	listed, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
	if err != nil || len(listed.GetProjects()) != 1 {
		t.Fatalf("want one project, got %d (%v)", len(listed.GetProjects()), err)
	}
	return listed.GetProjects()[0].GetWorkspace(), listed.GetProjects()[0].GetId()
}
