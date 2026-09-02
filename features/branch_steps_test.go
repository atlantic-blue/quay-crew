package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/cucumber/godog"
)

// One branch carrying a requirement from its failing tests to the build that turns them green,
// driven through the same calls both sides use.
//
// The fault behind these: the tests a worker wrote lived in that worker's sandbox and nowhere else,
// so the worker that built the same requirement was told to read files that were not there.

// theRepositoryTheseJobsWorkIn is where a branch is pushed to. It is a real address because the
// system reads a pull request address off an answer, and it reads one against this repository and no
// other.
const theRepositoryTheseJobsWorkIn = "atlantic-blue/quay-krewe"

// aRunThatOpenedNothing is a report that is perfect and says nothing about where the tests went. It
// is what a worker answered before this, and the files it describes are in a sandbox that has gone.
const aRunThatOpenedNothing = "I wrote the tests for requirement 1 and ran the suite.\n\n" +
	"Requirement: 1\nRan: 12\nFailing 1: TestRequirement1FailsUntilSomethingBuildsIt\n\nOutcome: proved"

func initializeBranchSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job in a repository whose list of (\d+) verticals? a person accepted$`,
		func(ctx context.Context, verticals int) error {
			w := worldFrom(ctx)
			if verticals == 1 {
				w.runner.willAnswer(job.TheDesignAsk, oneVertical)
			}
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: "the transcript page", Brief: "build what the design describes",
				Product: "pastes a link and gets the text back", Repository: theRepositoryTheseJobsWorkIn,
				Mode: model.PermissionModeOnTheNetwork(),
			}); err != nil {
				return err
			}
			if w.lastErr != nil {
				return w.lastErr
			}
			if err := aPersonAnsweredTheReading(ctx); err != nil {
				return err
			}
			return aPersonAcceptedTheList(ctx)
		})

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

	sc.Step(`^a person approves the plan$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.runner.willSay(thePlanTheCrewWrote)
		for range 6 {
			w.server.TickJob(ctx)
			if err := w.settled(ctx); err != nil {
				return err
			}
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPlanApproved() {
				break
			}
			if one.GetPhase() == job.PhaseAsking && one.GetPlan() != "" {
				if _, err := w.client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
					Id: one.GetId(), Answer: "yes",
				}); err != nil {
					return err
				}
			}
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if !one.GetPlanApproved() {
			return fmt.Errorf("the plan is not approved: the job is %q asking %q",
				one.GetPhase(), one.GetQuestion())
		}
		// And the build stage then fans out, which is what the assertions below read.
		for range 3 {
			w.server.TickJob(ctx)
			if err := w.settled(ctx); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the worker building each vertical is on the branch that vertical's tests are on$`,
		func(ctx context.Context) error {
			return eachBuilder(ctx, func(one *quaycrewv1.Job, worker *quaycrewv1.Job,
				vertical job.Requirement) error {
				branch := job.BranchForRequirement(one.GetId(), vertical)
				if worker.GetBranch() != branch {
					return fmt.Errorf("the worker building vertical %d is on %q, and its tests are on %q",
						vertical.Number, worker.GetBranch(), branch)
				}
				return nil
			})
		})

	sc.Step(`^the worker building each vertical is told to check those tests out before it starts$`,
		func(ctx context.Context) error {
			return eachBuilder(ctx, func(one *quaycrewv1.Job, worker *quaycrewv1.Job,
				vertical job.Requirement) error {
				branch := job.BranchForRequirement(one.GetId(), vertical)
				if !strings.Contains(worker.GetBrief(), "git switch "+branch) {
					return fmt.Errorf("the worker building vertical %d is not told to check out %q: %s",
						vertical.Number, branch, worker.GetBrief())
				}
				return nil
			})
		})

	sc.Step(`^each vertical has one pull request, opened by the worker that wrote its tests$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			workers, err := theWorkers(ctx)
			if err != nil {
				return err
			}
			for _, vertical := range job.RequirementsOf(jobAsKept(one)) {
				opened := map[string]bool{}
				for _, worker := range workers {
					held := worker.GetClaim()
					if held != job.ClaimOnRequirement(one.GetId(), vertical) &&
						held != job.ClaimOnBuild(one.GetId(), vertical) {
						continue
					}
					if worker.GetPullRequest() != "" {
						opened[worker.GetPullRequest()] = true
					}
					if held == job.ClaimOnBuild(one.GetId(), vertical) &&
						!strings.Contains(worker.GetBrief(), "Do not open a second pull request") {
						return fmt.Errorf("the worker building vertical %d is not told to stay in the pull "+
							"request its tests are open in: %s", vertical.Number, worker.GetBrief())
					}
				}
				if len(opened) != 1 {
					return fmt.Errorf("vertical %d has %d pull requests: %v", vertical.Number,
						len(opened), opened)
				}
			}
			return nil
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

// eachBuilder is the same over every worker that builds one.
func eachBuilder(ctx context.Context,
	check func(one *quaycrewv1.Job, worker *quaycrewv1.Job, vertical job.Requirement) error) error {
	return eachWorkerHolding(ctx, job.ClaimOnBuild, check)
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
