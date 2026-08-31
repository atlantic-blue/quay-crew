package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/role"
	"github.com/atlantic-blue/krewe/internal/store"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Steps for the read a session makes instead of being told what the crew did.
//
// The jobs are seeded into the store rather than run, because what is specified here is the reading
// and not the running: a scenario cannot make a job cost 62,140 tokens or fail on a piped gate, and a
// history of nothing but pending jobs proves none of the arithmetic a reader depends on.

// historyState is what one scenario read back.
type historyState struct {
	read *quaycrewv1.GetHistoryResponse
	err  error
}

type historyKey struct{}

func historyFrom(ctx context.Context) *historyState {
	state, _ := ctx.Value(historyKey{}).(*historyState)
	return state
}

func initializeHistorySteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, historyKey{}, &historyState{}), nil
	})

	// The two days the operator had to type out by hand, which is the incident this read answers.
	sc.Step(`^the system did two days of work$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		for _, one := range []struct {
			title, phase, ranAs, pull, reason string
			tokens                            int64
			day                               int
			ran                               time.Duration
		}{
			{title: "a failed job is continued rather than repeated", phase: job.PhaseDone,
				ranAs: "implementer", tokens: 62_140, day: 29, ran: 18 * time.Minute,
				pull: "https://github.com/atlantic-blue/quay-crew/pull/531"},
			{title: "a job counts the steers it took", phase: job.PhaseDone,
				ranAs: "implementer", tokens: 41_000, day: 29, ran: 12 * time.Minute,
				pull: "https://github.com/atlantic-blue/quay-crew/pull/530"},
			{title: "prove the coverage gate ran", phase: job.PhaseFailed,
				ranAs: "verifier", tokens: 18_004, day: 30, ran: 4 * time.Minute,
				reason: "the gate was piped through tail, so its exit status said nothing"},
			{title: "write the release notes", phase: job.PhaseStopped,
				ranAs: "releaser", tokens: 900, day: 30, ran: time.Minute,
				reason: "stopped by the operator"},
			{title: "read the machine's headroom", phase: job.PhaseRunning,
				ranAs: "implementer", day: 30},
		} {
			declared := onAugust(one.day)
			written := &job.Job{
				ID: store.NewID(), Workspace: w.workspaceID, Project: w.projectID,
				Title: one.title, Brief: theBriefAHistoryNeverCarries,
				Answer: theAnswerAHistoryNeverCarries,
				Role:   one.ranAs, Phase: one.phase, SpentTokens: one.tokens,
				PullRequest: one.pull, Reason: one.reason,
				Version: 1, CreatedAt: declared, UpdatedAt: declared,
			}
			if one.ran > 0 {
				started, finished := declared, declared.Add(one.ran)
				written.StartedAt, written.FinishedAt = &started, &finished
			}
			if err := w.store.CreateJob(ctx, written, &job.Event{
				ID: store.NewID(), Kind: job.EventDeclared, Job: written.ID,
				Workspace: w.workspaceID, Project: w.projectID, OccurredAt: declared,
			}); err != nil {
				return fmt.Errorf("seeding %q: %w", one.title, err)
			}
		}
		return nil
	})

	sc.Step(`^a session reads the history from "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, since, until string) error {
			return readHistory(ctx, since, until, 0)
		})

	sc.Step(`^a session reads the history from "([^"]*)" to "([^"]*)", taking (\d+) jobs$`,
		func(ctx context.Context, since, until string, limit int) error {
			return readHistory(ctx, since, until, limit)
		})

	sc.Step(`^a session reads the history without naming a window$`, func(ctx context.Context) error {
		return readHistory(ctx, "", "", 0)
	})

	sc.Step(`^the history says (\d+) jobs ran$`, func(ctx context.Context, want int) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if got := int(read.GetTotal().GetJobs()); got != want {
			return fmt.Errorf("the history says %d jobs ran, and the window holds %d", got, want)
		}
		return nil
	})

	sc.Step(`^the history says (\d+) of them are done, (\d+) failed and (\d+) was stopped$`,
		func(ctx context.Context, done, failed, stopped int) error {
			read, err := historyRead(ctx)
			if err != nil {
				return err
			}
			total := read.GetTotal()
			if int(total.GetDone()) != done || int(total.GetFailed()) != failed ||
				int(total.GetStopped()) != stopped {
				return fmt.Errorf("the history says %d done, %d failed and %d stopped; want %d, %d and %d",
					total.GetDone(), total.GetFailed(), total.GetStopped(), done, failed, stopped)
			}
			// Every job is accounted for, or a reader is left with a total that does not match the
			// words under it.
			parts := total.GetDone() + total.GetFailed() + total.GetStopped() + total.GetUnfinished()
			if parts != total.GetJobs() {
				return fmt.Errorf("the endings add to %d and the total says %d", parts, total.GetJobs())
			}
			return nil
		})

	sc.Step(`^the history says the window cost (\d+) tokens$`, func(ctx context.Context, want int64) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if got := read.GetTotal().GetSpentTokens(); got != want {
			return fmt.Errorf("the history says the window cost %d tokens, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the history says (\d+) jobs opened a pull request$`, func(ctx context.Context, want int) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if got := int(read.GetTotal().GetPullRequests()); got != want {
			return fmt.Errorf("the history says %d jobs opened a pull request, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the failed job says "([^"]*)"$`, func(ctx context.Context, want string) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		for _, one := range read.GetJobs() {
			if one.GetPhase() != job.PhaseFailed {
				continue
			}
			if !strings.Contains(one.GetReason(), want) {
				return fmt.Errorf("the failure says %q, and does not say %q", one.GetReason(), want)
			}
			return nil
		}
		return fmt.Errorf("the history holds no failed job, so a reader cannot see what went wrong")
	})

	sc.Step(`^the history returns (\d+) jobs$`, func(ctx context.Context, want int) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if got := len(read.GetJobs()); got != want {
			return fmt.Errorf("the history returns %d jobs, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the history says (\d+) jobs were left out$`, func(ctx context.Context, want int) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if got := int(read.GetLeftOut()); got != want {
			return fmt.Errorf("the history says %d jobs were left out, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^no job in the history carries its brief or its answer$`, func(ctx context.Context) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if len(read.GetJobs()) == 0 {
			return fmt.Errorf("the history came back empty, so this proves nothing")
		}
		for _, one := range read.GetJobs() {
			rendered := one.String()
			if strings.Contains(rendered, theBriefAHistoryNeverCarries) ||
				strings.Contains(rendered, theAnswerAHistoryNeverCarries) {
				return fmt.Errorf("a job in the history carries the prose of the job: %s", rendered)
			}
		}
		return nil
	})

	sc.Step(`^the history says which window it read$`, func(ctx context.Context) error {
		read, err := historyRead(ctx)
		if err != nil {
			return err
		}
		if read.GetSince() == nil || read.GetUntil() == nil {
			return fmt.Errorf("the history does not say what window it read")
		}
		if got := read.GetUntil().AsTime().Sub(read.GetSince().AsTime()); got != job.DefaultWindow {
			return fmt.Errorf("the history read %s, want the last week of %s", got, job.DefaultWindow)
		}
		return nil
	})

	sc.Step(`^the system refuses it, saying the window ends before it starts$`, func(ctx context.Context) error {
		state := historyFrom(ctx)
		if state.err == nil {
			return fmt.Errorf("a window that ends before it starts was accepted")
		}
		if !strings.Contains(state.err.Error(), "ends before it starts") {
			return fmt.Errorf("the refusal says %q, and does not say the window ends before it starts", state.err)
		}
		return nil
	})

	sc.Step(`^a role holding no verbs is refused the history, and told to ask for "([^"]*)"$`,
		func(_ context.Context, verb string) error {
			refused := controlplane.DeniedToJob(
				quaycrewv1.ControlPlaneService_GetHistory_FullMethodName,
				&quaycrewv1.GetHistoryRequest{}, auth.Grant{Job: "job-1"})
			if refused == nil {
				return fmt.Errorf("a role holding no verbs read the history")
			}
			if !strings.Contains(refused.Error(), verb) {
				return fmt.Errorf("the refusal says %q, and does not name %q to ask for", refused, verb)
			}
			// And the same call goes through for a role that holds it, or the refusal is proving
			// nothing but that the call is blocked for everybody.
			allowed := controlplane.DeniedToJob(
				quaycrewv1.ControlPlaneService_GetHistory_FullMethodName,
				&quaycrewv1.GetHistoryRequest{}, auth.Grant{Job: "job-1", Verbs: []string{role.VerbJobRead}})
			if allowed != nil {
				return fmt.Errorf("a role holding %s was refused the history: %w", verb, allowed)
			}
			return nil
		})
}

// The prose a history must never carry. Named once, so the seed and the assertion cannot drift apart
// and quietly stop proving anything.
const (
	theBriefAHistoryNeverCarries  = "a brief nobody should have to read to know what happened"
	theAnswerAHistoryNeverCarries = "an answer nobody should have to read either"
)

// onAugust is a day of the two the crew worked, at nine in the morning.
func onAugust(day int) time.Time {
	return time.Date(2026, time.August, day, 9, 0, 0, 0, time.UTC)
}

// readHistory makes the call and keeps whatever came back, so a Then step can read either the answer
// or the refusal.
func readHistory(ctx context.Context, since, until string, limit int) error {
	w, state := worldFrom(ctx), historyFrom(ctx)
	request := &quaycrewv1.GetHistoryRequest{Project: w.projectID, Limit: int32(limit)}
	if since != "" {
		at, err := time.Parse(time.DateOnly, since)
		if err != nil {
			return err
		}
		request.Since = timestamppb.New(at)
	}
	if until != "" {
		at, err := time.Parse(time.DateOnly, until)
		if err != nil {
			return err
		}
		request.Until = timestamppb.New(at)
	}
	state.read, state.err = w.client.GetHistory(ctx, request)
	return nil
}

// historyRead is the answer, for a step that needs one. A refusal here is the scenario failing rather
// than the step asserting on it, because the steps that are about a refusal read state.err directly.
func historyRead(ctx context.Context) (*quaycrewv1.GetHistoryResponse, error) {
	state := historyFrom(ctx)
	if state.err != nil {
		return nil, fmt.Errorf("reading the history: %w", state.err)
	}
	if state.read == nil {
		return nil, fmt.Errorf("no history was read")
	}
	return state.read, nil
}
