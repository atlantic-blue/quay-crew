package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job ends by stating one outcome from a fixed set, and the system reads that word rather than the
// prose around it.
//
// These steps drive the whole path: the session is told what to write, it writes it or it does not,
// and what a caller reads afterwards is a field. The assertions go past the phase, because the phase
// is what could not tell two jobs apart.

func initializeOutcomeSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the model will answer "([^"]*)" and state no outcome$`,
		func(ctx context.Context, answer string) error {
			worldFrom(ctx).runner.willSayExactly(answer)
			return nil
		})

	sc.Step(`^the model will answer "([^"]*)" and state the outcome "([^"]*)"$`,
		func(ctx context.Context, answer, outcome string) error {
			worldFrom(ctx).runner.willSay(answer + "\n\n" + job.OutcomeMarker + " " + outcome)
			return nil
		})

	sc.Step(`^the model will answer "([^"]*)" and end on the line "([^"]*)"$`,
		func(ctx context.Context, answer, line string) error {
			worldFrom(ctx).runner.willSayExactly(answer + "\n\n" + line)
			return nil
		})

	sc.Step(`^the model refuses the task it is given$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.failTheNextTask()
		return nil
	})

	sc.Step(`^the session was told to end its answer with an outcome$`, func(ctx context.Context) error {
		asked, err := taskAsking(ctx, 0)
		if err != nil {
			return err
		}
		if !strings.Contains(asked, job.OutcomeMarker) {
			return fmt.Errorf("the session was asked %q, want it to say to write the outcome line", asked)
		}
		return nil
	})

	sc.Step(`^the session was offered the four words$`, func(ctx context.Context) error {
		asked, err := taskAsking(ctx, 0)
		if err != nil {
			return err
		}
		for _, word := range job.Outcomes() {
			if !strings.Contains(asked, word) {
				return fmt.Errorf("the session was asked %q, want it to offer %q", asked, word)
			}
		}
		return nil
	})

	sc.Step(`^the job did not settle, and says the outcome line was missing$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() == job.PhaseDone {
			return fmt.Errorf("the job is done on the answer %q, which states no outcome", one.GetAnswer())
		}
		if one.GetOutcome() != "" {
			return fmt.Errorf("the job ended on %q, and its answer stated nothing", one.GetOutcome())
		}
		if !strings.Contains(one.GetReason(), job.OutcomeMarker) {
			return fmt.Errorf("the job says %q, want it to say what line was missing", one.GetReason())
		}
		return nil
	})

	// The end of an attempt is not the end of what it produced. A reader has to be able to see what
	// the session actually said, which is the half a refusal on its own would take away.
	sc.Step(`^what the session said is still on the record$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetAnswer() == "" {
			return fmt.Errorf("the job carries no answer, so what the session said is nowhere")
		}
		return nil
	})

	sc.Step(`^the job is done, and it ended on "([^"]*)"$`, func(ctx context.Context, outcome string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseDone {
			return fmt.Errorf("the job is %q saying %q, want done", one.GetPhase(), one.GetReason())
		}
		if one.GetOutcome() != outcome {
			return fmt.Errorf("the job ended on %q, want %q", one.GetOutcome(), outcome)
		}
		return nil
	})

	sc.Step(`^the job is failed, and it ended on no outcome$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseFailed {
			return fmt.Errorf("the job is %q, want failed", one.GetPhase())
		}
		if one.GetOutcome() != "" {
			return fmt.Errorf("the failed job ended on %q, and nothing stated one", one.GetOutcome())
		}
		return nil
	})

	// A job driven all the way through, because the listing this is about is a listing of jobs that
	// ran. Seeding the field would prove the filter against a row nothing wrote.
	sc.Step(`^a job titled "([^"]*)" that ended on "([^"]*)"$`,
		func(ctx context.Context, title, outcome string) error {
			w := worldFrom(ctx)
			w.runner.willSay("it is done\n\n" + job.OutcomeMarker + " " + outcome)
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "open it and say when it is due",
			}); err != nil {
				return err
			}
			if w.lastErr != nil {
				return w.lastErr
			}
			w.server.TickJob(ctx)
			if err := w.settled(ctx); err != nil {
				return err
			}
			w.server.TickJob(ctx)
			return nil
		})

	// The refusal is kept rather than returned, because a scenario about one has to be able to read it.
	sc.Step(`^the caller lists the jobs that ended on "([^"]*)"$`, func(ctx context.Context, outcome string) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Outcome: outcome})
		w.lastErr = err
		if err == nil {
			jobFrom(ctx).listed = listed.GetJobs()
		}
		return nil
	})

	sc.Step(`^the listing holds "([^"]*)" and nothing else$`, func(ctx context.Context, title string) error {
		listed := jobFrom(ctx).listed
		if len(listed) != 1 {
			return fmt.Errorf("the listing holds %d jobs, want the one that ended that way", len(listed))
		}
		if listed[0].GetTitle() != title {
			return fmt.Errorf("the listing holds %q, want %q", listed[0].GetTitle(), title)
		}
		return nil
	})

	// Both are done, which is the point: the phase cannot tell them apart, and that is the reading
	// this behaviour exists to end.
	sc.Step(`^listing by phase holds both of them$`, func(ctx context.Context) error {
		if err := listJob(ctx, &quaycrewv1.ListJobsRequest{Phase: job.PhaseDone}); err != nil {
			return err
		}
		if got := len(jobFrom(ctx).listed); got != 2 {
			return fmt.Errorf("the done jobs are %d rows, want both of them", got)
		}
		return nil
	})

	sc.Step(`^the system refuses it and offers the four words$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the listing was answered, and a word nothing ends on should be refused")
		}
		for _, word := range job.Outcomes() {
			if !strings.Contains(w.lastErr.Error(), word) {
				return fmt.Errorf("the refusal says %q, want it to offer %q", w.lastErr, word)
			}
		}
		return nil
	})
}
