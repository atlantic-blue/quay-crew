package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A person approving the plan before any work starts, driven through the same calls both sides use:
// the crew answers with a plan, the operator answers the question, and the session records what it
// finished.
//
// The assertions go past the row. What decides whether this works is what the session is handed
// next, so these read the task the system actually sent.

// theSentenceAPersonSaid is what the person wanted, in their own words, and thePlanTheCrewWrote is
// what the crew answers when it is asked to plan for it. The reply carries prose around the plan,
// because a model's answer does, and what is kept has to be the lines rather than the reply.
const (
	thePlanTheCrewWrote = "Right, here is what I will do.\n\nStep 1: read the design\n" +
		"Step 2: build the address that takes a link\n\nThat is the whole of it."
	thePlanAsKept = "Step 1: read the design\nStep 2: build the address that takes a link"
)

func initializePlanSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job that says a person "([^"]*)"$`, func(ctx context.Context, sentence string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "the transcript page", Brief: "build what the design describes", Product: sentence,
		})
	})

	sc.Step(`^the session will answer with a plan of (\d+) steps$`, func(ctx context.Context, steps int) error {
		if steps != 2 {
			return fmt.Errorf("this scenario has a plan of 2 steps to answer with, not %d", steps)
		}
		worldFrom(ctx).runner.willSay(thePlanTheCrewWrote)
		return nil
	})

	// A job that has written its plan and is waiting for a person, which is the state every scenario
	// about answering starts from.
	sc.Step(`^a job whose plan is waiting to be approved$`, func(ctx context.Context) error {
		return aJobWaitingForItsPlanToBeApproved(ctx)
	})

	// And the same job with the approval given, which is where the work begins.
	sc.Step(`^a job whose plan was approved$`, func(ctx context.Context) error {
		if err := aJobWaitingForItsPlanToBeApproved(ctx); err != nil {
			return err
		}
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if _, err := w.client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
			Id: one.GetId(), Answer: "yes",
		}); err != nil {
			return err
		}
		// The work task, which is what the steps below are recorded against.
		w.server.TickJob(ctx)
		return nil
	})

	sc.Step(`^the session records step "([^"]*)" and nothing else$`, func(ctx context.Context, said string) error {
		return recordAgainstThePlan(ctx, said)
	})

	sc.Step(`^the session records every step of the plan$`, func(ctx context.Context) error {
		for _, said := range []string{"1: read the design", "2: built the address"} {
			if err := recordAgainstThePlan(ctx, said); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the session is asked for a plan and told to do no work$`, func(ctx context.Context) error {
		return theSessionWasSent(ctx, "Do no work yet", "Step 1:")
	})

	sc.Step(`^the job is asking, and the row carries the plan it wrote$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q, want asking: %s", one.GetPhase(), one.GetReason())
		}
		if one.GetPlan() != thePlanAsKept {
			return fmt.Errorf("the plan on the row is %q, want %q", one.GetPlan(), thePlanAsKept)
		}
		return nil
	})

	sc.Step(`^the question names the sentence and the plan$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for _, phrase := range []string{one.GetProduct(), "Step 1: read the design"} {
			if !strings.Contains(one.GetQuestion(), phrase) {
				return fmt.Errorf("the question is %q, want it to say %q", one.GetQuestion(), phrase)
			}
		}
		return nil
	})

	sc.Step(`^the plan is not approved yet$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPlanApproved() {
			return fmt.Errorf("the plan reads as approved, and nobody approved it")
		}
		return nil
	})

	sc.Step(`^the plan is approved$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if !one.GetPlanApproved() {
			return fmt.Errorf("the plan is not approved after a person answered yes")
		}
		return nil
	})

	sc.Step(`^the session is sent the plan it wrote and what the person said$`, func(ctx context.Context) error {
		return theSessionWasSent(ctx, "was not approved", "Step 1: read the design",
			"do not make them find an identifier first", "Do no work yet")
	})

	sc.Step(`^the session is sent the brief and the plan it is held to$`, func(ctx context.Context) error {
		if err := theSessionWasSent(ctx, "build what the design describes",
			"Step 1: read the design", "record it with its number"); err != nil {
			return err
		}
		sent, err := theLastTaskSent(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(sent, "Do no work yet") {
			return fmt.Errorf("the session was told to do no work after its plan was approved")
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason names the step nothing accounted for$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("the job is %q, want stopped", one.GetPhase())
			}
			for _, phrase := range []string{"step 2", "build the address that takes a link"} {
				if !strings.Contains(one.GetReason(), phrase) {
					return fmt.Errorf("the reason is %q, want it to name %q", one.GetReason(), phrase)
				}
			}
			return nil
		})

	// The work is not lost when the job stops. It is unapproved, which is a different thing, and the
	// operator reads what it produced next to the reason.
	sc.Step(`^the answer the session gave is still on the row$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetAnswer() == "" {
			return fmt.Errorf("the job stopped and threw away what its session answered")
		}
		return nil
	})

	sc.Step(`^the job is done, and it carries no reason$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseDone {
			return fmt.Errorf("the job is %q, want done: %s", one.GetPhase(), one.GetReason())
		}
		if one.GetReason() != "" {
			return fmt.Errorf("a job that followed its plan carries the reason %q", one.GetReason())
		}
		return nil
	})

	sc.Step(`^that job carries no plan and asked nothing$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPlan() != "" {
			return fmt.Errorf("an errand carries the plan %q", one.GetPlan())
		}
		if one.GetQuestion() != "" {
			return fmt.Errorf("an errand was asked %q", one.GetQuestion())
		}
		return nil
	})
}

// aJobWaitingForItsPlanToBeApproved declares a planned job, lets the crew answer with a plan, and
// leaves the job asking.
func aJobWaitingForItsPlanToBeApproved(ctx context.Context) error {
	w := worldFrom(ctx)
	w.runner.willSay(thePlanTheCrewWrote)
	if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
		Title: "the transcript page", Brief: "build what the design describes",
		Product: "pastes a link and gets the text back",
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
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	if one.GetPhase() != job.PhaseAsking {
		return fmt.Errorf("the job is %q, want it waiting for its plan to be approved: %s",
			one.GetPhase(), one.GetReason())
	}
	return nil
}

// recordAgainstThePlan is the session saying it finished one step, carrying its own credential.
func recordAgainstThePlan(ctx context.Context, said string) error {
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	session, err := theSessionRunning(ctx, one.GetId())
	if err != nil {
		return err
	}
	_, err = session.RecordJobStep(ctx, &quaycrewv1.RecordJobStepRequest{Summary: said})
	return err
}

// theLastTaskSent is what the system last asked the session doing this job to do.
func theLastTaskSent(ctx context.Context) (string, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return "", err
	}
	listed, err := worldFrom(ctx).client.ListTasks(ctx,
		&quaycrewv1.ListTasksRequest{Session: one.GetSession()})
	if err != nil {
		return "", err
	}
	sent := listed.GetTasks()
	if len(sent) == 0 {
		return "", fmt.Errorf("the system sent this job's session nothing")
	}
	return sent[len(sent)-1].GetPrompt(), nil
}

// theSessionWasSent holds the last task against every phrase it has to carry.
func theSessionWasSent(ctx context.Context, phrases ...string) error {
	sent, err := theLastTaskSent(ctx)
	if err != nil {
		return err
	}
	for _, phrase := range phrases {
		if !strings.Contains(sent, phrase) {
			return fmt.Errorf("the session was sent %q, want it to say %q", sent, phrase)
		}
	}
	return nil
}
