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

// The requirements a person accepted becoming failing tests, driven through the same calls both
// sides use: the crew answers with a list, a person accepts it, and one worker for each requirement
// writes the tests and reports on its run.
//
// The assertions go past the row where they can. What decides whether this works is what each worker
// was handed and what a person is left holding, so these read the tasks the system really sent and
// the reading the tool really prints.

// aRunThatExecutedNothing and aRunWhereEverythingPassed are the two shapes of false green this stage
// exists to refuse. Both read as success everywhere else in this system.
// Each ends with an outcome, because every task asks for one and a worker that states none stops for
// that instead, which would be a scenario about the outcome line rather than about the suite.
const (
	aRunThatExecutedNothing = "I wrote the tests.\n\nRequirement: 1\nRan: 0\nFailing 1: TestNothingRan" +
		"\n\nOutcome: proved"
	aRunWhereEverythingPassed = "I wrote the tests and they all pass.\n\nRequirement: 1\nRan: 4" +
		"\n\nOutcome: proved"
)

func initializeTestStageSteps(sc *godog.ScenarioContext) {
	// A job past its accepted list, which is where every scenario here starts. The list is the
	// requirement list: there is no second record of what this job would build.
	sc.Step(`^a job whose list of (\d+) verticals? a person accepted$`,
		func(ctx context.Context, verticals int) error {
			return aListAPersonAccepted(ctx, verticals, "")
		})

	// The same job in a repository, which is where its tests have somewhere to go.
	sc.Step(`^a job in a repository whose list of (\d+) verticals? a person accepted$`,
		func(ctx context.Context, verticals int) error {
			return aListAPersonAccepted(ctx, verticals, "atlantic-blue/quay-crew")
		})

	// Where the tests have to end up. The worker writes them in a sandbox of its own, so a worker
	// that never commits them writes tests nobody in the next stage can open. The branch belongs to
	// the requirement, so each worker is told its own and no other worker's.
	sc.Step(`^each worker is told to commit its tests to the branch its requirement's tests live on$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			runs, err := theWorkers(ctx)
			if err != nil {
				return err
			}
			held := map[int]string{}
			for _, requirement := range job.RequirementsOf(jobAsKept(one)) {
				held[requirement.Number] = job.BranchFor(jobAsKept(one), requirement)
			}
			for _, run := range runs {
				branch := held[int(run.GetNumber())]
				if branch == "" {
					return fmt.Errorf("the worker holding %q names no branch for its tests, so they go "+
						"nowhere", run.GetClaim())
				}
				// What the session was actually asked, read off its task rather than off the row: a run
				// carries no words a person wrote, and what it was sent is what it worked from.
				asked, err := whatTheSessionWasAsked(ctx, run.GetSession())
				if err != nil {
					return err
				}
				for _, needed := range []string{branch, "commit every file you write"} {
					if !strings.Contains(asked, needed) {
						return fmt.Errorf("the worker holding %q is not told %q, so its tests stay in its "+
							"own sandbox:\n%s", run.GetClaim(), needed, asked)
					}
				}
			}
			return nil
		})
	// The stage driven to its end, for the scenarios about what comes after it.
	sc.Step(`^its requirements became failing tests$`, func(ctx context.Context) error {
		return theRequirementsBecameFailingTests(ctx)
	})

	sc.Step(`^the worker will answer that its run executed no tests$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willAnswer(job.TheTestAsk, aRunThatExecutedNothing)
		return nil
	})

	sc.Step(`^the worker will answer that every test passed$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willAnswer(job.TheTestAsk, aRunWhereEverythingPassed)
		return nil
	})

	sc.Step(`^a worker is writing the tests for each requirement, and the job itself has no session$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			runs, err := theWorkers(ctx)
			if err != nil {
				return err
			}
			if len(runs) != len(job.RequirementsOf(jobAsKept(one))) {
				return fmt.Errorf("%d workers are writing tests for %d requirements",
					len(runs), len(job.RequirementsOf(jobAsKept(one))))
			}
			// The row itself buys no session for this stage. It is pending throughout, and every session
			// the stage pays for belongs to one run holding one requirement.
			if one.GetPhase() != job.PhasePending {
				return fmt.Errorf("the job is %q while its workers write: %s",
					one.GetPhase(), one.GetReason())
			}
			for _, run := range runs {
				if run.GetSession() != "" && run.GetSession() == one.GetSession() {
					return fmt.Errorf("a worker writes tests in the job's own conversation %q",
						one.GetSession())
				}
			}
			// And nothing the fan out wrote stands in the listing of declared work.
			return noRunsInTheJobsListing(ctx, one)
		})

	sc.Step(`^each worker was given its own requirement and told not to implement it$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			wanted := job.RequirementsOf(jobAsKept(one))
			runs, err := theWorkers(ctx)
			if err != nil {
				return err
			}
			for _, run := range runs {
				asked, err := whatTheSessionWasAsked(ctx, run.GetSession())
				if err != nil {
					return err
				}
				mine, others := "", 0
				for _, requirement := range wanted {
					if run.GetClaim() == job.ClaimOnRequirement(one.GetId(), requirement) {
						mine = requirement.Text
						continue
					}
					if strings.Contains(asked, requirement.Text) {
						others++
					}
				}
				if mine == "" {
					return fmt.Errorf("the run of number %d claims %q, which is no requirement of this job",
						run.GetNumber(), run.GetClaim())
				}
				if !strings.Contains(asked, mine) || others > 0 {
					return fmt.Errorf("the worker holding %q was given %d other requirements as well",
						mine, others)
				}
				if !strings.Contains(asked, "Do not implement") {
					return fmt.Errorf("the worker holding %q was not told to leave the implementation "+
						"alone: %q", mine, asked)
				}
			}
			return nil
		})

	// Every worker runs and answers. It takes as many ticks as it takes: a run is claimed, dispatched
	// and landed on its own passes, the way a job is.
	sc.Step(`^every worker answers with its run$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		for range 5 {
			w.server.TickJob(ctx)
			if err := w.settled(ctx); err != nil {
				return err
			}
			runs, err := theWorkers(ctx)
			if err != nil {
				return err
			}
			done := 0
			for _, run := range runs {
				if job.Terminal(run.GetPhase()) {
					done++
				}
			}
			if len(runs) > 0 && done == len(runs) {
				return nil
			}
		}
		return fmt.Errorf("the workers never finished")
	})

	sc.Step(`^the worker for requirement (\d+) dies$`, func(ctx context.Context, requirement int) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		wanted := job.RequirementsOf(jobAsKept(one))
		if len(wanted) < requirement {
			return fmt.Errorf("this job has %d requirements, so it has no %dth", len(wanted), requirement)
		}
		claim := job.ClaimOnRequirement(one.GetId(), wanted[requirement-1])
		runs, err := theWorkers(ctx)
		if err != nil {
			return err
		}
		for _, run := range runs {
			if run.GetClaim() != claim {
				continue
			}
			_, err := w.client.StopExecution(ctx, &quaycrewv1.StopExecutionRequest{
				Id: run.GetId(), Reason: "the sandbox went away",
			})
			return err
		}
		return fmt.Errorf("no worker holds requirement %d", requirement)
	})

	sc.Step(`^(\d+) workers? (?:are|is) writing tests, one for each requirement$`,
		func(ctx context.Context, want int) error {
			runs, err := theWorkers(ctx)
			if err != nil {
				return err
			}
			if len(runs) != want {
				return fmt.Errorf("%d workers hold requirements of this job, want %d", len(runs), want)
			}
			claims := map[string]bool{}
			for _, run := range runs {
				if claims[run.GetClaim()] {
					return fmt.Errorf("two workers claim %q, so both write the same requirement",
						run.GetClaim())
				}
				claims[run.GetClaim()] = true
			}
			return nil
		})

	sc.Step(`^the row carries a failing test for every requirement$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		wanted := job.RequirementsOf(jobAsKept(one))
		requirements, failing := job.TestsOn(one.GetTests())
		if requirements != len(wanted) || failing < len(wanted) {
			return fmt.Errorf("the record covers %d of %d requirements with %d failing tests: %q",
				requirements, len(wanted), failing, one.GetTests())
		}
		return nil
	})

	sc.Step(`^every failure says which requirement it came from$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for _, requirement := range job.RequirementsOf(jobAsKept(one)) {
			fails := fmt.Sprintf("Fails %d:", requirement.Number)
			if !strings.Contains(one.GetTests(), fails) {
				return fmt.Errorf("no failure on the row opens with %q: %q", fails, one.GetTests())
			}
		}
		return nil
	})

	sc.Step(`^the job is asking, and the row carries no failing tests$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q, want asking: %s", one.GetPhase(), one.GetReason())
		}
		if one.GetTests() != "" {
			return fmt.Errorf("a suite that is not red closed the stage anyway: %q", one.GetTests())
		}
		return nil
	})

	sc.Step(`^the question says the run found nothing to execute$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "finds nothing to execute")
	})

	sc.Step(`^the question says nothing failed$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "none of them failed")
	})

	sc.Step(`^the question names requirement (\d+)$`, func(ctx context.Context, requirement int) error {
		return theQuestionSays(ctx, fmt.Sprintf("requirement %d", requirement))
	})

	// The whole of the split, said at the surface a person reads. A run is not declared work, so the
	// listing of declared work carries the one row somebody wrote and no other.
	sc.Step(`^the jobs listing carries only the job somebody declared$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		return noRunsInTheJobsListing(ctx, one)
	})

	// The number a person watching a fan out is looking straight at. The job has no session of its
	// own, so without this its row says it has none yet while a session for every requirement works.
	sc.Step(`^the job says (\d+) of its runs are working$`, func(ctx context.Context, want int) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if int(one.GetRunningExecutions()) != want {
			return fmt.Errorf("the job says %d of its runs are working, want %d",
				one.GetRunningExecutions(), want)
		}
		return nil
	})

	// And what a run is, off the wire: the job it belongs to and the stage of that job it runs. Those
	// two are what the stage gathers by, and what says a run is never read as a job.
	sc.Step(`^each run says which job and which stage it is a run of$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		runs, err := theWorkers(ctx)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			return fmt.Errorf("this job has no runs, so this checked nothing")
		}
		for _, run := range runs {
			if run.GetJob() != one.GetId() {
				return fmt.Errorf("a run belongs to job %q, and this job is %q", run.GetJob(), one.GetId())
			}
			if run.GetStage() != job.StageTest {
				return fmt.Errorf("a run of the test stage says it is a run of the %q stage", run.GetStage())
			}
			if run.GetNumber() < 1 {
				return fmt.Errorf("a run says it is for number %d, and a requirement counts from one",
					run.GetNumber())
			}
		}
		return nil
	})

	sc.Step(`^the reading carries a failing test for every requirement$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		out := toolFrom(ctx).stdout
		for _, requirement := range job.RequirementsOf(jobAsKept(one)) {
			if !strings.Contains(out, fmt.Sprintf("Fails %d:", requirement.Number)) {
				return fmt.Errorf("the reading says nothing about requirement %d failing: %s",
					requirement.Number, out)
			}
		}
		return nil
	})
}

// theWorkers is every run this job's test stage made, read as the runs of that stage.
func theWorkers(ctx context.Context) ([]*quaycrewv1.Execution, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	listed, err := worldFrom(ctx).client.ListExecutions(ctx, &quaycrewv1.ListExecutionsRequest{
		Job: one.GetId(), Stage: job.StageTest,
	})
	if err != nil {
		return nil, err
	}
	return listed.GetExecutions(), nil
}

// whatTheSessionWasAsked is the text of the last task one session was sent. A run carries no words a
// person wrote, so this is where what it worked from is read.
func whatTheSessionWasAsked(ctx context.Context, session string) (string, error) {
	if session == "" {
		return "", fmt.Errorf("this run holds no session, so nothing was asked of it")
	}
	tasks, err := worldFrom(ctx).client.ListTasks(ctx,
		&quaycrewv1.ListTasksRequest{Session: session})
	if err != nil {
		return "", err
	}
	if len(tasks.GetTasks()) == 0 {
		return "", fmt.Errorf("session %s was sent no task", session)
	}
	return tasks.GetTasks()[len(tasks.GetTasks())-1].GetPrompt(), nil
}

// noRunsInTheJobsListing refuses a listing of declared work that carries a row nobody declared.
//
// It is the whole point of the split, and it is checked here rather than only in the store because
// this is the surface a person reads: a job with five runs under it used to be six rows.
func noRunsInTheJobsListing(ctx context.Context, one *quaycrewv1.Job) error {
	listed, err := worldFrom(ctx).client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Project: one.GetProject(),
	})
	if err != nil {
		return err
	}
	for _, row := range listed.GetJobs() {
		if row.GetId() != one.GetId() {
			return fmt.Errorf("the jobs listing carries %q, %q, which nobody declared",
				row.GetId(), row.GetTitle())
		}
	}
	return nil
}

// theQuestionSays holds what a person was asked to a phrase, because the question is the whole of
// what they have to act on.
func theQuestionSays(ctx context.Context, phrase string) error {
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	if !strings.Contains(one.GetQuestion(), phrase) {
		return fmt.Errorf("the question is %q, want it to say %q", one.GetQuestion(), phrase)
	}
	return nil
}

// jobAsKept is a job off the wire as the package that decides these things reads it, so a step and
// the system cannot say two different things about one row.
func jobAsKept(one *quaycrewv1.Job) *job.Job {
	return &job.Job{
		ID: one.GetId(), Product: one.GetProduct(), Parent: one.GetParent(),
		Repository:     one.GetRepository(),
		IdeationAnswer: one.GetIdeationAnswer(),
		Design:         one.GetDesign(), DesignAccepted: one.GetDesignAccepted(),
		Tests: one.GetTests(), Build: one.GetBuild(), Accepted: one.GetAccepted(),
		Plan: one.GetPlan(), PlanApproved: one.GetPlanApproved(),
	}
}

// theRequirementsBecameFailingTests drives the whole test stage to its end: the fan out, the workers
// writing, and the record landing on the row.
//
// It is what the stages after this one start from, the way they already start from an answered
// reading and an accepted list. A job whose suite is not red is never asked for a plan, so a scenario
// about the plan that skipped this would be a scenario about a job that cannot reach it.
func theRequirementsBecameFailingTests(ctx context.Context) error {
	w := worldFrom(ctx)
	for range 8 {
		w.server.TickJob(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetTests() != "" {
			return nil
		}
		if one.GetPhase() == job.PhaseAsking {
			return fmt.Errorf("the job stopped in the test stage: %s", one.GetQuestion())
		}
	}
	return fmt.Errorf("the requirements never became failing tests")
}

// aListAPersonAccepted is a job driven to the point every scenario here starts from: it read the
// work, it said what it would build, and a person accepted the list.
//
// The repository is empty for a job that works in none. A job that names one is the same job with
// somewhere for its tests to go, and it needs the mode that reaches the network, because every way
// into a repository is a command that needs it.
func aListAPersonAccepted(ctx context.Context, verticals int, repository string) error {
	w := worldFrom(ctx)
	// Matched on the ask rather than queued by position: this job answers a reading first, and a
	// queue by position would hand the list to that question instead.
	if verticals == 1 {
		w.runner.willAnswer(job.TheDesignAsk, oneVertical)
	}
	declared := &quaycrewv1.CreateJobRequest{
		Title: "the transcript page", Brief: "build what the design describes",
		Product: "pastes a link and gets the text back",
	}
	if repository != "" {
		declared.Repository, declared.Mode = repository, model.PermissionModeOnTheNetwork()
	}
	if err := declareJob(ctx, declared); err != nil {
		return err
	}
	if w.lastErr != nil {
		return w.lastErr
	}
	if err := aPersonAnsweredTheReading(ctx); err != nil {
		return err
	}
	if err := aPersonAcceptedTheList(ctx); err != nil {
		return err
	}
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	if got := len(job.RequirementsOf(jobAsKept(one))); got != verticals {
		return fmt.Errorf("the accepted list carries %d requirements, want %d: %q",
			got, verticals, one.GetDesign())
	}
	return nil
}
