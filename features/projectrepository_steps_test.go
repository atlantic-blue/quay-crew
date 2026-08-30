package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/repository"
	"github.com/cucumber/godog"
)

// Where a project's work lands, and what kind of repository that is.
//
// The steps drive the control plane rather than the command line tool, the way every other scenario
// here does, so what they hold up is the record and the rule rather than the wording of one client.

// initializeProjectRepositorySteps registers the steps for a project's repository.
func initializeProjectRepositorySteps(sc *godog.ScenarioContext) {
	record := func(ctx context.Context, address, kind string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.SetProjectRepository(ctx, &quaycrewv1.SetProjectRepositoryRequest{
			Project: w.projectID, Repository: address, Visibility: kind,
		})
		return nil
	}

	// A job with nothing declared about where its work goes, which is what the project's record is
	// there to answer.
	sc.Step(`^the caller declares a job$`, func(ctx context.Context) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "fetch the captions",
			Brief: "fetch the captions of a video and store them against its id",
		})
	})

	sc.Step(`^the (?:operator says the )?project's work lands in "([^"]*)"$`,
		func(ctx context.Context, address string) error {
			return record(ctx, address, "")
		})

	sc.Step(`^the operator says the project's private work lands in "([^"]*)"$`,
		func(ctx context.Context, address string) error {
			return record(ctx, address, repository.Private)
		})

	sc.Step(`^the operator says the project's work lands in "([^"]*)", of kind "([^"]*)"$`,
		func(ctx context.Context, address, kind string) error {
			return record(ctx, address, kind)
		})

	sc.Step(`^the project works in "([^"]*)"$`, func(ctx context.Context, want string) error {
		held, err := theProject(ctx)
		if err != nil {
			return err
		}
		if held.GetRepository() != want {
			return fmt.Errorf("the project works in %q, want %q", held.GetRepository(), want)
		}
		return nil
	})

	sc.Step(`^the repository is (public|private), and the crew says its pipeline minutes are (free|metered)$`,
		func(ctx context.Context, kind, bill string) error {
			held, err := theProject(ctx)
			if err != nil {
				return err
			}
			if held.GetVisibility() != kind {
				return fmt.Errorf("the repository is %q, want %q", held.GetVisibility(), kind)
			}
			if said := repository.Costs(held.GetVisibility()); !strings.Contains(said, bill) {
				return fmt.Errorf("the crew says %q, want it to say the minutes are %s", said, bill)
			}
			return nil
		})

	sc.Step(`^the control plane refuses it as invalid, saying how to write a repository$`,
		theRefusalSays("atlantic-blue/quay-crew"))

	sc.Step(`^the control plane refuses it as invalid, naming the two kinds$`, func(ctx context.Context) error {
		for _, kind := range []string{repository.Public, repository.Private} {
			if err := theRefusalSays(kind)(ctx); err != nil {
				return err
			}
		}
		return nil
	})

	// What the session doing the job is actually sent, which is the half a record cannot buy on its
	// own: the line the crew puts in front of a brief is what tells a session where to push.
	sc.Step(`^the session doing it is asked to open a pull request against "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			asked := job.Asked(&job.Job{Brief: one.GetBrief(), Repository: one.GetRepository()})
			for _, phrase := range []string{want, "pull request", "Do not merge"} {
				if !strings.Contains(asked, phrase) {
					return fmt.Errorf("the session doing it is asked %q, want it to say %q", asked, phrase)
				}
			}
			return nil
		})

	// A job that claims nothing, so the session doing it is asked for nothing. It is the state every
	// job was in before a project could say where its work goes.
	sc.Step(`^the job works in nothing, and the session doing it is asked for no pull request$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetRepository() != "" {
				return fmt.Errorf("the job works in %q, want nothing", one.GetRepository())
			}
			asked := job.Asked(&job.Job{Brief: one.GetBrief(), Repository: one.GetRepository()})
			if strings.Contains(asked, "pull request") {
				return fmt.Errorf("the session doing it is asked %q, want nothing about a pull request", asked)
			}
			return nil
		})
}

// theProject reads the project back out of the crew, so an assertion is about what the crew holds
// rather than about what a call answered.
func theProject(ctx context.Context) (*quaycrewv1.Project, error) {
	w := worldFrom(ctx)
	if w.lastErr != nil {
		return nil, fmt.Errorf("the crew refused it: %w", w.lastErr)
	}
	resp, err := w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: w.projectID})
	if err != nil {
		return nil, err
	}
	return resp.GetProject(), nil
}
