package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/forge"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/cucumber/godog"
)

// Steps for the read back: what the crew learns about a pull request it opened, and what it says when
// it learned nothing.
//
// The job is run through the controller rather than seeded, because the address gets onto the row by
// being read off an answer, and a scenario that wrote it there by hand would prove nothing about the
// path a real job takes.

// theOpenedPullRequest is the address every scenario here talks about, kept so a step can say "that
// pull request" rather than repeating it in every line.
type pullRequestKey struct{}

func openedIn(ctx context.Context) string {
	address, _ := ctx.Value(pullRequestKey{}).(*string)
	if address == nil {
		return ""
	}
	return *address
}

func initializePullRequestSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		opened := ""
		return context.WithValue(ctx, pullRequestKey{}, &opened), nil
	})

	// A whole job, run the way a real one runs: declared in a repository, dispatched, answered with an
	// address, and landed by the controller reading that address off the answer.
	sc.Step(`^a job that opened the pull request "([^"]*)"$`, func(ctx context.Context, address string) error {
		w := worldFrom(ctx)
		if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
			Repository: "atlantic-blue/quay-crew", Mode: "dangerous",
		}); err != nil {
			return err
		}
		if w.lastErr != nil {
			return w.lastErr
		}
		w.runner.willSay("done, opened " + address)
		w.server.TickJob(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		w.server.TickJob(ctx)

		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseDone {
			return fmt.Errorf("the job is %q saying %q, want done", one.GetPhase(), one.GetReason())
		}
		if one.GetPullRequest() != address {
			return fmt.Errorf("the job names the pull request %q, want %q", one.GetPullRequest(), address)
		}
		*ctx.Value(pullRequestKey{}).(*string) = address
		return nil
	})

	sc.Step(`^the forge will not answer about that pull request, saying "([^"]*)"$`,
		func(ctx context.Context, why string) error {
			worldFrom(ctx).pullRequests().Refuses(openedIn(ctx), why)
			return nil
		})

	sc.Step(`^this system holds no forge credential$`, func(ctx context.Context) error {
		worldFrom(ctx).noForgeCredential = true
		return nil
	})

	sc.Step(`^the forge says that pull request merged with its checks green$`, func(ctx context.Context) error {
		return theForgeSays(ctx, forge.Reading{
			Status: forge.StatusMerged, Checks: forge.ChecksGreen, Review: forge.ReviewApproved,
		})
	})

	sc.Step(`^the forge says that pull request is open with its checks green$`, func(ctx context.Context) error {
		return theForgeSays(ctx, forge.Reading{
			Status: forge.StatusOpen, Checks: forge.ChecksGreen, Review: forge.ReviewNone,
		})
	})

	sc.Step(`^the forge says that pull request is open and the check "([^"]*)" failed$`,
		func(ctx context.Context, check string) error {
			return theForgeSays(ctx, forge.Reading{
				Status: forge.StatusOpen, Checks: forge.ChecksRed, FailedCheck: check,
				Review: forge.ReviewNone,
			})
		})

	sc.Step(`^the forge says a review of that pull request asked for changes$`, func(ctx context.Context) error {
		return theForgeSays(ctx, forge.Reading{
			Status: forge.StatusOpen, Checks: forge.ChecksGreen, Review: forge.ReviewChangesRequested,
		})
	})

	sc.Step(`^the system reads the pull requests it opened$`, func(ctx context.Context) error {
		worldFrom(ctx).server.ReadPullRequests(ctx)
		return nil
	})

	sc.Step(`^the system reads the pull requests it opened again$`, func(ctx context.Context) error {
		worldFrom(ctx).server.ReadPullRequests(ctx)
		return nil
	})

	sc.Step(`^the caller reads the job (\d+) times$`, func(ctx context.Context, times int) error {
		for range times {
			if _, err := readJob(ctx, 0); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the caller reads the job three times$`, func(ctx context.Context) error {
		for range 3 {
			if _, err := readJob(ctx, 0); err != nil {
				return err
			}
		}
		return nil
	})

	// Unknown throughout, and not passing. The two halves are asserted apart because a row of empty
	// words and a row that says unknown look the same to a store and different to whoever reads it.
	sc.Step(`^the job says nothing is known about its pull request$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequestStatus() != forge.StatusUnknown {
			return fmt.Errorf("the job says its pull request is %q, want unknown", one.GetPullRequestStatus())
		}
		if one.GetPullRequestChecks() != forge.ChecksUnknown {
			return fmt.Errorf("the job says the checks are %q, want unknown", one.GetPullRequestChecks())
		}
		if one.GetPullRequestChecks() == forge.ChecksGreen || one.GetPullRequestChecks() == forge.ChecksNone {
			return fmt.Errorf("a pull request nobody read reads as %q", one.GetPullRequestChecks())
		}
		if one.GetPullRequestReview() != forge.ReviewUnknown {
			return fmt.Errorf("the job says the review is %q, want unknown", one.GetPullRequestReview())
		}
		return nil
	})

	sc.Step(`^the job carries no moment of a reading$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequestReadAt() != nil {
			return fmt.Errorf("a pull request nothing read was read at %s", one.GetPullRequestReadAt().AsTime())
		}
		if one.GetPullRequestFailed() != "" {
			return fmt.Errorf("a pull request nothing tried to read says %q", one.GetPullRequestFailed())
		}
		return nil
	})

	sc.Step(`^the job carries the moment it was read$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequestReadAt() == nil {
			return fmt.Errorf("the job carries no moment, so nothing can tell a stale reading from a fresh one")
		}
		return nil
	})

	sc.Step(`^the job says why it could not be read, naming "([^"]*)"$`,
		func(ctx context.Context, phrase string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if !strings.Contains(one.GetPullRequestFailed(), phrase) {
				return fmt.Errorf("the job says %q about why it could not be read, want it to name %q",
					one.GetPullRequestFailed(), phrase)
			}
			return nil
		})

	sc.Step(`^the job says its pull request merged$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequestStatus() != forge.StatusMerged {
			return fmt.Errorf("the job says its pull request is %q, want merged", one.GetPullRequestStatus())
		}
		return nil
	})

	sc.Step(`^the job says a check is red, naming "([^"]*)"$`, func(ctx context.Context, check string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequestChecks() != forge.ChecksRed {
			return fmt.Errorf("the job says the checks are %q, want red", one.GetPullRequestChecks())
		}
		if one.GetPullRequestCheck() != check {
			return fmt.Errorf("the job names %q as the failed check, want %q", one.GetPullRequestCheck(), check)
		}
		return nil
	})

	sc.Step(`^the job says a review asked for changes$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequestReview() != forge.ReviewChangesRequested {
			return fmt.Errorf("the job says the review is %q, want changes requested",
				one.GetPullRequestReview())
		}
		return nil
	})

	sc.Step(`^the forge was asked about that pull request once$`, func(ctx context.Context) error {
		if asked := worldFrom(ctx).pullRequests().Asked(openedIn(ctx)); asked != 1 {
			return fmt.Errorf("the forge was asked about it %d times, want once", asked)
		}
		return nil
	})

	sc.Step(`^the forge was asked about nothing$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if asked := w.pullRequests().Asked(one.GetPullRequest()); asked != 0 {
			return fmt.Errorf("the forge was asked %d times about a job that opened nothing", asked)
		}
		return nil
	})
}

func theForgeSays(ctx context.Context, reading forge.Reading) error {
	address := openedIn(ctx)
	if address == "" {
		return fmt.Errorf("this scenario has not opened a pull request yet")
	}
	worldFrom(ctx).pullRequests().Says(address, reading)
	return nil
}
