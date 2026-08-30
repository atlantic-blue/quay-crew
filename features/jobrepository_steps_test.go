package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/cucumber/godog"
)

// A job that names a repository ends in a pull request against it.
//
// The two halves are here together because they are one behaviour: the declaration says where the
// work goes, and the controller holds the answer to it.

func initializeJobRepositorySteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller declares a job in the repository "([^"]*)"$`,
		func(ctx context.Context, repository string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
				Repository: repository,
			})
		})

	sc.Step(`^a job titled "([^"]*)" in the repository "([^"]*)"$`,
		func(ctx context.Context, title, repository string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "make the listing sort by the clock it shows", Repository: repository,
			})
		})

	sc.Step(`^the job works in "([^"]*)"$`, func(ctx context.Context, want string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetRepository() != want {
			return fmt.Errorf("the job works in %q, want %q", one.GetRepository(), want)
		}
		return nil
	})

	sc.Step(`^the system refuses it and says how to write a repository$`,
		theRefusalSays("atlantic-blue/quay-crew"))

	sc.Step(`^the model will answer "([^"]*)"$`, func(ctx context.Context, answer string) error {
		worldFrom(ctx).runner.willSay(answer)
		return nil
	})

	sc.Step(`^then the model will answer "([^"]*)"$`, func(ctx context.Context, answer string) error {
		worldFrom(ctx).runner.willSay(answer)
		return nil
	})

	// The system says how the job ends, rather than leaving it to whoever wrote the brief. It says not
	// to merge in the same breath, because the merge is the gate: a push applies nothing, and a merge
	// runs the pipeline.
	sc.Step(`^the session was asked to open a pull request against "([^"]*)", and not to merge$`,
		func(ctx context.Context, repository string) error {
			asked, err := taskAsking(ctx, 0)
			if err != nil {
				return err
			}
			for _, phrase := range []string{repository, "pull request", "Do not merge"} {
				if !strings.Contains(asked, phrase) {
					return fmt.Errorf("the session was asked %q, want it to say %q", asked, phrase)
				}
			}
			return nil
		})

	sc.Step(`^the session was asked again for the pull request against "([^"]*)"$`,
		func(ctx context.Context, repository string) error {
			asked, err := taskAsking(ctx, 1)
			if err != nil {
				return err
			}
			for _, phrase := range []string{repository, "no pull request", "Do not merge"} {
				if !strings.Contains(asked, phrase) {
					return fmt.Errorf("the session was asked again %q, want it to say %q", asked, phrase)
				}
			}
			return nil
		})

	sc.Step(`^the job is done, and it names the pull request "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseDone {
				return fmt.Errorf("the job is %q saying %q, want done", one.GetPhase(), one.GetReason())
			}
			if one.GetPullRequest() != want {
				return fmt.Errorf("the job names the pull request %q, want %q", one.GetPullRequest(), want)
			}
			return nil
		})

	sc.Step(`^the job is stopped, and the reason names the repository "([^"]*)"$`,
		func(ctx context.Context, repository string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
			}
			if !strings.Contains(one.GetReason(), repository) {
				return fmt.Errorf("the reason is %q, want it to name %q", one.GetReason(), repository)
			}
			return nil
		})
}

// taskAsking is the text of the nth task the model was asked to run, zero indexed.
//
// Waited for rather than read once. A dispatch that lets go answers before the model is called, so
// reading straight after the tick asks whether a goroutine has been scheduled yet, which is a
// question about the machine rather than about the system.
func taskAsking(ctx context.Context, which int) (string, error) {
	runner := worldFrom(ctx).runner
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if asked, found := runner.task(which); found {
			return asked.Text, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return "", fmt.Errorf("task %d never reached the model runner; %d did", which+1, runner.count())
}
