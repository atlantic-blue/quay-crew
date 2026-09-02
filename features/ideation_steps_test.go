package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job saying what it understood and a person answering it, driven through the same calls both
// sides use. The assertions go past the row to the task the system actually sent, because what the
// session is handed next is the half that decides whether this works.

func initializeIdeationSteps(sc *godog.ScenarioContext) {
	// Said literally, because the double otherwise follows its task: a task asking what the session
	// understood gets a reading, and a scenario about a reply that is not one could never write it.
	sc.Step(`^the session will answer "([^"]*)"$`, func(ctx context.Context, said string) error {
		worldFrom(ctx).runner.willSayExactly(said)
		return nil
	})

	// A job that has said what it understood and is waiting for a person, which is where every
	// scenario about answering starts.
	sc.Step(`^a job waiting for a person to answer what it understood$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
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
		if one.GetPhase() != job.PhaseAsking || one.GetIdeation() == "" {
			return fmt.Errorf("the job is %q carrying %q, want it waiting to be answered",
				one.GetPhase(), one.GetIdeation())
		}
		return nil
	})

	// The second answer is refused, so this step keeps the refusal rather than failing on it.
	sc.Step(`^the operator answers the job again with "([^"]*)"$`,
		func(ctx context.Context, answer string) error {
			w := worldFrom(ctx)
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			_, w.lastErr = w.client.AnswerJob(ctx,
				&quaycrewv1.AnswerJobRequest{Id: one.GetId(), Answer: answer})
			return nil
		})

	sc.Step(`^the session is asked what it understood and told to write no plan$`,
		func(ctx context.Context) error {
			if err := theSessionWasSent(ctx, "write no plan yet", "Understood:", "Question 1:"); err != nil {
				return err
			}
			sent, err := theLastTaskSent(ctx)
			if err != nil {
				return err
			}
			if strings.Contains(sent, "Step 1:") {
				return fmt.Errorf("a session that owes a reading was asked for a plan: %q", sent)
			}
			return nil
		})

	sc.Step(`^the job is asking, and the row carries what it understood$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q, want asking: %s", one.GetPhase(), one.GetReason())
		}
		if !strings.Contains(one.GetIdeation(), "Understood:") {
			return fmt.Errorf("what it understood is %q", one.GetIdeation())
		}
		if one.GetIdeationAnswer() != "" {
			return fmt.Errorf("the row reads as answered by %q, and nobody answered it",
				one.GetIdeationAnswer())
		}
		return nil
	})

	// The two lists that are the point of the record. What a person stated and what the session
	// filled in read the same on a row today, and this is where they stop reading the same.
	sc.Step(`^the record marks what it was told apart from what it assumed$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for _, heading := range []string{"Told:", "Assumed:", "Unknown:", "Confidence:"} {
			if !strings.Contains(one.GetIdeation(), heading) {
				return fmt.Errorf("the record is %q, want it to carry %q", one.GetIdeation(), heading)
			}
		}
		return nil
	})

	sc.Step(`^the question names the sentence and says there is nothing to approve$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			for _, phrase := range []string{one.GetProduct(), "in your own words", "nothing to approve"} {
				if !strings.Contains(one.GetQuestion(), phrase) {
					return fmt.Errorf("the question is %q, want it to say %q", one.GetQuestion(), phrase)
				}
			}
			return nil
		})

	sc.Step(`^no plan was written$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPlan() != "" {
			return fmt.Errorf("a job nobody has answered wrote the plan %q", one.GetPlan())
		}
		return nil
	})

	sc.Step(`^no plan is approved$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPlanApproved() {
			return fmt.Errorf("a plan reads as approved before one was written")
		}
		return nil
	})

	sc.Step(`^the row carries that answer, word for word$`, func(ctx context.Context) error {
		return theRowCarriesTheAnswer(ctx,
			"1: on the command line first, the panel can come later")
	})

	sc.Step(`^the row still carries the first answer$`, func(ctx context.Context) error {
		return theRowCarriesTheAnswer(ctx, "1: on the command line")
	})

	sc.Step(`^the second answer is refused$`, func(ctx context.Context) error {
		if worldFrom(ctx).lastErr == nil {
			return fmt.Errorf("a second answer was taken")
		}
		return nil
	})

	sc.Step(`^the session is asked what it would build, carrying that answer and what it assumed$`,
		func(ctx context.Context) error {
			return theSessionWasSent(ctx, "Vertical 1:", "the panel can come later",
				"Assumed:", "still an assumption")
		})

	sc.Step(`^the session is told which question is still unknown$`, func(ctx context.Context) error {
		return theSessionWasSent(ctx, "still unknown", "Vertical 1:")
	})

	sc.Step(`^the session is asked again and told what was wrong$`, func(ctx context.Context) error {
		return theSessionWasSent(ctx, "asked what you understood once already", "Understood:")
	})

	sc.Step(`^the job is stopped because it was asked twice$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q, want stopped", one.GetPhase())
		}
		if !strings.Contains(one.GetReason(), "asked twice") {
			return fmt.Errorf("the reason is %q, want it to say the session was asked twice",
				one.GetReason())
		}
		return nil
	})

	sc.Step(`^that job was never asked what it understood$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		return nothingWasAsked(one)
	})

	sc.Step(`^the new job was never asked what it understood$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), productFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the declaration was refused: %w", w.lastErr)
		}
		if len(scenario.declared) == 0 {
			return fmt.Errorf("nothing was declared")
		}
		newest := scenario.declared[len(scenario.declared)-1]
		found, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: newest.GetId()})
		if err != nil {
			return err
		}
		return nothingWasAsked(found.GetJob())
	})
}

// theRowCarriesTheAnswer holds the row against the words a person wrote, which is the whole of the
// answer being content rather than consent.
func theRowCarriesTheAnswer(ctx context.Context, want string) error {
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	if one.GetIdeationAnswer() != want {
		return fmt.Errorf("the row carries %q as what the person wrote, want %q",
			one.GetIdeationAnswer(), want)
	}
	return nil
}

// nothingWasAsked is a job that never owed anybody a reading.
func nothingWasAsked(one *quaycrewv1.Job) error {
	if one.GetIdeation() != "" {
		return fmt.Errorf("it was asked what it understood: %q", one.GetIdeation())
	}
	if one.GetQuestion() != "" {
		return fmt.Errorf("it was asked %q", one.GetQuestion())
	}
	return nil
}
