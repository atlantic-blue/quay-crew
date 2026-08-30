package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/cucumber/godog"
)

// The controller that makes declared job happen, driven one tick at a time.
//
// A tick rather than a ticker: what is specified is what one pass over the rows does, and waiting
// out a real interval would be slow when it passed and flaky when it did not.

func initializeJobControllerSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job titled "([^"]*)" that claims the answer carries "([^"]*)"$`,
		func(ctx context.Context, title, carries string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "open the bill and say when it is due", ExpectContains: carries,
			})
		})

	sc.Step(`^a job titled "([^"]*)" after the first$`, func(ctx context.Context, title string) error {
		first, err := firstJob(ctx)
		if err != nil {
			return err
		}
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: title, Brief: "pay it", After: []string{first.GetId()},
		})
	})

	sc.Step(`^a job titled "([^"]*)" in the role "([^"]*)"$`,
		func(ctx context.Context, title, role string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "read the open pull requests", Role: role,
			})
		})

	sc.Step(`^the controller ticks$`, func(ctx context.Context) error {
		worldFrom(ctx).server.TickJob(ctx)
		return nil
	})

	sc.Step(`^the controller ticks again$`, func(ctx context.Context) error {
		worldFrom(ctx).server.TickJob(ctx)
		return nil
	})

	sc.Step(`^the controller ticks (\d+) times$`, func(ctx context.Context, times int) error {
		for range times {
			worldFrom(ctx).server.TickJob(ctx)
		}
		return nil
	})

	// The caller's own context is cancelled, which is what a closed terminal does to the call it was
	// holding. What runs afterwards runs for nobody.
	sc.Step(`^the caller goes away and the controller ticks$`, func(ctx context.Context) error {
		calling, hangUp := context.WithCancel(ctx)
		hangUp()
		if _, err := worldFrom(ctx).client.ListJobs(calling, &quaycrewv1.ListJobsRequest{}); err == nil {
			return fmt.Errorf("the call the caller hung up on answered anyway, so the caller never went away")
		}
		worldFrom(ctx).server.TickJob(ctx)
		return nil
	})

	sc.Step(`^the task the controller sent lands$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release != nil {
			w.release()
			w.release = nil
		}
		return w.settled(ctx)
	})

	// A dispatch that lets go answers before the model is called, so the count is waited for rather
	// than read once: reading immediately would pass or fail on how fast the machine is.
	sc.Step(`^the crew was asked to run (\d+) tasks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		deadline := time.Now().Add(5 * time.Second)
		for w.runner.count() < want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := w.runner.count(); got != want {
			return fmt.Errorf("the crew was asked to run %d tasks, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the job is done, and its answer is what the model said$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseDone {
			return fmt.Errorf("the job is %q saying %q, want done", one.GetPhase(), one.GetReason())
		}
		// The double answers with what it was asked, so the answer names the brief the job carried.
		if !strings.Contains(one.GetAnswer(), one.GetBrief()) {
			return fmt.Errorf("the answer is %q, want what the model said about %q", one.GetAnswer(), one.GetBrief())
		}
		return nil
	})

	sc.Step(`^the job says which session did it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetSession() == "" {
			return fmt.Errorf("the job does not say which session did it")
		}
		// And that session is one the crew holds, so a person can open the conversation.
		if _, err := worldFrom(ctx).client.GetSession(ctx, &quaycrewv1.GetSessionRequest{
			Id: one.GetSession(),
		}); err != nil {
			return fmt.Errorf("the session on the job is not one the crew holds: %w", err)
		}
		return nil
	})

	sc.Step(`^the job carries the moment it started and the moment it finished$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetStartedAt() == nil || one.GetFinishedAt() == nil {
			return fmt.Errorf("the job started at %v and finished at %v", one.GetStartedAt(), one.GetFinishedAt())
		}
		return nil
	})

	// Counted on the record rather than on the model, because a model that has not answered yet has
	// still been asked, and asking twice is paying twice.
	sc.Step(`^one task is recorded against that job$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetSession() == "" {
			return fmt.Errorf("the job says no session, so nothing was asked of the crew")
		}
		// The controller lets go of its task, so the row is written by the goroutine running it rather
		// than by the call that started it. Waited for rather than read once: reading straight after
		// the tick asks whether the goroutine has been scheduled yet, which is a question about the
		// machine and not about the crew.
		var counted int
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			tasks, err := worldFrom(ctx).client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: one.GetSession()})
			if err != nil {
				return err
			}
			counted = len(tasks.GetTasks())
			if counted >= 1 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if counted != 1 {
			return fmt.Errorf("%d tasks are recorded against the job, want 1", counted)
		}
		return nil
	})

	sc.Step(`^the job is running$`, func(ctx context.Context) error {
		return jobIs(ctx, 0, job.PhaseRunning)
	})

	sc.Step(`^the job titled "([^"]*)" is pending$`, func(ctx context.Context, title string) error {
		one, err := jobTitled(ctx, title)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhasePending {
			return fmt.Errorf("%q is %q, want pending", title, one.GetPhase())
		}
		return nil
	})

	sc.Step(`^the job is failed, and the reason says what the model said$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseFailed {
			return fmt.Errorf("the job is %q, want failed", one.GetPhase())
		}
		if one.GetReason() == "" {
			return fmt.Errorf("the job failed and says nothing about why")
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason names what was claimed$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q, want stopped", one.GetPhase())
		}
		if !strings.Contains(one.GetReason(), one.GetExpectContains()) {
			return fmt.Errorf("the reason is %q, want it to name what the job claimed", one.GetReason())
		}
		return nil
	})

	sc.Step(`^the answer is still on the record$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetAnswer() == "" {
			return fmt.Errorf("the job carries no answer, and what the model said is how somebody works out why the claim failed")
		}
		return nil
	})

	sc.Step(`^the records for that job read "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)"$`,
		func(ctx context.Context, first, second, third, fourth string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			events, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
			if err != nil {
				return err
			}
			got := eventKinds(events)
			want := []string{first, second, third, fourth}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				return fmt.Errorf("the records read %v, want %v", got, want)
			}
			return nil
		})

	// The name cell of a listing, not the column behind it. What was broken is what an operator reads
	// while four jobs are running, so the assertion is on the words the row draws.
	sc.Step(`^the session doing that job is listed as "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			session, err := sessionDoingTheJob(ctx)
			if err != nil {
				return err
			}
			if named := display.SessionLabel(session); named != want {
				return fmt.Errorf("the name cell of the session doing the job says %q, want %q", named, want)
			}
			return nil
		})

	// The other half of that scenario: with nothing having described this conversation, the name in
	// the cell above can only have come from the declaration.
	sc.Step(`^nothing has described that conversation$`, func(ctx context.Context) error {
		session, err := sessionDoingTheJob(ctx)
		if err != nil {
			return err
		}
		if said := session.GetDescription(); said != "" {
			return fmt.Errorf("the crew described the conversation as %q while its task was still running", said)
		}
		return nil
	})
}

// readJob reads one of the scenario's jobs back off the crew, so an assertion is about
// what the crew holds rather than about what a call answered.
func readJob(ctx context.Context, which int) (*quaycrewv1.Job, error) {
	scenario := jobFrom(ctx)
	if len(scenario.declared) <= which {
		return nil, fmt.Errorf("%d jobs were declared in this scenario", len(scenario.declared))
	}
	found, err := worldFrom(ctx).client.GetJob(ctx, &quaycrewv1.GetJobRequest{
		Id: scenario.declared[which].GetId(),
	})
	if err != nil {
		return nil, err
	}
	return found.GetJob(), nil
}

func jobIs(ctx context.Context, which int, phase string) error {
	one, err := readJob(ctx, which)
	if err != nil {
		return err
	}
	if one.GetPhase() != phase {
		return fmt.Errorf("the job is %q saying %q, want %q", one.GetPhase(), one.GetReason(), phase)
	}
	return nil
}

func jobTitled(ctx context.Context, title string) (*quaycrewv1.Job, error) {
	scenario := jobFrom(ctx)
	for i, declared := range scenario.declared {
		if declared.GetTitle() == title {
			return readJob(ctx, i)
		}
	}
	return nil, fmt.Errorf("this scenario declared no job titled %q", title)
}

// The controller a scenario stands up beside the crew's own, so a death is one controller going away
// and another finding what it left. Both job over the same store, which is two control plane
// processes over one database.
func anotherController(ctx context.Context, name string, lease time.Duration) *controlplane.Server {
	w := worldFrom(ctx)
	return controlplane.NewServer(controlplane.Config{
		Store: w.crewStore(), Runner: w.taskRunner(), Provider: w.provider, Secrets: w.secrets,
		Storage: w.storage, Info: w.info, Events: w.eventLog(), Reachable: w.reachable,
		Skills: w.skills, SkillsHost: w.skillsDir, SandboxImage: "quaycrew-sandbox:test",
		ControllerName: name, JobLease: lease,
	})
}

func initializeJobLeaseSteps(sc *godog.ScenarioContext) {
	// A controller with a hold that runs out at once, ticked once and then never again. That is what
	// a controller killed after its task started leaves behind: a task running in the crew, and a
	// row nobody is renewing.
	sc.Step(`^the controller that started it goes away after the task starts$`, func(ctx context.Context) error {
		dying := anotherController(ctx, "controller-a", time.Millisecond)
		dying.TickJob(ctx)

		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseRunning {
			return fmt.Errorf("the job is %q after its controller ticked, want running", one.GetPhase())
		}
		return waitForTheLeaseToRunOut(ctx, one.GetId())
	})

	sc.Step(`^another controller ticks$`, func(ctx context.Context) error {
		anotherController(ctx, "controller-b", time.Minute).TickJob(ctx)
		return nil
	})

	sc.Step(`^the job is still held by the controller that started it$`, func(ctx context.Context) error {
		one, err := heldJob(ctx)
		if err != nil {
			return err
		}
		if one.LeaseOwner == "" {
			return fmt.Errorf("the job is held by nobody, and its controller has not gone anywhere")
		}
		if one.LeaseOwner == "controller-b" {
			return fmt.Errorf("the job was taken by a controller that arrived while its holder was alive")
		}
		if one.LeaseUntil == nil || !one.LeaseUntil.After(time.Now()) {
			return fmt.Errorf("the hold runs to %v, want a moment still ahead", one.LeaseUntil)
		}
		return nil
	})

	sc.Step(`^the records for that job say the job was taken over once, and started once$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			events, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
			if err != nil {
				return err
			}
			counted := map[string]int{}
			for _, event := range events {
				counted[event.Kind]++
			}
			if counted[job.EventReleased] != 1 {
				return fmt.Errorf("the records say the job was taken over %d times, want once: %v",
					counted[job.EventReleased], eventKinds(events))
			}
			// The absence is the evidence: one start means one task, and one task means one bill.
			if counted[job.EventStarted] != 1 {
				return fmt.Errorf("the records say the job was started %d times, want once: %v",
					counted[job.EventStarted], eventKinds(events))
			}
			return nil
		})
}

// heldJob reads the lease off the row, which no call puts on the wire: it is the one part of the
// status a reader is told to ignore.
func heldJob(ctx context.Context) (*job.Job, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	return worldFrom(ctx).store.GetJob(ctx, one.GetId())
}

// waitForTheLeaseToRunOut waits out a hold a scenario made short on purpose, so a death is a lease
// that expired rather than a clock a scenario had to fake.
func waitForTheLeaseToRunOut(ctx context.Context, id string) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		one, err := worldFrom(ctx).store.GetJob(ctx, id)
		if err != nil {
			return err
		}
		if one.LeaseUntil == nil || one.LeaseUntil.Before(time.Now()) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the hold on %s still runs to %v", id, one.LeaseUntil)
		}
		time.Sleep(time.Millisecond)
	}
}

// The steps for a job that names a role: the session it runs in, and the refusal that keeps a
// container from ever being built for it.
func initializeJobRoleSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job titled "([^"]*)" in the role "([^"]*)" requiring "([^"]*)"$`,
		func(ctx context.Context, title, named, material string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "from the job alone", Role: named, Requires: []string{material},
			})
		})

	// The session, not the row. What decides whether the boundary is real is the conversation the
	// crew actually built, and a row saying the name proves nothing about it.
	sc.Step(`^the session doing that job runs as the "([^"]*)" role$`,
		func(ctx context.Context, named string) error {
			session, err := sessionDoingTheJob(ctx)
			if err != nil {
				return err
			}
			if session.GetRole() != named {
				return fmt.Errorf("the session doing the work runs as %q, want %q", session.GetRole(), named)
			}
			return nil
		})

	sc.Step(`^the job is stopped, saying the "([^"]*)" role does not receive "([^"]*)"$`,
		func(ctx context.Context, named, material string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
			}
			for _, want := range []string{named, material, "declare the job without"} {
				if !strings.Contains(one.GetReason(), want) {
					return fmt.Errorf("the refusal says %q, want it to name %q", one.GetReason(), want)
				}
			}
			return nil
		})

	sc.Step(`^the job is stopped, and the reason names the "([^"]*)" role$`,
		func(ctx context.Context, named string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseStopped {
				return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
			}
			if !strings.Contains(one.GetReason(), named) {
				return fmt.Errorf("the refusal says %q, want it to name the %s role", one.GetReason(), named)
			}
			return nil
		})

	sc.Step(`^the job ran in no session$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetSession() != "" {
			return fmt.Errorf("the refused job ran in session %q, and no session should exist", one.GetSession())
		}
		return nil
	})
}

// sessionDoingTheJob is the session the first job's task went to, read off the crew rather than off
// the row: a row naming a session says nothing about what the crew actually built.
func sessionDoingTheJob(ctx context.Context) (*quaycrewv1.Session, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	if one.GetSession() == "" {
		return nil, fmt.Errorf("the job says no session, so there is no conversation to read")
	}
	w := worldFrom(ctx)
	listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: w.projectID})
	if err != nil {
		return nil, err
	}
	for _, session := range listed.GetSessions() {
		if session.GetId() == one.GetSession() {
			return session, nil
		}
	}
	return nil, fmt.Errorf("the crew holds no session %s", one.GetSession())
}
