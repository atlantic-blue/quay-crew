package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job that stops for a person tells them, driven the way a person meets it: the session asks, the
// console redraws on its own timer, and the next command an operator types says it first.
//
// The assertions go past the read. What decides whether this works is what somebody is left looking
// at, so these read the console's screen and the tool's own error stream rather than stopping at the
// count the control plane answered with.

// theSealedToken is a value the workspace keeps sealed, quoted inside a question the way a session
// quotes whatever was in front of it.
const theSealedToken = "ghp_thetokenthatmustnotreachascreen"

func initializeTellingSteps(sc *godog.ScenarioContext) {
	// The console as it runs in front of an operator: given the system to ask and a bell to ring, the
	// way console.Run gives it both. The other console scenarios open one without either, because what
	// they are about is a key and a row.
	sc.Step(`^the operator is looking at the console$`, func(ctx context.Context) error {
		c, w := consoleFrom(ctx), worldFrom(ctx)
		if err := c.openModel(w); err != nil {
			return err
		}
		c.model = c.model.WithClient(w.client).WithBell(func() { c.rung++ })
		return nil
	})

	// The console redrawing on its own, which is what its three second clock does. It is asked for
	// here rather than by waiting for the clock, because a suite that waited three seconds for each
	// of these is a suite nobody runs.
	sc.Step(`^the console draws again on its own$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		next, err := drive(c.model, c.model.Opening())
		if err != nil {
			return err
		}
		c.model = next
		return nil
	})

	sc.Step(`^the console rang the bell once$`, func(ctx context.Context) error {
		if rung := consoleFrom(ctx).rung; rung != 1 {
			return fmt.Errorf("the console rang %d times, and a person looking at another tab hears nothing", rung)
		}
		return nil
	})

	sc.Step(`^the console rang the bell no times$`, func(ctx context.Context) error {
		if rung := consoleFrom(ctx).rung; rung != 0 {
			return fmt.Errorf("the console rang %d times with nothing waiting", rung)
		}
		return nil
	})

	sc.Step(`^the console says the job is waiting, and what it asks$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		screen := plain(consoleFrom(ctx).model.View())
		for _, want := range []string{"1 job waits for you", one.GetId()[:8], "aurora serverless"} {
			if !strings.Contains(screen, want) {
				return fmt.Errorf("the console does not say %q:\n%s", want, screen)
			}
		}
		return nil
	})

	sc.Step(`^the console says nothing about anything waiting$`, func(ctx context.Context) error {
		if screen := plain(consoleFrom(ctx).model.View()); strings.Contains(screen, "waits for you") {
			return fmt.Errorf("the console says something waits while nothing does:\n%s", screen)
		}
		return nil
	})

	sc.Step(`^a workspace where a wait lasts (\d+) seconds?$`, func(ctx context.Context, seconds int) error {
		w := worldFrom(ctx)
		held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
			Workspace: w.workspaceID,
		})
		if err != nil {
			return err
		}
		asked := held.GetLimits()
		asked.WaitingSeconds = int32(seconds)
		_, err = w.client.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{Limits: asked})
		return err
	})

	// The clock, because this is the one thing in the feature that is genuinely a length of time. A
	// second, so the wait is real and the suite still runs in the time it always did.
	sc.Step(`^the wait passes that limit$`, func(ctx context.Context) error {
		time.Sleep(1100 * time.Millisecond)
		return nil
	})

	sc.Step(`^the telling names how long the job has waited$`, func(ctx context.Context) error {
		waiting, err := whatWaits(ctx, "a scenario")
		if err != nil {
			return err
		}
		if len(waiting) != 1 {
			return fmt.Errorf("%d jobs wait for a person", len(waiting))
		}
		if !waiting[0].GetOverLimit() {
			return fmt.Errorf("a wait of %d seconds does not read as past the limit",
				waiting[0].GetWaitedSeconds())
		}
		return nil
	})

	sc.Step(`^the operator runs any command$`, func(ctx context.Context) error {
		if err := listenForTool(ctx); err != nil {
			return err
		}
		return runTool(ctx, "workspace", "list")
	})

	sc.Step(`^the command says the job is waiting above its own output$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		t := toolFrom(ctx)
		if t.exitCode != 0 {
			return fmt.Errorf("the command exited %d, so the telling cost the operator their command", t.exitCode)
		}
		// On the error stream, so the answer on standard output is still the answer: a caller piping
		// a listing into something else must not find a line about a job in it.
		for _, want := range []string{"1 job waits for you", one.GetId()[:8]} {
			if err := says("standard error", t.stderr, want); err != nil {
				return err
			}
		}
		if strings.Contains(t.stdout, "waits for you") {
			return fmt.Errorf("the telling is on standard output, where it becomes part of the answer:\n%s", t.stdout)
		}
		if !strings.Contains(t.stdout, "acme") {
			return fmt.Errorf("the command's own output did not arrive:\n%s", t.stdout)
		}
		return nil
	})

	sc.Step(`^the workspace seals a token$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace: w.workspaceID, Key: "GITHUB_TOKEN", Value: theSealedToken,
		})
		return err
	})

	sc.Step(`^the session running that job asked about that token$`, func(ctx context.Context) error {
		if err := askAsTheSession(ctx,
			"the forge refused "+theSealedToken+", do i open a new one?", ""); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^the telling says what was asked and not the token$`, func(ctx context.Context) error {
		waiting, err := whatWaits(ctx, "a scenario")
		if err != nil {
			return err
		}
		if len(waiting) != 1 {
			return fmt.Errorf("%d jobs wait for a person", len(waiting))
		}
		if strings.Contains(waiting[0].GetWant(), theSealedToken) {
			return fmt.Errorf("the telling prints a sealed value: %q", waiting[0].GetWant())
		}
		if !strings.Contains(waiting[0].GetWant(), "do i open a new one?") {
			return fmt.Errorf("redacting took the question with it: %q", waiting[0].GetWant())
		}
		return nil
	})

	sc.Step(`^two surfaces draw the same waiting job$`, func(ctx context.Context) error {
		for _, surface := range []string{"console", "command line"} {
			if _, err := whatWaits(ctx, surface); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the record holds one telling, naming the surface that carried it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		events, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
		if err != nil {
			return err
		}
		var raised []string
		for _, event := range events {
			if event.Kind == job.EventRaised {
				raised = append(raised, event.Detail)
			}
		}
		if len(raised) != 1 {
			return fmt.Errorf("%d records of the telling for one wait: %v", len(raised), raised)
		}
		if raised[0] != "console" {
			return fmt.Errorf("the record says the telling was carried by %q, and the console drew it first", raised[0])
		}
		return nil
	})

	sc.Step(`^krewe job show prints the gap between the question and the telling$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if err := listenForTool(ctx); err != nil {
			return err
		}
		if err := runTool(ctx, "job", "show", one.GetId()); err != nil {
			return err
		}
		t := toolFrom(ctx)
		for _, want := range []string{"asked at:", "told at:", "the wait was carried after"} {
			if err := says("standard output", t.stdout, want); err != nil {
				return err
			}
		}
		return nil
	})

	// The shape the gap was wrong in: a question, an answer, more work, and then a failure. The row
	// still carries the moment of the question, and dating this wait from it reported the answer and
	// the whole run as time somebody spent not knowing.
	sc.Step(`^a person answered it and the job ran on$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if _, err := w.client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
			Id: one.GetId(), Answer: "the key value store, on demand",
		}); err != nil {
			return err
		}
		// The task that put the question is let go and waited for before the answer starts another
		// one. Two tasks in the model double at once is what makes the step below a race: it fails
		// "the next task", and the next task to reach the double is whichever of the two the runtime
		// wakes first. The one that asked belongs to an attempt the answer has already superseded, so
		// its failure is discarded and the job runs to done with nobody waiting on it.
		if w.release != nil {
			w.release()
			w.release = nil
		}
		if err := w.settled(ctx); err != nil {
			return err
		}
		// The work the answer starts, held, so the step below decides what it does rather than racing
		// it to the finish.
		w.release = w.runner.hold()
		w.server.TickJob(ctx)
		return w.runner.waitForTask()
	})

	sc.Step(`^that job fails and a surface names it$`, func(ctx context.Context) error {
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
		if _, err := whatWaits(ctx, "console"); err != nil {
			return err
		}
		return nil
	})

	sc.Step(`^krewe job show dates the wait from the failure, not from the answered question$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetRaisedAt() == nil {
				return fmt.Errorf("no surface named this wait, so there is no gap to read")
			}
			if err := listenForTool(ctx); err != nil {
				return err
			}
			if err := runTool(ctx, "job", "show", one.GetId()); err != nil {
				return err
			}
			t := toolFrom(ctx)
			if err := says("standard output", t.stdout, "the wait was carried after"); err != nil {
				return err
			}
			// The question belonged to a wait that ended, so this reading must not carry it into this
			// one. The arithmetic itself is held by the unit tests, which can put the question hours
			// before the failure. A scenario runs both inside one second, so a gap measured from
			// either moment reads the same here and this cannot tell the two apart.
			if strings.Contains(t.stdout, "asked at:") {
				return fmt.Errorf("the reading dates this wait from the answered question:\n%s",
					t.stdout)
			}
			return nil
		})
}

// whatWaits asks the system what waits for a person, as one surface.
func whatWaits(ctx context.Context, surface string) ([]*quaycrewv1.Waiting, error) {
	answer, err := worldFrom(ctx).client.GetWaiting(ctx, &quaycrewv1.GetWaitingRequest{Surface: surface})
	if err != nil {
		return nil, err
	}
	return answer.GetWaiting(), nil
}
