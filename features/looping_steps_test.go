package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/cucumber/godog"
)

// A job going in circles, driven the way one really goes in circles: an attempt that did not get
// there, the operator putting it back, and the same thing said again.
//
// The assertions go past the phase. What decides whether this is worth having is what the record
// says each attempt was, and what the next session is handed, so these read the attempts on the row
// and the text of the task that came after them.

func initializeLoopingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job is declared escalating by "([^"]*)"$`, func(ctx context.Context, route string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "get the coverage check green", Brief: "make the coverage gate pass",
			Escalation: route,
		})
	})

	sc.Step(`^a job titled "([^"]*)" that escalates by "([^"]*)"$`,
		func(ctx context.Context, title, route string) error {
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "make the coverage gate pass", Escalation: route,
			}); err != nil {
				return err
			}
			return worldFrom(ctx).lastErr
		})

	sc.Step(`^the workspace holds a role called "([^"]*)"$`, func(ctx context.Context, named string) error {
		w := worldFrom(ctx)
		if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
			Files: roleFilesThatMay(named, role.Grantable),
		}); err != nil {
			return err
		}
		_, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
			Workspace: w.workspaceID, Name: named,
		})
		return err
	})

	// One attempt, end to end: the controller sends the task, the model does not get there, and the
	// controller reads what came back on the next tick.
	sc.Step(`^the attempt fails saying "([^"]*)"$`, func(ctx context.Context, said string) error {
		w := worldFrom(ctx)
		w.runner.failTheNextTaskWith(said)
		return anAttempt(ctx)
	})

	sc.Step(`^the attempt answers "([^"]*)"$`, func(ctx context.Context, said string) error {
		worldFrom(ctx).runner.willSay(said)
		return anAttempt(ctx)
	})

	sc.Step(`^the system refuses it, saying a role that runs on that model is the way to say it$`,
		func(ctx context.Context) error {
			return theLoopRefusalSays(ctx, "one model", "role:<name>")
		})

	sc.Step(`^the system refuses it, naming the role$`, func(ctx context.Context) error {
		return theLoopRefusalSays(ctx, "archivist")
	})

	sc.Step(`^the system refuses it, offering ask and role$`, func(ctx context.Context) error {
		return theLoopRefusalSays(ctx, "retry", "ask", "role:<name>")
	})

	sc.Step(`^the job is done$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseDone {
			return fmt.Errorf("the job is %q saying %q, want done", one.GetPhase(), one.GetReason())
		}
		if one.GetLoopedStep() != 0 {
			return fmt.Errorf("an attempt that finished the job read as a loop, on step %d",
				one.GetLoopedStep())
		}
		return nil
	})

	sc.Step(`^the job is failed rather than escalated$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseFailed {
			return fmt.Errorf("the job is %q saying %q, want failed", one.GetPhase(), one.GetReason())
		}
		if one.GetLoopedStep() != 0 || one.GetEscalatedTo() != "" {
			return fmt.Errorf("three attempts at different work read as a loop: step %d, escalated to %q",
				one.GetLoopedStep(), one.GetEscalatedTo())
		}
		return nil
	})

	// The measure, on the record. Without this the escalation above is a decision nobody can check.
	sc.Step(`^the record says what each attempt said, and how alike it was$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		attempts := one.GetAttempted()
		if len(attempts) != 3 {
			return fmt.Errorf("the job records %d attempts, want the three it made", len(attempts))
		}
		for at, attempt := range attempts {
			switch {
			case attempt.GetSeq() != int32(at+1):
				return fmt.Errorf("attempt %d is numbered %d", at+1, attempt.GetSeq())
			case attempt.GetStep() != 1:
				return fmt.Errorf("attempt %d was at step %d, and nothing was finished", at+1, attempt.GetStep())
			case attempt.GetSaid() == "":
				return fmt.Errorf("attempt %d says nothing about what it produced", at+1)
			case attempt.GetTask() == "":
				return fmt.Errorf("attempt %d names no task, so reading it twice would count it twice", at+1)
			}
		}
		if attempts[0].GetSimilarity() != 0 {
			return fmt.Errorf("the first attempt is %.2f alike, and it has nothing to be like",
				attempts[0].GetSimilarity())
		}
		for _, attempt := range attempts[1:] {
			if attempt.GetSimilarity() >= job.LoopThreshold {
				return fmt.Errorf("attempt %d scores %.2f against work that was different, and the "+
					"threshold is %.2f", attempt.GetSeq(), attempt.GetSimilarity(), job.LoopThreshold)
			}
		}
		return nil
	})

	sc.Step(`^the job is asking, and says it went in circles on step (\d+)$`,
		func(ctx context.Context, step int) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			switch {
			case one.GetPhase() != job.PhaseAsking:
				return fmt.Errorf("the job is %q saying %q, want it waiting to be told",
					one.GetPhase(), one.GetReason())
			case one.GetLoopedStep() != int32(step):
				return fmt.Errorf("the job says it went in circles on step %d, want %d",
					one.GetLoopedStep(), step)
			case one.GetEscalatedTo() != job.RouteAsk:
				return fmt.Errorf("the job says it escalated to %q, want the operator", one.GetEscalatedTo())
			}
			return nil
		})

	// A number is not a decision. What the person answering has is what the session actually said,
	// which is what an operator reading the transcript would have seen.
	sc.Step(`^the question carries what the attempts said$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for _, want := range []string{"the check is still red", "3 attempts", "step 1"} {
			if !strings.Contains(one.GetQuestion(), want) {
				return fmt.Errorf("the question does not say %q:\n%s", want, one.GetQuestion())
			}
		}
		return nil
	})

	sc.Step(`^the job is going again, handed to the architect role$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		switch {
		case one.GetPhase() != job.PhasePending:
			return fmt.Errorf("the job is %q saying %q, want it waiting to be started again",
				one.GetPhase(), one.GetReason())
		case one.GetEscalatedTo() != "role:architect":
			return fmt.Errorf("the job says it escalated to %q", one.GetEscalatedTo())
		case one.GetReason() != "":
			return fmt.Errorf("a job that is going again carries the reason %q, which reads as one the "+
				"machine is holding back for want of room", one.GetReason())
		}
		return nil
	})

	// A role is read only when a session is born, so a job handed on that carried on in the same
	// conversation would be handed on in name and not in fact.
	sc.Step(`^the next task runs in a conversation of its own, as that role$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.server.TickJob(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		found, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: one.GetSession()})
		if err != nil {
			return err
		}
		// The handle is derived from the row rather than minted, so whichever controller comes back to
		// this job finds the same conversation. The attempts that went in circles stay in the other one.
		if handle := found.GetSession().GetHandle(); handle != job.ConversationFor(&job.Job{
			ID: one.GetId(), EscalatedTo: "role:architect",
		}) {
			return fmt.Errorf("the handed job runs in %q, which is the conversation it went in circles in",
				handle)
		}
		if found.GetSession().GetRole() != "architect" {
			return fmt.Errorf("the new conversation runs as %q, want the role it was handed to",
				found.GetSession().GetRole())
		}
		for _, attempt := range one.GetAttempted() {
			if attempt.GetSession() == one.GetSession() {
				return fmt.Errorf("the attempts that went in circles read as though they were made in the "+
					"conversation this role is starting in: %q", attempt.GetSession())
			}
		}
		return nil
	})

	sc.Step(`^the next task carries what the earlier attempts said, and says not to make them again$`,
		func(ctx context.Context) error {
			carried, err := taskAsking(ctx, 3)
			if err != nil {
				return err
			}
			for _, want := range []string{
				"handed to you", "the check is still red", "Do not make those attempts again",
			} {
				if !strings.Contains(carried, want) {
					return fmt.Errorf("the task the new role was given does not say %q:\n%s", want, carried)
				}
			}
			return nil
		})

	sc.Step(`^the job is stopped, and the reason says it went in circles again after being escalated$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
			}
			for _, want := range []string{"went in circles again", "role:architect"} {
				if !strings.Contains(one.GetReason(), want) {
					return fmt.Errorf("the reason does not say %q:\n%s", want, one.GetReason())
				}
			}
			return nil
		})

	sc.Step(`^the job still says it was handed to the architect role$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetEscalatedTo() != "role:architect" {
			return fmt.Errorf("the job says it escalated to %q, and the route it took the first time is "+
				"what a reader needs to see why it stopped", one.GetEscalatedTo())
		}
		return nil
	})
}

// anAttempt runs one attempt end to end: the controller sends the task, the task lands, and the
// controller reads what came of it on the next tick.
func anAttempt(ctx context.Context) error {
	w := worldFrom(ctx)
	w.server.TickJob(ctx)
	if err := w.settled(ctx); err != nil {
		return err
	}
	w.server.TickJob(ctx)
	return w.settled(ctx)
}

// theLoopRefusalSays holds the last refusal to the words it has to carry, because a refusal a caller cannot
// act on sends them looking.
func theLoopRefusalSays(ctx context.Context, wanted ...string) error {
	w := worldFrom(ctx)
	if w.lastErr == nil {
		return fmt.Errorf("the declaration was accepted")
	}
	for _, want := range wanted {
		if !strings.Contains(w.lastErr.Error(), want) {
			return fmt.Errorf("the refusal says %q, want it to say %q", w.lastErr, want)
		}
	}
	return nil
}
