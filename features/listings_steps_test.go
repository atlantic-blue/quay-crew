package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// The steps behind listings.feature. They drive the real command line tool, because the defect and
// the fix are both a sentence on the operator's screen: a listing that read one project and said
// nothing about it.
//
// The tool is run twice in most of these, once to move and once to list, which is why the tool's
// home survives a whole scenario: standing somewhere is what the second command reads.

func initializeListingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a second project named "([^"]*)" holding a job titled "([^"]*)"$`,
		func(ctx context.Context, project, title string) error {
			return aJobInASecondProject(ctx, project, title)
		})

	sc.Step(`^a second project named "([^"]*)" holding no jobs$`,
		func(ctx context.Context, project string) error {
			_, err := secondProject(ctx, project)
			return err
		})

	sc.Step(`^the operator moves to "([^"]*)" and lists the jobs through the tool$`,
		func(ctx context.Context, address string) error {
			if err := moveTo(ctx, address); err != nil {
				return err
			}
			return runTool(ctx, "job", "list")
		})

	sc.Step(`^the operator moves to "([^"]*)" and lists the sessions through the tool$`,
		func(ctx context.Context, address string) error {
			if err := moveTo(ctx, address); err != nil {
				return err
			}
			return runTool(ctx, "sessions")
		})

	sc.Step(`^the operator lists the jobs of the whole crew through the tool$`,
		func(ctx context.Context) error {
			return runTool(ctx, "job", "list", "crew")
		})

	sc.Step(`^standard output says "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("standard output", toolFrom(ctx).stdout, want)
	})
}

// moveTo stands the operator somewhere, and refuses to go on when the move failed. A scenario whose
// move was refused reads the crew from nowhere, which lists everything and passes for the wrong
// reason.
func moveTo(ctx context.Context, address string) error {
	if err := runTool(ctx, "use", address); err != nil {
		return err
	}
	if t := toolFrom(ctx); t.exitCode != 0 {
		return fmt.Errorf("moving to %s exited %d, saying %q", address, t.exitCode, t.stderr)
	}
	return nil
}

// secondProject makes another project in the same workspace, and leaves the scenario's own project
// alone: the point of these scenarios is that work sits one address away from where somebody looks.
func secondProject(ctx context.Context, name string) (string, error) {
	w := worldFrom(ctx)
	created, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: w.workspaceID, Name: name,
	})
	if err != nil {
		return "", fmt.Errorf("create the second project: %w", err)
	}
	return created.GetProject().GetId(), nil
}

func aJobInASecondProject(ctx context.Context, project, title string) error {
	w := worldFrom(ctx)
	id, err := secondProject(ctx, project)
	if err != nil {
		return err
	}
	if _, err := w.client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: id, Title: title, Brief: "open it and say what it needs",
	}); err != nil {
		return fmt.Errorf("declare the job in %s: %w", project, err)
	}
	return nil
}
