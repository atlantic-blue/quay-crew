package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/cucumber/godog"
)

// The briefing, read back through a real control plane. What a table test in internal/web cannot say
// is that the page answers from jobs the system actually ran.

// theBriefingPullRequest is the address the session in these scenarios answers with. The system reads
// it off the answer rather than believing a report of one.
const theBriefingPullRequest = "https://github.com/atlantic-blue/quay-crew/pull/454"

func initializeBriefingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator opens the briefing$`, func(ctx context.Context) error {
		return webFrom(ctx).visit(ctx, "/")
	})

	// A job the model would not do. Two ticks: one sends the task, one reads what came back and lands
	// the job on it.
	sc.Step(`^a job titled "([^"]*)" that the model refused, saying "([^"]*)"$`,
		func(ctx context.Context, title, said string) error {
			w := worldFrom(ctx)
			w.runner.failTheNextTaskWith(said)
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{Title: title, Brief: "open it"}); err != nil {
				return err
			}
			if w.lastErr != nil {
				return w.lastErr
			}
			return tickUntilTheJobIs(ctx, job.PhaseFailed)
		})

	sc.Step(`^a job titled "([^"]*)" that landed a pull request$`, func(ctx context.Context, title string) error {
		w := worldFrom(ctx)
		w.runner.willSay("Pushed the branch and opened " + theBriefingPullRequest)
		if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: title, Brief: "sort it",
			Repository: "atlantic-blue/quay-crew", Mode: model.PermissionModeOnTheNetwork(),
		}); err != nil {
			return err
		}
		if w.lastErr != nil {
			return w.lastErr
		}
		return tickUntilTheJobIs(ctx, job.PhaseDone)
	})

	sc.Step(`^the briefing says nothing is waiting on the operator$`,
		theBriefingSays("Nothing is waiting on you."))
	sc.Step(`^the briefing says nothing is blocked$`, theBriefingSays("Nothing is blocked."))
	sc.Step(`^the briefing says the system has produced nothing$`,
		theBriefingSays("The system has produced nothing yet."))
	sc.Step(`^the briefing says the checks were not read$`, theBriefingSays("checks not read"))
	sc.Step(`^the briefing carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		return theBriefingSays(want)(ctx)
	})

	// Under, rather than anywhere on the page. A row in the wrong block tells the operator to do
	// something about work that needs nothing from him.
	sc.Step(`^the briefing carries "([^"]*)" under "([^"]*)"$`,
		func(ctx context.Context, want, block string) error {
			return inTheBlock(ctx, block, want)
		})

	sc.Step(`^the briefing carries the command that answers that job$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		return theBriefingSays("krewe job answer " + display.ShortID(one.GetId()))(ctx)
	})

	sc.Step(`^"([^"]*)" comes before "([^"]*)" on the briefing$`,
		func(ctx context.Context, first, second string) error {
			body := webFrom(ctx).body
			at, after := strings.Index(body, blockMark(first)), strings.Index(body, blockMark(second))
			if at < 0 || after < 0 {
				return fmt.Errorf("the briefing has no %q or no %q block:\n%s", first, second, body)
			}
			if at > after {
				return fmt.Errorf("%q is below %q on the briefing", first, second)
			}
			return nil
		})

	// The way off the old front door, tested beside the way onto the new one.
	sc.Step(`^the briefing lists no sessions$`, func(ctx context.Context) error {
		if strings.Contains(webFrom(ctx).body, `<li class="session">`) {
			return fmt.Errorf("the front door is still the session listing:\n%s", webFrom(ctx).body)
		}
		return nil
	})

	sc.Step(`^the session listing still carries that conversation$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.tasks) == 0 {
			return fmt.Errorf("nothing was dispatched, so there is no conversation to find")
		}
		if err := webFrom(ctx).visit(ctx, "/sessions"); err != nil {
			return err
		}
		return theBriefingSays(w.tasks[len(w.tasks)-1].sessionID)(ctx)
	})

	sc.Step(`^the web view refuses, and says a reader would have to be authenticated first$`,
		func(ctx context.Context) error {
			err := webFrom(ctx).err
			if err == nil {
				return fmt.Errorf("the web view served an address that is reachable from another machine")
			}
			for _, wanted := range []string{"this machine only", "authenticated", "encrypted"} {
				if !strings.Contains(err.Error(), wanted) {
					return fmt.Errorf("the refusal does not say %q: %v", wanted, err)
				}
			}
			return nil
		})
}

// blockMark is how a block of the briefing is found in what the browser received.
func blockMark(id string) string { return `id="` + id + `"` }

// inTheBlock says whether the page carries something between one block's heading and the next one's,
// which is what puts a row in a block rather than merely on the page.
func inTheBlock(ctx context.Context, id, want string) error {
	body := webFrom(ctx).body
	from := strings.Index(body, blockMark(id))
	if from < 0 {
		return fmt.Errorf("the briefing has no %q block:\n%s", id, body)
	}
	rest := body[from+len(blockMark(id)):]
	if next := strings.Index(rest, `<section class="block"`); next >= 0 {
		rest = rest[:next]
	}
	if !strings.Contains(rest, want) {
		return fmt.Errorf("the %q block does not carry %q:\n%s", id, want, rest)
	}
	return nil
}

func theBriefingSays(want string) func(context.Context) error {
	return func(ctx context.Context) error {
		if !strings.Contains(webFrom(ctx).body, want) {
			return fmt.Errorf("the briefing does not carry %q:\n%s", want, webFrom(ctx).body)
		}
		return nil
	}
}

// tickUntilTheJobIs moves the system on until the job the scenario declared reaches a phase. Nothing
// runs the controller in a scenario, so a tick here is what its own timer does in a running system.
func tickUntilTheJobIs(ctx context.Context, phase string) error {
	w := worldFrom(ctx)
	var last *quaycrewv1.Job
	for range 10 {
		w.server.TickJob(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		last = one
		if one.GetPhase() == phase {
			return nil
		}
	}
	return fmt.Errorf("the job is %q saying %q, want %q", last.GetPhase(), last.GetReason(), phase)
}
