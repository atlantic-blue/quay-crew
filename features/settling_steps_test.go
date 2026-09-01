package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job that names a repository does not settle on its own answer: a reviewer and a tester read it
// first, in sessions that did not do the work.
//
// The scenarios drive the whole system, and each gate is a real session with a container of its own.
// What the gate said is read off the record of that session rather than off the double, because the
// record is what the system really saw.

func initializeSettlingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job titled "([^"]*)" in the repository "([^"]*)" with the gate off$`,
		func(ctx context.Context, title, repository string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "make the listing sort by the clock it shows",
				// In the mode that reaches the network, the way every job working in a repository is
				// declared: the clone, the push and the pull request all need it.
				Repository: repository, Mode: "dangerous", Ungated: true,
			})
		})

	// What each conversation answers, by what it was asked rather than by the order the system asks
	// in. Three are in flight here, and their order is the system's business.
	sc.Step(`^the (reviewer|tester) answers "([^"]*)"$`,
		func(ctx context.Context, gate, answer string) error {
			worldFrom(ctx).runner.willAnswer("You are the "+gate, answer)
			return nil
		})

	sc.Step(`^the session doing the work answers "([^"]*)"$`, func(ctx context.Context, answer string) error {
		worldFrom(ctx).runner.willSay(answer)
		return nil
	})

	sc.Step(`^the crew runs until the work comes back$`, func(ctx context.Context) error {
		return theCrewRuns(ctx, "the work to come back to the session that did it", func() (bool, error) {
			sent, err := tasksOfTheJob(ctx)
			if err != nil {
				return false, err
			}
			return len(sent) > 1 && job.SentBackByTheGate(sent[len(sent)-1].GetPrompt()), nil
		})
	})

	sc.Step(`^the crew runs until the job (stops|is done)$`, func(ctx context.Context, what string) error {
		want := job.PhaseStopped
		if what == "is done" {
			want = job.PhaseDone
		}
		return theCrewRuns(ctx, "the job to be "+want, func() (bool, error) {
			one, err := readJob(ctx, 0)
			if err != nil {
				return false, err
			}
			return one.GetPhase() == want, nil
		})
	})

	sc.Step(`^the (reviewer|tester) was asked to read "([^"]*)"$`,
		func(ctx context.Context, gate, address string) error {
			read, err := whatTheGateWasAsked(ctx, gate)
			if err != nil {
				return err
			}
			if len(read) == 0 {
				return fmt.Errorf("the %s was never asked anything", gate)
			}
			if !strings.Contains(read[0], address) {
				return fmt.Errorf("the %s was asked %q, want the address of the change", gate, read[0])
			}
			return nil
		})

	sc.Step(`^the (reviewer|tester) was never asked$`, func(ctx context.Context, gate string) error {
		read, err := whatTheGateWasAsked(ctx, gate)
		if err != nil {
			return err
		}
		if len(read) != 0 {
			return fmt.Errorf("the %s was asked %d times", gate, len(read))
		}
		return nil
	})

	sc.Step(`^the work went back to the session that did it, saying "([^"]*)"$`,
		func(ctx context.Context, reason string) error {
			sent, err := tasksOfTheJob(ctx)
			if err != nil {
				return err
			}
			if len(sent) < 2 {
				return fmt.Errorf("the session that did the work ran %d tasks, want the work and the fail", len(sent))
			}
			last := sent[len(sent)-1].GetPrompt()
			if !job.SentBackByTheGate(last) {
				return fmt.Errorf("the last task of that session is not the gate sending the work back: %q", last)
			}
			for _, want := range []string{reason, "pull request"} {
				if !strings.Contains(last, want) {
					return fmt.Errorf("the work went back saying %q, want it to say %q", last, want)
				}
			}
			return nil
		})

	sc.Step(`^the job says nothing passed it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetReviewed() || one.GetTested() {
			return fmt.Errorf("the job says reviewed=%v tested=%v, want neither",
				one.GetReviewed(), one.GetTested())
		}
		return nil
	})

	sc.Step(`^the job says the reviewer and the tester passed it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if !one.GetReviewed() || !one.GetTested() {
			return fmt.Errorf("the job says reviewed=%v tested=%v, want both",
				one.GetReviewed(), one.GetTested())
		}
		return nil
	})

	sc.Step(`^the job says it was declared with the gate off$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if !one.GetUngated() {
			return fmt.Errorf("the job was declared with the gate off and the row says it was on")
		}
		if one.GetReviewed() || one.GetTested() {
			return fmt.Errorf("a job with the gate off says reviewed=%v tested=%v",
				one.GetReviewed(), one.GetTested())
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason says the reviewer failed it twice$`,
		theReasonSays("twice", "the migration is missing"))

	sc.Step(`^the job is stopped, and the reason says the reviewer gave no verdict$`,
		theReasonSays("without a verdict", job.VerdictMarker))

	sc.Step(`^the answer on the row is still what the session that did the work said$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if !strings.Contains(one.GetAnswer(), "I made the change") {
				return fmt.Errorf("the answer is %q, want what the session that did the work said", one.GetAnswer())
			}
			return nil
		})

	// Each gate in a conversation of its own. A second opinion from the session that formed the first
	// is not a second opinion, and this is where that is either true or not.
	sc.Step(`^the reviewer and the tester each read it in a session of their own$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			held, err := sessionsByHandle(ctx)
			if err != nil {
				return err
			}
			for _, handle := range []string{
				job.SessionFor(one.GetId()), job.ReviewerFor(one.GetId()), job.TesterFor(one.GetId()),
			} {
				if held[handle] == "" {
					return fmt.Errorf("the system holds no session %q, so a gate ran in the session that "+
						"did the work", handle)
				}
			}
			return nil
		})

	// The boundary, read where it is real. A session is granted verbs by the job it runs, and a gate
	// runs no job. The session doing the work is the control: without it, a system that mints nobody a
	// credential would pass this.
	sc.Step(`^neither of them was given a credential, and the session doing the work was$`,
		func(ctx context.Context) error {
			w := worldFrom(ctx)
			working := false
			for i := 0; ; i++ {
				asked, found := w.runner.task(i)
				if !found {
					break
				}
				gate := ""
				for _, named := range []string{job.GateReviewer, job.GateTester} {
					if strings.Contains(asked.Text, "You are the "+named) {
						gate = named
					}
				}
				carried := asked.Env[auth.TokenEnv] != ""
				if gate != "" && carried {
					return fmt.Errorf("the %s was handed a credential, so it may call the system", gate)
				}
				if gate == "" && carried {
					working = true
				}
			}
			if !working {
				return fmt.Errorf("no session held a credential at all, so this says nothing about the gate")
			}
			return nil
		})

	sc.Step(`^reading that job says it was passed by the reviewer and the tester$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if err := runTool(ctx, "job", "show", one.GetId()); err != nil {
				return err
			}
			return says("standard output", toolFrom(ctx).stdout,
				fmt.Sprintf("passed by the %s and the %s", job.GateReviewer, job.GateTester))
		})
}

// theCrewRuns ticks the controller and lets each detached task land, until something is true.
//
// A crew left alone is a loop, so a scenario about what two gates decide cannot be written as a fixed
// number of ticks: the number depends on how many conversations are in flight, which is the system's
// business rather than the scenario's.
func theCrewRuns(ctx context.Context, waitingFor string, until func() (bool, error)) error {
	w := worldFrom(ctx)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w.server.TickJob(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		done, err := until()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	return fmt.Errorf("the crew never reached %s: the job is %q saying %q",
		waitingFor, one.GetPhase(), one.GetReason())
}

// tasksOfTheJob is the history of the session doing the job, which is where a fail coming back is
// read from.
func tasksOfTheJob(ctx context.Context) ([]*quaycrewv1.Task, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	if one.GetSession() == "" {
		return nil, nil
	}
	listed, err := worldFrom(ctx).client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: one.GetSession()})
	if err != nil {
		return nil, err
	}
	return listed.GetTasks(), nil
}

// whatTheGateWasAsked is every task the system sent one gate, in order, read off that session's own
// record rather than off the model double.
func whatTheGateWasAsked(ctx context.Context, gate string) ([]string, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	handle := job.ReviewerFor(one.GetId())
	if gate == job.GateTester {
		handle = job.TesterFor(one.GetId())
	}
	held, err := sessionsByHandle(ctx)
	if err != nil {
		return nil, err
	}
	if held[handle] == "" {
		return nil, nil
	}
	listed, err := worldFrom(ctx).client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: held[handle]})
	if err != nil {
		return nil, err
	}
	var asked []string
	for _, task := range listed.GetTasks() {
		asked = append(asked, task.GetPrompt())
	}
	return asked, nil
}

// sessionsByHandle is every conversation the project holds, by the handle the system named it with.
func sessionsByHandle(ctx context.Context) (map[string]string, error) {
	w := worldFrom(ctx)
	listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: w.projectID})
	if err != nil {
		return nil, err
	}
	by := make(map[string]string, len(listed.GetSessions()))
	for _, session := range listed.GetSessions() {
		by[session.GetHandle()] = session.GetId()
	}
	return by, nil
}
