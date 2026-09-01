package console

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
)

// aBusySystem is two workspaces holding three projects between them, with work under each in every
// state a row has to say something about.
func aBusySystem() *treeClient {
	client := aSystemWithOneOfEverything()
	client.workspaces = append(client.workspaces,
		&quaycrewv1.Workspace{Id: "9999999999999999aaaaaaaa", Name: "quiet"})
	client.projects = append(client.projects, &quaycrewv1.Project{
		Id: "7777777777777777bbbbbbbb", Workspace: "1111111111111111aaaaaaaa",
		Name: "gardening", Repository: "atlantic-blue/gardening",
	})
	client.projects[0].Repository = "atlantic-blue/house-bills"
	client.jobs = append(client.jobs,
		&quaycrewv1.Job{
			Id: "aaaaaaaaaaaaaaaa11111111", Workspace: "1111111111111111aaaaaaaa",
			Project: "2222222222222222bbbbbbbb", Title: "answer the meter question",
			Phase: job.PhaseAsking, Question: "which meter?",
		},
		&quaycrewv1.Job{
			Id: "bbbbbbbbbbbbbbbb22222222", Workspace: "1111111111111111aaaaaaaa",
			Project: "7777777777777777bbbbbbbb", Title: "mow the lawn", Phase: job.PhaseDone,
		})
	return client
}

// A workspace row has to carry enough to choose by: how many projects, how much work is running, and
// whether anything under it is waiting for a person.
func TestAWorkspaceRowSaysEnoughToChooseBetweenWorkspaces(t *testing.T) {
	client := aBusySystem()

	rows, err := Workspaces(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing workspaces: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the console lists %d workspaces, want 2", len(rows))
	}
	busy, quiet := rows[0], rows[1]
	if busy.Name() != "acme" {
		busy, quiet = quiet, busy
	}

	// id, name, projects, running, asking, age.
	if busy.Cells[2] != "2" {
		t.Fatalf("acme says it holds %q projects, want 2", busy.Cells[2])
	}
	if busy.Cells[3] != "1" {
		t.Fatalf("acme says %q is running, want the one running job", busy.Cells[3])
	}
	if busy.Cells[4] != "1 asking" {
		t.Fatalf("acme says %q about work waiting for a person, want 1 asking", busy.Cells[4])
	}
	if busy.State != StateBusy {
		t.Fatalf("a workspace with work waiting for a person is drawn as %v, want busy", busy.State)
	}

	// A workspace with nothing under it says nothing loudly. A zero in the asking column would read
	// as a measurement, and this is the ordinary state of most rows.
	if quiet.Cells[2] != "0" {
		t.Fatalf("the quiet workspace says it holds %q projects, want 0", quiet.Cells[2])
	}
	if quiet.Cells[4] != "" {
		t.Fatalf("the quiet workspace says %q in the asking column, want nothing at all", quiet.Cells[4])
	}
	if quiet.State != StateReady {
		t.Fatalf("a workspace with nothing under it is drawn as %v, want ready", quiet.State)
	}
}

// A project row says where its work lands and what its work is doing. The repository is the fact that
// decides whether a job declared there can finish at all.
func TestAProjectRowSaysItsRepositoryAndWhatItsWorkIsDoing(t *testing.T) {
	client := aBusySystem()

	rows, err := Projects(client).List(context.Background(), "1111111111111111aaaaaaaa")
	if err != nil {
		t.Fatalf("listing projects: %v", err)
	}
	byName := map[string]Row{}
	for _, one := range rows {
		byName[one.Name()] = one
	}

	// id, name, workspace, repository, running, asking, deploys to, age.
	bills, found := byName["house-bills"]
	if !found {
		t.Fatalf("the listing does not hold house-bills: %v", byName)
	}
	if bills.Cells[3] != "atlantic-blue/house-bills" {
		t.Fatalf("house-bills says its repository is %q", bills.Cells[3])
	}
	if bills.Cells[4] != "1" {
		t.Fatalf("house-bills says %q is running, want the one running job", bills.Cells[4])
	}
	if bills.Cells[5] != "1 asking" {
		t.Fatalf("house-bills says %q about work waiting for a person, want 1 asking", bills.Cells[5])
	}

	lawn := byName["gardening"]
	if lawn.Cells[4] != "-" {
		t.Fatalf("gardening says %q is running, want a dash: its one job is done", lawn.Cells[4])
	}
	if lawn.Cells[5] != "" {
		t.Fatalf("gardening says %q in the asking column, want nothing at all", lawn.Cells[5])
	}
}

// A project that names no repository says so rather than leaving a hole in the row.
func TestAProjectWithNoRepositorySaysSoRatherThanLeavingTheCellEmpty(t *testing.T) {
	client := aSystemWithOneOfEverything()

	rows, err := Projects(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing projects: %v", err)
	}
	// The literal rather than the constant, because a case reading the constant passes against it
	// emptied out, which is the one mistake this is here to catch.
	if rows[0].Cells[3] != "-" {
		t.Fatalf(`the repository cell says %q, want "-"`, rows[0].Cells[3])
	}
}

// A system that will not answer for its jobs still draws its workspaces. Counts beside a name are
// worth having and never worth an error screen where the listing used to be.
func TestTheCountsGoMissingRatherThanTakingTheListingWithThem(t *testing.T) {
	client := &countlessClient{treeClient: aBusySystem()}

	rows, err := Workspaces(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("a system that cannot count its jobs refused the whole listing: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the console lists %d workspaces, want both of them", len(rows))
	}
	if rows[0].Cells[3] != "-" || rows[0].Cells[4] != "" {
		t.Fatalf("the counts are %q and %q, want nothing claimed", rows[0].Cells[3], rows[0].Cells[4])
	}
}

// countlessClient answers for everything except its jobs, which is a control plane too old for the
// call or one that is having a bad day.
type countlessClient struct {
	*treeClient
}

func (c *countlessClient) ListJobs(context.Context, *quaycrewv1.ListJobsRequest, ...grpc.CallOption) (*quaycrewv1.ListJobsResponse, error) {
	return nil, errors.New("unavailable")
}

// A job waiting for a person is marked in the first column, on every screen, whether or not the
// terminal has colour. A colour alone is a claim that disappears on a monochrome terminal and in a
// listing where half the rows are yellow already.
func TestAJobWaitingForAPersonIsMarkedInTheRowItself(t *testing.T) {
	asking := jobRow(aJob("1111111111111111aaaaaaaa", job.PhaseAsking, nil), 0)
	if asking.Cells[0] != "?" {
		t.Fatalf("a job waiting for a person carries %q in its first cell, want the mark", asking.Cells[0])
	}
	for _, quiet := range []string{job.PhaseRunning, job.PhaseDone, job.PhasePending, job.PhaseFailed} {
		if got := jobRow(aJob("1111111111111111aaaaaaaa", quiet, nil), 0); got.Cells[0] != "" {
			t.Fatalf("a %s job carries %q in the mark column, want nothing", quiet, got.Cells[0])
		}
	}
}

// The mark has to reach the screen, not only the row, and it has to survive a window too narrow for
// the widest row: the whole reason it is there is to be seen.
func TestTheAskingMarkIsDrawnAndNeverGivesWay(t *testing.T) {
	client := aBusySystem()
	model := openedOnTheTree(t, client)
	// Into acme, then past gardening to house-bills, which is the project holding the job that is
	// waiting for a person.
	model = walk(t, model, enter())
	model = walk(t, model, runes("j"))
	model = walk(t, model, enter())

	// The row itself, not the screen: the header carries a question mark of its own, and a case that
	// only looked at the whole view would pass against the column deleted.
	for _, width := range []int{120, 60, 40} {
		sized, _ := update(t, model, tea.WindowSizeMsg{Width: width, Height: 30})
		drawn := sized.View()
		var theRow string
		for _, line := range strings.Split(drawn, "\n") {
			if strings.Contains(line, "aaaaaaaa") {
				theRow = line
			}
		}
		if theRow == "" {
			t.Fatalf("at %d columns the job waiting for a person is not on the screen at all:\n%s", width, drawn)
		}
		if !strings.Contains(theRow, "?") {
			t.Fatalf("at %d columns the mark is gone from the row: %q", width, theRow)
		}
	}
}

// And said once above the columns, because a listing longer than the screen hides every mark below
// the fold.
func TestTheJobsLevelSaysHowManyAreWaitingForAPerson(t *testing.T) {
	client := aBusySystem()

	line, state := Jobs(client).Summary(context.Background(), "2222222222222222bbbbbbbb")
	if line != "1 job is waiting for a person" {
		t.Fatalf("the line above the columns says %q", line)
	}
	if state != StateBusy {
		t.Fatalf("the line is drawn as %v, want busy", state)
	}

	// Nothing waiting draws nothing. A line saying nobody is waiting is a line an operator learns to
	// stop reading, and this is the one place the fact is announced.
	quiet := aSystemWithOneOfEverything()
	if line, _ := Jobs(quiet).Summary(context.Background(), ""); line != "" {
		t.Fatalf("with nothing waiting the line says %q, want nothing drawn at all", line)
	}
}

func TestTheJobsSummarySaysJobsWhenThereIsMoreThanOne(t *testing.T) {
	client := aBusySystem()
	client.jobs = append(client.jobs, &quaycrewv1.Job{
		Id: "cccccccccccccccc33333333", Workspace: "1111111111111111aaaaaaaa",
		Project: "2222222222222222bbbbbbbb", Title: "answer the other question", Phase: job.PhaseAsking,
	})

	line, _ := Jobs(client).Summary(context.Background(), "2222222222222222bbbbbbbb")
	if line != "2 jobs are waiting for a person" {
		t.Fatalf("the line above the columns says %q", line)
	}
}
