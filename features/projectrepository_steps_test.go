package features_test

import (
	"context"
	"fmt"
	"os"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/repository"
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

	sc.Step(`^the repository is (public|private), and the system says its pipeline minutes are (free|metered)$`,
		func(ctx context.Context, kind, bill string) error {
			held, err := theProject(ctx)
			if err != nil {
				return err
			}
			if held.GetVisibility() != kind {
				return fmt.Errorf("the repository is %q, want %q", held.GetVisibility(), kind)
			}
			if said := repository.Costs(held.GetVisibility()); !strings.Contains(said, bill) {
				return fmt.Errorf("the system says %q, want it to say the minutes are %s", said, bill)
			}
			return nil
		})

	sc.Step(`^the control plane refuses it as invalid, saying how to write a repository$`,
		theRefusalSays("atlantic-blue/quay-krewe"))

	sc.Step(`^the control plane refuses it as invalid, naming the two kinds$`, func(ctx context.Context) error {
		for _, kind := range []string{repository.Public, repository.Private} {
			if err := theRefusalSays(kind)(ctx); err != nil {
				return err
			}
		}
		return nil
	})

	// What the session doing the job is actually sent, which is the half a record cannot buy on its
	// own: the line the system puts in front of a brief is what tells a session where to push.
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

// theProject reads the project back out of the system, so an assertion is about what the system holds
// rather than about what a call answered.
func theProject(ctx context.Context) (*quaycrewv1.Project, error) {
	w := worldFrom(ctx)
	if w.lastErr != nil {
		return nil, fmt.Errorf("the system refused it: %w", w.lastErr)
	}
	resp, err := w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: w.projectID})
	if err != nil {
		return nil, err
	}
	return resp.GetProject(), nil
}

// What the person typing sees, driven through the real tool.
//
// The rest of this file drives the control plane, which is right for the record and the rule. The
// fault this covers is neither: it is one argument being read as the wrong thing by the tool, and
// the damage landing on a project the command never names. That is only visible from outside the
// process, in a home directory the operator is standing in.

type projectRepositoryKey struct{}

// projectRepositoryWorld is the home the operator stands in, and the two addresses the refusal has to
// name.
type projectRepositoryWorld struct {
	home string
	// scope is the address of the project the operator is standing in, which is the one the fault
	// overwrote and the one the refusal has to name.
	scope string
	// typed is the address the operator put on the command line.
	typed string
}

func projectRepositoryFrom(ctx context.Context) *projectRepositoryWorld {
	p, _ := ctx.Value(projectRepositoryKey{}).(*projectRepositoryWorld)
	return p
}

// atTheAddressOf is the address an operator types for a project in the scenario's workspace, built
// from the names rather than from identifiers, because names are what is in front of them.
func atTheAddressOf(ctx context.Context, project string) string {
	return worldFrom(ctx).workspaceName + "/" + project
}

func initializeProjectRepositoryToolSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		home, err := os.MkdirTemp("", "quaycrew-project-repository-")
		if err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, projectRepositoryKey{}, &projectRepositoryWorld{home: home}), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if scenario := projectRepositoryFrom(ctx); scenario != nil && scenario.home != "" {
			_ = os.RemoveAll(scenario.home)
		}
		return ctx, err
	})

	sc.Step(`^the operator is standing in the project "([^"]*)"$`, func(ctx context.Context, project string) error {
		scenario := projectRepositoryFrom(ctx)
		scenario.scope = atTheAddressOf(ctx, project)
		if err := runToolIn(ctx, scenario.home, "use", scenario.scope); err != nil {
			return err
		}
		if code := toolFrom(ctx).exitCode; code != 0 {
			return fmt.Errorf("krewe use exited %d, saying %q", code, toolFrom(ctx).stderr)
		}
		return nil
	})

	sc.Step(`^the project the operator is standing in works in "([^"]*)", which is (public|private)$`,
		func(ctx context.Context, address, kind string) error {
			if err := runToolIn(ctx, projectRepositoryFrom(ctx).home, "project", "repository", address, kind); err != nil {
				return err
			}
			if code := toolFrom(ctx).exitCode; code != 0 {
				return fmt.Errorf("recording the repository exited %d, saying %q", code, toolFrom(ctx).stderr)
			}
			return nil
		})

	// The command in the fault. One argument, and it happens to be the address of a project this
	// system holds.
	sc.Step(`^the operator types the address of the project "([^"]*)" as the whole command$`,
		func(ctx context.Context, project string) error {
			scenario := projectRepositoryFrom(ctx)
			scenario.typed = atTheAddressOf(ctx, project)
			return runToolIn(ctx, scenario.home, "project", "repository", scenario.typed)
		})

	sc.Step(`^the operator reads the project "([^"]*)" through the tool$`,
		func(ctx context.Context, project string) error {
			return runToolIn(ctx, projectRepositoryFrom(ctx).home,
				"project", "repository", "show", atTheAddressOf(ctx, project))
		})

	sc.Step(`^the operator records "([^"]*)" as the repository, saying no kind$`,
		func(ctx context.Context, address string) error {
			return runToolIn(ctx, projectRepositoryFrom(ctx).home, "project", "repository", address)
		})

	// Both readings and the spelling of each, and the project in scope by name, because that is the
	// one about to be overwritten and the command never mentions it.
	sc.Step(`^the refusal says how to read that project and how to record it here$`, func(ctx context.Context) error {
		scenario := projectRepositoryFrom(ctx)
		for _, wants := range []string{
			"krewe project repository show " + scenario.typed,
			"krewe project repository " + scenario.scope + " " + scenario.typed,
		} {
			if err := says("standard error", toolFrom(ctx).stderr, wants); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^reading the project "([^"]*)" back through the tool says it works in "([^"]*)", which is (public|private)$`,
		func(ctx context.Context, project, address, kind string) error {
			if err := runToolIn(ctx, projectRepositoryFrom(ctx).home,
				"project", "repository", "show", atTheAddressOf(ctx, project)); err != nil {
				return err
			}
			if err := says("standard output", toolFrom(ctx).stdout, address); err != nil {
				return err
			}
			return says("standard output", toolFrom(ctx).stdout, repository.Costs(kind))
		})

	sc.Step(`^reading the project "([^"]*)" back through the tool says it has no repository$`,
		func(ctx context.Context, project string) error {
			if err := runToolIn(ctx, projectRepositoryFrom(ctx).home,
				"project", "repository", "show", atTheAddressOf(ctx, project)); err != nil {
				return err
			}
			return says("standard output", toolFrom(ctx).stdout, "no repository")
		})

	// The line that turned a mistake into a confirmation. A write now says it wrote, and what it
	// wrote over.
	sc.Step(`^it says it recorded "([^"]*)", and that the project worked in "([^"]*)" before$`,
		func(ctx context.Context, now, before string) error {
			said := toolFrom(ctx).stdout
			for _, wants := range []string{"recorded", now, before, "before this"} {
				if err := says("standard output", said, wants); err != nil {
					return err
				}
			}
			return nil
		})
}
