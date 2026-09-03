package console

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// OneJob is the view of a single job: the job itself on the first line, and one line for each run of
// the stage it fanned out into, so a person watching sees every session working under it.
//
// Enter on a job used to open the tasks of that job's session, which is one level past the thing a
// person pointed at, and it refused a job that had reached no session at all. A job that stopped for
// an answer was worse: the question was on the row, and the answer had to be typed at the command
// line or into the web page, which is the one place the person watching was not looking.
//
// The line above the columns is that question, and what the job was told once somebody answers it.
func OneJob(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		// It takes no word of its own in the command bar: it is read of one job, so a person reaches
		// it by opening a job rather than by typing. The word jobs already answers to job and to j.
		Name:    "onejob",
		Columns: jobColumns(),
		// The order the system answers in, the same as the listing above it. The job is first and its
		// runs follow in the order the system holds them.
		SortBy:  -1,
		Summary: askingLine(client),
		Actions: []Action{answerAJob(client, true)},
		// parent is the job this view is open on. There is no unscoped form of it: a view of one job
		// with no job says so rather than listing every job in the system under a heading that
		// promises one.
		List: func(ctx context.Context, id string) ([]Row, error) {
			if id == "" {
				return nil, fmt.Errorf("a view of one job needs a job: open one from the jobs listing")
			}
			answer, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
			if err != nil {
				return nil, err
			}
			rows := []Row{jobRow(answer.GetJob())}

			runs, err := client.ListExecutions(ctx, &quaycrewv1.ListExecutionsRequest{Job: id})
			if err != nil {
				return nil, err
			}
			for _, run := range runs.GetExecutions() {
				line := executionRow(run)
				// Every row here belongs to the job this view is open on, so nothing is folded away
				// under anything: the runs are what a person came to watch.
				line.Under = ""
				rows = append(rows, line)
			}
			return rows, nil
		},
	}
}

// askingLine is the line above the columns: what the job is waiting to be told, and what it was told
// once somebody answered. The words are the ones `krewe job show` prints, so the console and the
// command line cannot say two different things about one job.
//
// Both stay on the line after the answer. An answer on its own says nothing, and a question on its
// own leaves a reader looking for the decision. The answer is written first because the line is cut
// at the right edge of the window: a person who has just typed an answer is looking for it, and a
// long question would push it off a narrow screen.
func askingLine(client quaycrewv1.ControlPlaneServiceClient) Summariser {
	return func(ctx context.Context, id string) (string, State) {
		if id == "" {
			return "", StateUnknown
		}
		answer, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err != nil {
			return "", StateUnknown
		}
		return askedAndTold(answer.GetJob())
	}
}

func askedAndTold(one *quaycrewv1.Job) (string, State) {
	asked, told := oneLine(one.GetQuestion()), oneLine(one.GetTold())
	switch {
	case asked != "" && one.GetPhase() == job.PhaseAsking:
		return "asking: " + asked, StateBusy
	case asked != "" && told != "":
		return "told: " + told + "   asked: " + asked, StateReady
	case told != "":
		return "told: " + told, StateReady
	case asked != "":
		return "asked: " + asked, StateUnknown
	default:
		return "", StateUnknown
	}
}
