package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job that failed being continued rather than declared again, driven from both sides: the session
// records what it finished over the credential the system minted for its job, and the operator
// continues or refuses it as the operator.
//
// The assertions go past the row. What decides whether anything was saved is what the second task
// carries, so these read the task record rather than stopping at the phase.

func initializeResumingSteps(sc *godog.ScenarioContext) {
	// A job whose task is under way, working in a repository, which is the shape every job on the
	// acceptance run had: a worktree, a branch, and a pull request at the end of it.
	//
	// It runs as a role holding every verb the system grants, so a refusal in these scenarios is the
	// system's boundary rather than a narrow grant.
	sc.Step(`^a job titled "([^"]*)" in the repository "([^"]*)" whose session is still working$`,
		func(ctx context.Context, title, repository string) error {
			w := worldFrom(ctx)
			if err := aRoleHoldingEveryVerb(ctx); err != nil {
				return err
			}
			w.release = w.runner.hold()
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "make the listing sort by the clock it shows",
				// In the mode that reaches the network: a job that works in a repository is only declared in
				// that one, because the clone, the push and the pull request all need it.
				Role: everyVerbRole, Repository: repository, Mode: "dangerous",
				// With the settle gate off, so every scenario in this file ends where continuing a job
				// ends. A gated job is held back until a reviewer and a tester have passed it, which is
				// features/settling.feature.
				Ungated: true,
			}); err != nil {
				return err
			}
			if w.lastErr != nil {
				return w.lastErr
			}
			w.server.TickJob(ctx)
			return w.runner.waitForTask()
		})

	sc.Step(`^the failed job names the pull request "([^"]*)"$`, func(ctx context.Context, want string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseFailed {
			return fmt.Errorf("the job is %q, want failed", one.GetPhase())
		}
		if one.GetPullRequest() != want {
			return fmt.Errorf("the failed job names the pull request %q, want %q", one.GetPullRequest(), want)
		}
		return nil
	})

	sc.Step(`^the session running that job records "([^"]*)"$`, func(ctx context.Context, said string) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		session, err := theSessionRunning(ctx, one.GetId())
		if err != nil {
			return err
		}
		_, w.lastErr = session.RecordJobStep(ctx, &quaycrewv1.RecordJobStepRequest{Summary: said})
		return w.lastErr
	})

	// The task ends the way most of them ended on the acceptance run: not because the work was wrong,
	// but because something underneath it went away.
	sc.Step(`^the task the controller sent fails$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.runner.failTheNextTask()
		if w.release != nil {
			w.release()
			w.release = nil
		}
		if err := w.settled(ctx); err != nil {
			return err
		}
		w.server.TickJob(ctx)
		return nil
	})

	sc.Step(`^the job is failed, and it still says the two steps it finished$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseFailed {
			return fmt.Errorf("the job is %q saying %q, want failed", one.GetPhase(), one.GetReason())
		}
		if len(one.GetSteps()) != 2 {
			return fmt.Errorf("the failed job says it finished %v, want the two steps it recorded",
				summariesOf(one.GetSteps()))
		}
		return nil
	})

	sc.Step(`^the job says it finished one step$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if len(one.GetSteps()) != 1 {
			return fmt.Errorf("the job says it finished %v, want one step", summariesOf(one.GetSteps()))
		}
		return nil
	})

	sc.Step(`^the operator continues the job$`, func(ctx context.Context) error {
		return continueTheJob(ctx)
	})

	sc.Step(`^the operator continues the job again$`, func(ctx context.Context) error {
		// The refusal is what this asks about, so the error is kept rather than returned.
		_ = continueTheJob(ctx)
		return nil
	})

	sc.Step(`^the operator refuses the job saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.RefuseJob(ctx, &quaycrewv1.RefuseJobRequest{Id: one.GetId(), Reason: reason})
		return w.lastErr
	})

	sc.Step(`^that session tries to continue its own job$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		session, err := theSessionRunning(ctx, one.GetId())
		if err != nil {
			return err
		}
		_, w.lastErr = session.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: one.GetId()})
		return nil
	})

	sc.Step(`^the second task carries the steps that are finished, and not the brief$`,
		func(ctx context.Context) error {
			carried, err := taskAsking(ctx, 1)
			if err != nil {
				return err
			}
			for _, want := range []string{"read the issue", "cut the worktree from origin/main"} {
				if !strings.Contains(carried, want) {
					return fmt.Errorf("the second task does not say %q:\n%s", want, carried)
				}
			}
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			// Sending the brief again asks for the whole job a second time, which is the bill this
			// behaviour exists to stop paying.
			if strings.Contains(carried, one.GetBrief()) {
				return fmt.Errorf("the second task sends the brief again:\n%s", carried)
			}
			return nil
		})

	sc.Step(`^the second task says what it failed with, and asks what moved under its base$`,
		func(ctx context.Context) error {
			carried, err := taskAsking(ctx, 1)
			if err != nil {
				return err
			}
			// The shape it is asked to answer in as well, because the system reads a shape rather than
			// the prose: a session asked for a report and never told how to write one is a session the
			// reading refuses.
			for _, want := range []string{
				"the model refused this task", "fetch the branch this work is based on", "Base:",
			} {
				if !strings.Contains(carried, want) {
					return fmt.Errorf("the second task does not say %q:\n%s", want, carried)
				}
			}
			return nil
		})

	// The session is the point. The worktree, the branch and the pull request are inside it, so a
	// continued job that started a second conversation would be a second attempt from nothing.
	sc.Step(`^the second task ran in the session the first attempt was in$`, func(ctx context.Context) error {
		sent, err := tasksSentForTheJob(ctx, 2)
		if err != nil {
			return err
		}
		if sent[0].GetSession() != sent[1].GetSession() {
			return fmt.Errorf("the second task ran in session %s and the first ran in %s",
				sent[1].GetSession(), sent[0].GetSession())
		}
		return nil
	})

	sc.Step(`^the system refuses it, saying the job has not stopped$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("a job that is already going again was continued a second time")
		}
		if !strings.Contains(w.lastErr.Error(), "has not stopped") {
			return fmt.Errorf("the refusal says %q, want it to say the job has not stopped", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason carries what the operator decided and what it failed with$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("a refused job is %q, want stopped", one.GetPhase())
			}
			for _, want := range []string{"the migration was wrong", "It failed with"} {
				if !strings.Contains(one.GetReason(), want) {
					return fmt.Errorf("the reason says %q, want it to carry %q", one.GetReason(), want)
				}
			}
			return nil
		})

	sc.Step(`^continuing it is refused, and no second task is sent$`, func(ctx context.Context) error {
		if err := continueTheJob(ctx); err == nil {
			return fmt.Errorf("a job the operator refused was continued anyway")
		}
		w := worldFrom(ctx)
		w.server.TickJob(ctx)
		if counted := w.runner.count(); counted != 1 {
			return fmt.Errorf("the system was asked to run %d tasks, want only the one that failed", counted)
		}
		return nil
	})

	sc.Step(`^the system refuses it, and the job is still failed$`, func(ctx context.Context) error {
		if worldFrom(ctx).lastErr == nil {
			return fmt.Errorf("the session continued the job it was doing itself")
		}
		return jobIs(ctx, 0, job.PhaseFailed)
	})

	// The base a continued attempt stands on. Nothing here runs git, so the system states the shape it reads
	// and reads the answer against it, the way it already does with the address of a pull request.
	sc.Step(`^the session was asked what moved under its base$`, func(ctx context.Context) error {
		asked, err := taskAsking(ctx, 2)
		if err != nil {
			return err
		}
		for _, want := range []string{"atlantic-blue/quay-crew", "Base:", "Fetch the branch"} {
			if !strings.Contains(asked, want) {
				return fmt.Errorf("the session was asked %q, want it to say %q", asked, want)
			}
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason says no answer said what moved under its base$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
			}
			for _, want := range []string{"what moved under its base", "asked twice"} {
				if !strings.Contains(one.GetReason(), want) {
					return fmt.Errorf("the reason says %q, want it to say %q", one.GetReason(), want)
				}
			}
			return nil
		})

	// The end of an attempt is not the end of what it produced. A reader who cannot find the pull
	// request declares the job a second time, which is the bill this whole behaviour exists to stop.
	sc.Step(`^the job still names the pull request "([^"]*)"$`, func(ctx context.Context, want string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequest() != want {
			return fmt.Errorf("the job names the pull request %q, want %q", one.GetPullRequest(), want)
		}
		return nil
	})
}

// continueTheJob is the operator continuing the scenario's job, keeping whatever the system said so
// a scenario about the refusal can read it.
func continueTheJob(ctx context.Context) error {
	w := worldFrom(ctx)
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	_, w.lastErr = w.client.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: one.GetId()})
	return w.lastErr
}

func summariesOf(steps []*quaycrewv1.JobStep) []string {
	said := make([]string, 0, len(steps))
	for _, one := range steps {
		said = append(said, one.GetSummary())
	}
	return said
}
