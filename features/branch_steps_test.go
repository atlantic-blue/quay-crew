package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// One branch carrying a requirement from its failing tests to the build that turns them green,
// driven through the same calls both sides use.
//
// The fault behind these: the tests a worker wrote lived in that worker's sandbox and nowhere else,
// so the worker that built the same requirement was told to read files that were not there.

// aRunThatOpenedNothing is a report that is perfect and says nothing about where the tests went. It
// is what a worker answered before this, and the files it describes are in a sandbox that has gone.
const aRunThatOpenedNothing = "I wrote the tests for requirement 1 and ran the suite.\n\n" +
	"Requirement: 1\nRan: 12\nFailing 1: TestRequirement1FailsUntilSomethingBuildsIt\n\nOutcome: proved"

func initializeBranchSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the worker will answer without opening a pull request$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willAnswer(job.TheTestAsk, aRunThatOpenedNothing)
		return nil
	})

	sc.Step(`^each worker was told which branch its requirement's tests go on$`,
		func(ctx context.Context) error {
			return eachTestWorker(ctx, func(one *quaycrewv1.Job, worker *quaycrewv1.Job,
				requirement job.Requirement) error {
				branch := job.BranchForRequirement(one.GetId(), requirement)
				if worker.GetBranch() != branch {
					return fmt.Errorf("the worker writing requirement %d is on %q, and its branch is %q",
						requirement.Number, worker.GetBranch(), branch)
				}
				if !strings.Contains(worker.GetBrief(), "git switch --create "+branch) {
					return fmt.Errorf("the worker writing requirement %d is not told to cut %q: %s",
						requirement.Number, branch, worker.GetBrief())
				}
				return nil
			})
		})

	sc.Step(`^each worker was told to open its pull request from that branch and leave it red$`,
		func(ctx context.Context) error {
			return eachTestWorker(ctx, func(_ *quaycrewv1.Job, worker *quaycrewv1.Job,
				requirement job.Requirement) error {
				for _, phrase := range []string{"leave it open", "Do not merge it"} {
					if !strings.Contains(worker.GetBrief(), phrase) {
						return fmt.Errorf("the worker writing requirement %d is not told %q: %s",
							requirement.Number, phrase, worker.GetBrief())
					}
				}
				return nil
			})
		})

	sc.Step(`^the question says the tests reached no branch$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "reached no branch")
	})
}

// eachTestWorker runs one check over every worker that writes a requirement's tests, and refuses a
// run that found no worker to check: a fan out that declared nothing reads as every worker passing.
func eachTestWorker(ctx context.Context,
	check func(one *quaycrewv1.Job, worker *quaycrewv1.Job, requirement job.Requirement) error) error {
	return eachWorkerHolding(ctx, job.ClaimOnRequirement, check)
}

func eachWorkerHolding(ctx context.Context, claim func(string, job.Requirement) string,
	check func(one *quaycrewv1.Job, worker *quaycrewv1.Job, requirement job.Requirement) error) error {
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	workers, err := theWorkers(ctx)
	if err != nil {
		return err
	}
	wanted := job.RequirementsOf(jobAsKept(one))
	checked := 0
	for _, requirement := range wanted {
		for _, worker := range workers {
			if worker.GetClaim() != claim(one.GetId(), requirement) {
				continue
			}
			if err := check(one, worker, requirement); err != nil {
				return err
			}
			checked++
		}
	}
	if checked != len(wanted) {
		return fmt.Errorf("%d of %d requirements have a worker holding them, so this checked nothing "+
			"about the rest", checked, len(wanted))
	}
	return nil
}
