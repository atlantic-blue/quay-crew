package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A session stopping for a person, driven the way the incident happened: nobody calls anything, the
// session simply answers, and what the record does with that answer is the whole question.
//
// The assertions read the listing rather than the row wherever they can, because the complaint was
// never "this job is mislabelled". It was that nothing said which of four jobs needed anybody.

// theStoreDecision is what a session writes under the line when the work stops at a choice no
// measurement settles.
const theStoreDecision = "The store for the transcripts. One bills a minimum capacity " +
	"continuously, the other bills nothing at rest. Which do you want?"

func initializeDecidingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job whose session answers that a person has to decide$`, func(ctx context.Context) error {
		return aJobAnswering(ctx, theStoreDecision+"\n\n"+job.OutcomeMarker+" "+job.OutcomeDecide)
	})

	sc.Step(`^a job whose session answers that the work is done$`, func(ctx context.Context) error {
		return aJobAnswering(ctx, "The bill is due on the 14th.\n\n"+job.OutcomeMarker+" "+job.OutcomeProved)
	})

	// The session is held inside its task, which is what a session doing work is. Nothing has been
	// answered, so there is nothing for the record to read.
	sc.Step(`^a job whose session is still working on it$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.release = w.runner.hold()
		if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it and say when it is due",
		}); err != nil {
			return err
		}
		w.server.TickJob(ctx)
		return w.runner.waitForTask()
	})

	sc.Step(`^the controller reads what that session answered$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		w.server.TickJob(ctx)
		return nil
	})

	// The question is held against what the double was told to answer, so one step serves a scenario
	// that names its own words and a scenario that takes the ones here.
	sc.Step(`^the job is waiting on a person, carrying what the session wrote$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q saying %q, want asking: a person has to decide and the "+
				"record says work is happening", one.GetPhase(), one.GetReason())
		}
		wrote := job.WithoutTheOutcome(worldFrom(ctx).runner.lastSaid())
		if one.GetQuestion() != wrote {
			return fmt.Errorf("the row carries the question %q, want %q", one.GetQuestion(), wrote)
		}
		return nil
	})

	// A job waiting on a person is nobody's to hold. A lease still running on it would have a
	// controller renewing a hold on work that is not moving.

	// The record carries the moment, so the gap between a job stopping and a person hearing about it
	// can be measured rather than guessed at.
	sc.Step(`^the record for that job says a question was put$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		records, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.Kind == job.EventAsked {
				return nil
			}
		}
		return fmt.Errorf("the record for this job holds no %s", job.EventAsked)
	})

	// A job stopped with a person has not finished, so nothing about it reads as an ending: no
	// finishing moment, and no word about what became of the work.
	sc.Step(`^the job did not end$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetFinishedAt() != nil {
			return fmt.Errorf("the job is marked finished, and it is waiting to be told something")
		}
		if one.GetOutcome() != "" {
			return fmt.Errorf("the job says it ended on %q, and it is waiting on a person", one.GetOutcome())
		}
		return nil
	})

	sc.Step(`^that job is still running$`, func(ctx context.Context) error {
		return jobIs(ctx, 0, job.PhaseRunning)
	})

	sc.Step(`^the question on the row does not carry the outcome line$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if strings.Contains(one.GetQuestion(), job.OutcomeMarker) {
			return fmt.Errorf("the question a person reads carries the system's own line: %q", one.GetQuestion())
		}
		return nil
	})

	sc.Step(`^(one|no) jobs? reads? as waiting on a person$`, func(ctx context.Context, how string) error {
		want := 0
		if how == "one" {
			want = 1
		}
		waiting, err := worldFrom(ctx).client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
			Phase: job.PhaseAsking,
		})
		if err != nil {
			return err
		}
		if got := len(waiting.GetJobs()); got != want {
			return fmt.Errorf("%d jobs read as waiting on a person, want %d", got, want)
		}
		return nil
	})
}

// aJobAnswering declares a job whose session answers exactly this, and waits for the answer to land.
//
// Exactly, because the double follows the instruction it is given otherwise and would add an outcome
// line of its own over the one the scenario is about.
func aJobAnswering(ctx context.Context, answer string) error {
	w := worldFrom(ctx)
	w.runner.willSayExactly(answer)
	if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
		Title: "choose where the transcripts are kept",
		Brief: "read what the project says about cost and pick the store",
	}); err != nil {
		return err
	}
	w.server.TickJob(ctx)
	return nil
}
