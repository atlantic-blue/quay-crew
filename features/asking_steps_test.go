package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/cucumber/godog"
)

// A job putting a question to a person, driven the way both sides drive it: the session calls
// carrying the credential the system minted for its job, and the operator calls as the operator.
//
// The assertions go past the row. What decides whether this works is what the session is handed
// next, so the scenarios read the task record rather than stopping at the phase.

// theCostQuestion is the question the acceptance run needed and could not ask.
const theCostQuestion = "aurora serverless version two bills a minimum capacity continuously. " +
	"a key value store on demand bills nothing at rest. Which?"

func initializeAskingSteps(sc *godog.ScenarioContext) {
	// A job whose task is under way, which is the only state a question can be put from: a session
	// that has not started has nothing to ask about, and one that has answered has stopped.
	//
	// It runs as a role holding every verb the system grants, which is what makes the refusal below
	// mean anything: a session that cannot answer because its role is narrow says nothing about
	// whether any session can.
	sc.Step(`^a job titled "([^"]*)" whose session is still working$`, func(ctx context.Context, title string) error {
		w := worldFrom(ctx)
		if err := aRoleHoldingEveryVerb(ctx); err != nil {
			return err
		}
		w.release = w.runner.hold()
		if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: title, Brief: "read what the project says about cost and pick the store",
			Role: everyVerbRole,
		}); err != nil {
			return err
		}
		if w.lastErr != nil {
			return w.lastErr
		}
		w.server.TickJob(ctx)
		return w.runner.waitForTask()
	})

	sc.Step(`^another job titled "([^"]*)"$`, func(ctx context.Context, title string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{Title: title, Brief: "open it"})
	})

	sc.Step(`^the session running that job asks "([^"]*)"$`, func(ctx context.Context, question string) error {
		return askAsTheSession(ctx, question, "")
	})

	sc.Step(`^the session running that job (?:asks|asked) its question$`, func(ctx context.Context) error {
		if err := askAsTheSession(ctx, theCostQuestion, ""); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^the session running the first job asks about the other one$`, func(ctx context.Context) error {
		other, err := readJob(ctx, 1)
		if err != nil {
			return err
		}
		return askAsTheSession(ctx, theCostQuestion, other.GetId())
	})

	sc.Step(`^that session tries to answer its own question$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		asking, err := theSessionRunning(ctx, one.GetId())
		if err != nil {
			return err
		}
		_, w.lastErr = asking.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
			Id: one.GetId(), Answer: "the key value store, on demand",
		})
		return nil
	})

	sc.Step(`^the operator answers the job with "([^"]*)"$`, func(ctx context.Context, answer string) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{Id: one.GetId(), Answer: answer})
		return w.lastErr
	})

	sc.Step(`^the job is asking, and the row carries the question it put$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q saying %q, want asking", one.GetPhase(), one.GetReason())
		}
		if !strings.Contains(one.GetQuestion(), "aurora serverless") {
			return fmt.Errorf("the row carries the question %q", one.GetQuestion())
		}
		return nil
	})

	sc.Step(`^the job is still asking$`, func(ctx context.Context) error {
		return jobIs(ctx, 0, job.PhaseAsking)
	})

	sc.Step(`^the job is running again$`, func(ctx context.Context) error {
		return jobIs(ctx, 0, job.PhaseRunning)
	})

	sc.Step(`^the second task carries the answer and the question it answers$`, func(ctx context.Context) error {
		sent, err := tasksSentForTheJob(ctx, 2)
		if err != nil {
			return err
		}
		carried := sent[1].GetPrompt()
		if !strings.Contains(carried, "the key value store, on demand") {
			return fmt.Errorf("the second task does not carry the answer:\n%s", carried)
		}
		if !strings.Contains(carried, "aurora serverless") {
			return fmt.Errorf("the second task does not restate the question, so the answer arrives at nothing:\n%s", carried)
		}
		return nil
	})

	sc.Step(`^the second task does not send the brief again$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		sent, err := tasksSentForTheJob(ctx, 2)
		if err != nil {
			return err
		}
		if strings.Contains(sent[1].GetPrompt(), one.GetBrief()) {
			return fmt.Errorf("the second task sends the brief again, which asks the session to do the job over:\n%s",
				sent[1].GetPrompt())
		}
		return nil
	})

	sc.Step(`^the records for that job read "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)"$`,
		func(ctx context.Context, first, second, third, fourth, fifth string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			events, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
			if err != nil {
				return err
			}
			got, want := eventKinds(events), []string{first, second, third, fourth, fifth}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				return fmt.Errorf("the records read %v, want %v", got, want)
			}
			return nil
		})

	sc.Step(`^the system refuses it, and the job is still asking$`, func(ctx context.Context) error {
		if worldFrom(ctx).lastErr == nil {
			return fmt.Errorf("the session answered the question a person was asked")
		}
		return jobIs(ctx, 0, job.PhaseAsking)
	})

	sc.Step(`^the system refuses it, naming the job the credential is for$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if err := theRefusalSays(one.GetId())(ctx); err != nil {
			return err
		}
		if phase := one.GetPhase(); phase != job.PhaseRunning {
			return fmt.Errorf("the refused question moved the job to %q", phase)
		}
		return nil
	})
}

// everyVerbRole is a role granting every verb the system has, so a refusal in these scenarios is the
// system's boundary rather than a narrow grant.
const everyVerbRole = "planner"

// aRoleHoldingEveryVerb imports and attaches that role.
func aRoleHoldingEveryVerb(ctx context.Context) error {
	w := worldFrom(ctx)
	if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: roleFilesThatMay(everyVerbRole, role.Grantable),
	}); err != nil {
		return err
	}
	_, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: w.workspaceID, Name: everyVerbRole,
	})
	return err
}

// askAsTheSession puts a question the way the session doing the job puts one: over the credential
// the system minted for that job. `about` is the job named in the request, and empty is the caller's
// own, which is the only one the system accepts.
func askAsTheSession(ctx context.Context, question, about string) error {
	w := worldFrom(ctx)
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	asking, err := theSessionRunning(ctx, one.GetId())
	if err != nil {
		return err
	}
	_, w.lastErr = asking.AskJob(ctx, &quaycrewv1.AskJobRequest{Question: question, Id: about})
	return nil
}

// theSessionRunning is a caller holding the credential the system minted for this job, which is what
// the session inside the container holds.
func theSessionRunning(ctx context.Context, id string) (quaycrewv1.ControlPlaneServiceClient, error) {
	w := worldFrom(ctx)
	token, minted := w.server.JobCredentialForTest(ctx, id)
	if !minted {
		return nil, fmt.Errorf("the system minted no credential for the job %s", id)
	}
	return w.dialAs(token), nil
}

// tasksSentForTheJob is what the system sent the session doing this job, waited for rather than read
// once: a dispatch lets go, so the row is written by the goroutine running the task.
func tasksSentForTheJob(ctx context.Context, want int) ([]*quaycrewv1.Task, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	if one.GetSession() == "" {
		return nil, fmt.Errorf("the job says no session, so nothing was asked of the system")
	}
	var sent []*quaycrewv1.Task
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		listed, err := worldFrom(ctx).client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: one.GetSession()})
		if err != nil {
			return nil, err
		}
		sent = listed.GetTasks()
		if len(sent) >= want {
			return sent, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, fmt.Errorf("%d tasks were sent to the session, want %d", len(sent), want)
}
