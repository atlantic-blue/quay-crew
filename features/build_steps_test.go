package features_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// The failing tests becoming an implementation, driven through the same calls both sides use: a
// person approves the plan, one worker for each vertical builds against its own tests, and the job
// holds for that person to accept what arrived.
//
// The boundary is driven the way a sandbox runs it. Those scenarios run the entry point this build
// ships, fed the payload the model runtime feeds it, so a gate whose entry point was never built
// fails them rather than passing over nothing.

// The three shapes of false green this stage refuses. Each reads as success everywhere else in this
// system, and none of them says a vertical was built. Each ends with an outcome, because every task
// asks for one and a worker that states none stops for that instead, which would make these
// scenarios about the outcome line.
const (
	aBuildThatExecutedNothing = "I built it.\n\nVertical: 1\nRan: 0\nRed: 0\n" +
		"Passing 1: TestPastingALinkPrintsTheTranscript\nChanged 1: internal/paste.go\n\nOutcome: proved"
	aBuildThatIsStillRed = "I built some of it.\n\nVertical: 1\nRan: 14\nRed: 2\n" +
		"Passing 1: TestPastingALinkPrintsTheTranscript\nChanged 1: internal/paste.go\n\nOutcome: proved"
	aBuildThatChangedNothing = "It already passed.\n\nVertical: 1\nRan: 14\nRed: 0\n" +
		"Passing 1: TestPastingALinkPrintsTheTranscript\n\nOutcome: proved"
)

func initializeBuildStageSteps(sc *godog.ScenarioContext) {
	// A job at the last gate: its list is accepted, its suite is red, and a person approved the plan
	// that turns those tests green. Every scenario here starts from there, because that is the state
	// the fan out reads.
	sc.Step(`^a job whose plan a person approved and whose suite is red for (\d+) verticals?$`,
		func(ctx context.Context, verticals int) error {
			w := worldFrom(ctx)
			if verticals == 1 {
				w.runner.willAnswer(job.TheDesignAsk, oneVertical)
			}
			if err := aJobWaitingForItsPlanToBeApproved(ctx); err != nil {
				return err
			}
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if got := len(job.RequirementsOf(jobAsKept(one))); got != verticals {
				return fmt.Errorf("the accepted list carries %d verticals, want %d: %q",
					got, verticals, one.GetDesign())
			}
			if _, err := w.client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
				Id: one.GetId(), Answer: "yes",
			}); err != nil {
				return err
			}
			return nil
		})

	sc.Step(`^the builder will answer that its run executed no tests$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willAnswer(job.TheBuildAsk, aBuildThatExecutedNothing)
		return nil
	})

	sc.Step(`^the builder will answer that its suite is still red$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willAnswer(job.TheBuildAsk, aBuildThatIsStillRed)
		return nil
	})

	sc.Step(`^the builder will answer that it changed no file$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willAnswer(job.TheBuildAsk, aBuildThatChangedNothing)
		return nil
	})

	sc.Step(`^a worker is building each vertical, and the job itself has no session$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			workers, err := theBuilders(ctx)
			if err != nil {
				return err
			}
			wanted := job.RequirementsOf(jobAsKept(one))
			if len(workers) != len(wanted) {
				return fmt.Errorf("%d workers are building %d verticals", len(workers), len(wanted))
			}
			// The row itself buys no session for this stage. It is pending throughout, and every session
			// the stage pays for belongs to a worker holding one vertical.
			if one.GetPhase() != job.PhasePending {
				return fmt.Errorf("the job is %q while its workers build: %s",
					one.GetPhase(), one.GetReason())
			}
			for _, worker := range workers {
				if worker.GetSession() != "" && worker.GetSession() == one.GetSession() {
					return fmt.Errorf("a worker builds in the job's own conversation %q", one.GetSession())
				}
			}
			return nil
		})

	sc.Step(`^each worker was given its own vertical and told it may not change a test$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			wanted := job.RequirementsOf(jobAsKept(one))
			workers, err := theBuilders(ctx)
			if err != nil {
				return err
			}
			for _, worker := range workers {
				mine, others := "", 0
				for _, vertical := range wanted {
					if worker.GetClaim() == job.ClaimOnBuild(one.GetId(), vertical) {
						mine = vertical.Text
						continue
					}
					if strings.Contains(worker.GetBrief(), vertical.Text) {
						others++
					}
				}
				if mine == "" {
					return fmt.Errorf("worker %q claims %q, which is no vertical of this job",
						worker.GetTitle(), worker.GetClaim())
				}
				if !strings.Contains(worker.GetBrief(), mine) || others > 0 {
					return fmt.Errorf("the worker holding %q was given %d other verticals as well",
						mine, others)
				}
				if !strings.Contains(worker.GetBrief(), "You may not change one") {
					return fmt.Errorf("the worker holding %q was not told the boundary: %q",
						mine, worker.GetBrief())
				}
				// And it is under the gate rather than only under the sentence, because a rule a session
				// weighs against everything else it was told is advice.
				if !worker.GetBuilding() {
					return fmt.Errorf("the worker holding %q builds outside the boundary", mine)
				}
			}
			return nil
		})

	sc.Step(`^the builder for vertical (\d+) dies$`, func(ctx context.Context, vertical int) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		wanted := job.RequirementsOf(jobAsKept(one))
		if len(wanted) < vertical {
			return fmt.Errorf("this job has %d verticals, so it has no %dth", len(wanted), vertical)
		}
		claim := job.ClaimOnBuild(one.GetId(), wanted[vertical-1])
		workers, err := theBuilders(ctx)
		if err != nil {
			return err
		}
		for _, worker := range workers {
			if worker.GetClaim() != claim {
				continue
			}
			_, err := w.client.StopJob(ctx, &quaycrewv1.StopJobRequest{
				Id: worker.GetId(), Reason: "the sandbox went away",
			})
			return err
		}
		return fmt.Errorf("no worker holds vertical %d", vertical)
	})

	sc.Step(`^(\d+) workers? (?:are|is) building, one for each vertical$`,
		func(ctx context.Context, want int) error {
			workers, err := theBuilders(ctx)
			if err != nil {
				return err
			}
			if len(workers) != want {
				return fmt.Errorf("%d workers hold verticals of this job, want %d", len(workers), want)
			}
			claims := map[string]bool{}
			for _, worker := range workers {
				if claims[worker.GetClaim()] {
					return fmt.Errorf("two workers claim %q, so both build the same vertical",
						worker.GetClaim())
				}
				claims[worker.GetClaim()] = true
			}
			return nil
		})

	sc.Step(`^the row carries what was built for every vertical$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		wanted := job.RequirementsOf(jobAsKept(one))
		verticals, passing := job.BuiltOn(one.GetBuild())
		if verticals != len(wanted) || passing < len(wanted) {
			// What the job says next is where a run that did not close the stage went wrong, so it is in
			// the message: a record that is empty tells nobody which report was refused.
			return fmt.Errorf("the record covers %d of %d verticals with %d passing tests: %q; "+
				"the job is %q, asking %q, reason %q", verticals, len(wanted), passing, one.GetBuild(),
				one.GetPhase(), one.GetQuestion(), one.GetReason())
		}
		return nil
	})

	sc.Step(`^every file that was written says which vertical it came from$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			for _, vertical := range job.RequirementsOf(jobAsKept(one)) {
				changed := fmt.Sprintf("Changed %d:", vertical.Number)
				if !strings.Contains(one.GetBuild(), changed) {
					return fmt.Errorf("no file on the row opens with %q: %q", changed, one.GetBuild())
				}
			}
			return nil
		})

	sc.Step(`^the job is waiting for a person to accept what was built$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("a built job is %q, want asking: %s", one.GetPhase(), one.GetReason())
		}
		for _, needed := range []string{"say whether the value arrived", "Nothing else happens"} {
			if !strings.Contains(one.GetQuestion(), needed) {
				return fmt.Errorf("the question does not say %q: %s", needed, one.GetQuestion())
			}
		}
		return nil
	})

	sc.Step(`^the job is asking, and the row carries nothing built$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q, want asking: %s", one.GetPhase(), one.GetReason())
		}
		if one.GetBuild() != "" {
			return fmt.Errorf("a build that did not happen closed the stage anyway: %q", one.GetBuild())
		}
		return nil
	})

	sc.Step(`^the question says which test is wrong is a person's to decide$`,
		func(ctx context.Context) error {
			return theQuestionSays(ctx, "say which test is wrong")
		})

	sc.Step(`^the question says the test was already passing$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "was already passing")
	})

	sc.Step(`^the question names vertical (\d+)$`, func(ctx context.Context, vertical int) error {
		return theQuestionSays(ctx, fmt.Sprintf("vertical %d", vertical))
	})

	sc.Step(`^the reading says one session for each vertical is building, and none can change a test$`,
		func(ctx context.Context) error {
			return theReadingSays(ctx, "one session for each vertical", "change a test")
		})

	sc.Step(`^the reading says it waits for a person to look at what was built$`,
		func(ctx context.Context) error {
			return theReadingSays(ctx, "waits for you to look at", "verticals were built")
		})

	// The boundary, fired the way the model runtime fires it.

	sc.Step(`^a session that the system is building with$`, func(ctx context.Context) error {
		worldFrom(ctx).building = true
		return nil
	})

	sc.Step(`^a session the system is not building with$`, func(ctx context.Context) error {
		worldFrom(ctx).building = false
		return nil
	})

	sc.Step(`^that session is about to write to "([^"]*)"$`,
		func(ctx context.Context, where string) error {
			payload, err := json.Marshal(map[string]any{
				"tool_name":  "Write",
				"tool_input": map[string]string{"file_path": where},
			})
			if err != nil {
				return err
			}
			return fireTestGate(ctx, string(payload))
		})

	sc.Step(`^that session is about to run the command: (.+)$`,
		func(ctx context.Context, command string) error {
			payload, err := json.Marshal(map[string]any{
				"tool_name":  "Bash",
				"tool_input": map[string]string{"command": command},
			})
			if err != nil {
				return err
			}
			return fireTestGate(ctx, string(payload))
		})

	sc.Step(`^that session sends the test gate a payload it cannot read$`, func(ctx context.Context) error {
		return fireTestGate(ctx, "this is not the payload a runtime sends")
	})

	sc.Step(`^the test gate refuses it$`, func(ctx context.Context) error {
		if !worldFrom(ctx).testGate.refused {
			return errors.New("the gate let it through, and the suite is the only thing holding the " +
				"requirement")
		}
		return nil
	})

	sc.Step(`^the test gate allows it$`, func(ctx context.Context) error {
		answer := worldFrom(ctx).testGate
		if answer.refused {
			return fmt.Errorf("the gate refused work a build does, which is how a gate gets turned "+
				"off:\n%s", answer.said)
		}
		return nil
	})

	// A command that takes a directory whole names no test file, so what the refusal owes the session
	// is the directory, what is in it, and the way through.
	sc.Step(`^the refusal says to name the files it means$`, func(ctx context.Context) error {
		said := worldFrom(ctx).testGate.said
		for _, needed := range []string{"holds tests", "Name the files you mean", "say so in your answer"} {
			if !strings.Contains(said, needed) {
				return fmt.Errorf("the refusal does not say %q, so the session is left guessing:\n%s",
					needed, said)
			}
		}
		return nil
	})

	sc.Step(`^the refusal names the file and says to answer that the test is wrong$`,
		func(ctx context.Context) error {
			said := worldFrom(ctx).testGate.said
			for _, needed := range []string{"is a test", "say so in your answer", "name the file"} {
				if !strings.Contains(said, needed) {
					return fmt.Errorf("the refusal does not say %q, so the session is left guessing:\n%s",
						needed, said)
				}
			}
			return nil
		})

	sc.Step(`^the refusal names the variable the system sets$`, func(ctx context.Context) error {
		said := worldFrom(ctx).testGate.said
		if !strings.Contains(said, "KREWE_BUILDING") {
			return fmt.Errorf("the refusal does not name the variable, so nobody knows what "+
				"happened:\n%s", said)
		}
		return nil
	})
}

// theBuilders is every worker this job's build stage declared, which is every job under it that holds
// a build claim.
//
// By the claim rather than by the parent, because the test stage's workers are under the same parent
// and answered a different question. A step that counted every child would count those too.
func theBuilders(ctx context.Context) ([]*quaycrewv1.Job, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	listed, err := worldFrom(ctx).client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Parent: one.GetId(),
	})
	if err != nil {
		return nil, err
	}
	building := map[string]bool{}
	for _, vertical := range job.RequirementsOf(jobAsKept(one)) {
		building[job.ClaimOnBuild(one.GetId(), vertical)] = true
	}
	var builders []*quaycrewv1.Job
	for _, worker := range listed.GetJobs() {
		if building[worker.GetClaim()] {
			builders = append(builders, worker)
		}
	}
	return builders, nil
}

// fireTestGate runs the shipped entry point over one payload and records what it said, with the
// boundary set the way the system sets it on a build worker's task.
func fireTestGate(ctx context.Context, payload string) error {
	entry, err := shippedEntry("test-gate")
	if err != nil {
		return err
	}
	run := exec.CommandContext(ctx, entry)
	run.Stdin = strings.NewReader(payload)
	// Cleared rather than left alone, so a scenario about a session that is not building cannot pass
	// because the variable happened to be set in the environment running the suite.
	run.Env = append(run.Environ(), "KREWE_BUILDING=")
	if worldFrom(ctx).building {
		run.Env = append(run.Environ(), "KREWE_BUILDING=1")
	}
	var said strings.Builder
	run.Stderr = &said

	answer := gateAnswer{}
	switch err := run.Run(); {
	case err == nil:
	case isExit(err, 2):
		answer.refused = true
	default:
		return fmt.Errorf("running %s: %w\n%s", entry, err, said.String())
	}
	answer.said = said.String()
	worldFrom(ctx).testGate = answer
	return nil
}

// theReadingSays holds what the tool printed to the phrases a person needs off it. The reading is the
// surface somebody watching a build actually looks at, so a phrase missing here is a person who
// cannot tell a job that is building from one that is waiting for them.
func theReadingSays(ctx context.Context, needed ...string) error {
	out := toolFrom(ctx).stdout
	for _, phrase := range needed {
		if !strings.Contains(out, phrase) {
			return fmt.Errorf("the reading does not say %q: %s", phrase, out)
		}
	}
	return nil
}

// theVerticalsWereBuilt drives the whole build stage to its end: the fan out, the workers building,
// and the job holding for a person to accept what arrived.
//
// It is what the scenarios after this stage start from, the way they already start from an answered
// reading, an accepted list and a red suite. A job that owes a build never reaches its own session,
// so a scenario about that session which skipped this would be a scenario about a job that cannot get
// there.
func theVerticalsWereBuilt(ctx context.Context) error {
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
		if one.GetBuild() != "" {
			return nil
		}
		// Asking with nothing built is the stage stopping for a person, which is a failure of this run
		// rather than the hold it ends on: the hold carries the record.
		if one.GetPhase() == job.PhaseAsking {
			return fmt.Errorf("the job stopped in the build stage: %s", one.GetQuestion())
		}
	}
	return errors.New("the verticals were never built")
}
