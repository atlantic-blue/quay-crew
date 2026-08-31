package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
	"github.com/cucumber/godog"
)

// A job claims the piece of work it is doing. These steps declare over the real interface, and the
// two that matter run the tool in its own process: what a second caller reads, and that the command
// they typed failed rather than looking as though it worked.

func initializeClaimSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job claiming "([^"]*)"$`, func(ctx context.Context, claim string) error {
		if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "nothing claims a piece of work", Brief: "build the claim", Claim: claim,
		}); err != nil {
			return err
		}
		if err := worldFrom(ctx).lastErr; err != nil {
			return fmt.Errorf("the first job was refused: %w", err)
		}
		return nil
	})

	sc.Step(`^the caller declares a second job claiming "([^"]*)"$`, func(ctx context.Context, claim string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "build the same thing again", Brief: "build the claim", Claim: claim,
		})
	})

	sc.Step(`^the caller declares a job with a claim of (\d+) bytes$`, func(ctx context.Context, bytes int) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "build the claim", Brief: "build it", Claim: strings.Repeat("c", bytes),
		})
	})

	// A session that died leaves its job reading as running with nothing moving it. The row is
	// written the way the store holds one, because waiting for a claim to run out would take hours.
	sc.Step(`^a job that claimed "([^"]*)" and then stopped moving$`, func(ctx context.Context, claim string) error {
		w, scenario := worldFrom(ctx), jobFrom(ctx)
		long := time.Now().UTC().Add(-job.ClaimLife - time.Hour)
		crashed := &job.Job{
			ID: store.NewID(), Workspace: w.workspaceID, Project: w.projectID,
			Title: "nothing claims a piece of work", Brief: "build the claim",
			Version: 1, Phase: job.PhaseRunning, Claim: job.TidyClaim(claim),
			CreatedAt: long, UpdatedAt: long,
		}
		if err := w.store.CreateJob(ctx, crashed, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: crashed.ID,
			Workspace: w.workspaceID, Project: w.projectID, Detail: crashed.Title,
			OccurredAt: long,
		}); err != nil {
			return err
		}
		found, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: crashed.ID})
		if err != nil {
			return err
		}
		scenario.declared = append(scenario.declared, found.GetJob())
		return nil
	})

	sc.Step(`^the system refuses it and names the job holding the work$`, func(ctx context.Context) error {
		holder, err := theHolder(ctx)
		if err != nil {
			return err
		}
		refusal := worldFrom(ctx).lastErr
		if refusal == nil {
			return fmt.Errorf("the second declaration was accepted, so two jobs hold one piece of work")
		}
		for _, want := range []string{holder.GetId(), holder.GetClaim()} {
			if !strings.Contains(refusal.Error(), want) {
				return fmt.Errorf("the refusal does not say %q: %v", want, refusal)
			}
		}
		return nil
	})

	// How old the claim is decides what somebody does next: a claim taken a minute ago is a session
	// working, and one taken hours ago is a question for whoever declared it.
	sc.Step(`^the refusal says how old the claim is$`, func(ctx context.Context) error {
		refusal := worldFrom(ctx).lastErr
		if refusal == nil {
			return fmt.Errorf("the second declaration was accepted")
		}
		if !strings.Contains(refusal.Error(), "ago") {
			return fmt.Errorf("the refusal says nothing about how old the claim is: %v", refusal)
		}
		return nil
	})

	sc.Step(`^the system holds one job on that piece of work$`, func(ctx context.Context) error {
		holder, err := theHolder(ctx)
		if err != nil {
			return err
		}
		w := worldFrom(ctx)
		listed, err := w.client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		holding := 0
		for _, one := range listed.GetJobs() {
			if one.GetClaim() == holder.GetClaim() {
				holding++
			}
		}
		if holding != 1 {
			return fmt.Errorf("%d jobs claim %q, and two sessions would build the same slice",
				holding, holder.GetClaim())
		}
		return nil
	})

	sc.Step(`^the caller lists the jobs through the tool$`, func(ctx context.Context) error {
		return runTool(ctx, "job", "list", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller declares a job claiming "([^"]*)" through the tool$`,
		func(ctx context.Context, claim string) error {
			return runTool(ctx, "job", "create", whereTheProjectIs(ctx),
				"--title", "build the same thing again", "--brief", "build the claim", "--claim", claim)
		})

	sc.Step(`^standard output names the job holding the work$`, func(ctx context.Context) error {
		return theOutputNamesTheHolder(ctx, toolFrom(ctx).stdout, "standard output")
	})

	sc.Step(`^standard error names the job holding the work$`, func(ctx context.Context) error {
		return theOutputNamesTheHolder(ctx, toolFrom(ctx).stderr, "standard error")
	})
}

// theHolder is the job that took the piece of work, which every scenario here declares first.
func theHolder(ctx context.Context) (*quaycrewv1.Job, error) {
	scenario := jobFrom(ctx)
	if len(scenario.declared) == 0 {
		return nil, fmt.Errorf("no job was declared, so there is no holder to name")
	}
	return scenario.declared[0], nil
}

// theOutputNamesTheHolder holds a stream to naming the job that has the work, in the shortened form
// a listing prints, because that is the identifier a person then types.
func theOutputNamesTheHolder(ctx context.Context, out, stream string) error {
	holder, err := theHolder(ctx)
	if err != nil {
		return err
	}
	if !strings.Contains(out, display.ShortID(holder.GetId())) {
		return fmt.Errorf("%s does not name the job holding the work: %s", stream, out)
	}
	return nil
}
