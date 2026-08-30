package features_test

import (
	"context"
	"fmt"
	"os"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/role"
	"github.com/cucumber/godog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job counts the steers it took.
//
// The steps run the real tool, in one home directory that survives the whole scenario, because
// `krewe steer` with no job named reads where the operator is standing and that is kept on the
// machine the tool runs on.

type steersKey struct{}

// steersWorld is the tree this scenario built and the home the operator is standing in.
type steersWorld struct {
	home  string
	jobs  []*quaycrewv1.Job
	token string
	child *quaycrewv1.Job
}

func steersFrom(ctx context.Context) *steersWorld {
	s, _ := ctx.Value(steersKey{}).(*steersWorld)
	return s
}

func initializeSteersSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		home, err := os.MkdirTemp("", "quaycrew-steers-")
		if err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, steersKey{}, &steersWorld{home: home}), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if scenario := steersFrom(ctx); scenario != nil && scenario.home != "" {
			_ = os.RemoveAll(scenario.home)
		}
		return ctx, err
	})

	sc.Step(`^a job in flight titled "([^"]*)"$`, func(ctx context.Context, title string) error {
		return aJobToSteer(ctx, title)
	})

	sc.Step(`^the session running it declared a job of its own$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), steersFrom(ctx)
		declared, err := w.dialAs(scenario.token).CreateJob(ctx, &quaycrewv1.CreateJobRequest{
			Project: w.projectID, Title: "fetch the captions", Brief: "fetch them once and keep them",
			Role: steerRole,
		})
		if err != nil {
			return err
		}
		scenario.child = declared.GetJob()
		return nil
	})

	sc.Step(`^the operator steer(?:s|ed) the job in flight with "([^"]*)"$`,
		func(ctx context.Context, said string) error {
			return steerTheTool(ctx, steersFrom(ctx).jobs[0].GetId(), said)
		})

	sc.Step(`^the operator steer(?:s|ed) that child with "([^"]*)"$`,
		func(ctx context.Context, said string) error {
			child := steersFrom(ctx).child
			if child == nil {
				return fmt.Errorf("this scenario declared no child to steer")
			}
			return steerTheTool(ctx, child.GetId(), said)
		})

	// No identifier at all, which is the form that gets typed in the moment.
	sc.Step(`^the operator steers whatever is in flight with "([^"]*)"$`,
		func(ctx context.Context, said string) error {
			return steerTheTool(ctx, "", said)
		})

	sc.Step(`^a job titled "([^"]*)" that took (\d+) steers?$`,
		func(ctx context.Context, title string, count int) error {
			if err := aJobToSteer(ctx, title); err != nil {
				return err
			}
			landed := steersFrom(ctx).jobs[len(steersFrom(ctx).jobs)-1]
			for i := 0; i < count; i++ {
				if err := steerTheTool(ctx, landed.GetId(), fmt.Sprintf("it needed telling %d", i+1)); err != nil {
					return err
				}
				if toolFrom(ctx).exitCode != 0 {
					return fmt.Errorf("the mark was refused: %s", toolFrom(ctx).stderr)
				}
			}
			return nil
		})

	sc.Step(`^the operator reads the steers of that job back$`, func(ctx context.Context) error {
		return runToolIn(ctx, steersFrom(ctx).home, "steers", steersFrom(ctx).jobs[0].GetId())
	})

	sc.Step(`^the operator reads the steers of this project back$`, func(ctx context.Context) error {
		if err := standInTheProject(ctx); err != nil {
			return err
		}
		return runToolIn(ctx, steersFrom(ctx).home, "steers")
	})

	sc.Step(`^reading that job back through the tool says "([^"]*)"$`, func(ctx context.Context, want string) error {
		return readJobBack(ctx, steersFrom(ctx).jobs[0].GetId(), want)
	})

	sc.Step(`^reading the job at the top back through the tool says "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			return readJobBack(ctx, steersFrom(ctx).jobs[0].GetId(), want)
		})

	sc.Step(`^the report says "([^"]*)" before "([^"]*)"$`, func(ctx context.Context, first, second string) error {
		report := toolFrom(ctx).stdout
		at, then := strings.Index(report, first), strings.Index(report, second)
		if at < 0 || then < 0 {
			return fmt.Errorf("the report says %q, want both %q and %q in it", report, first, second)
		}
		if at > then {
			return fmt.Errorf("the report puts %q after %q, and they happened the other way round", first, second)
		}
		return nil
	})

	sc.Step(`^the report says "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("the report", toolFrom(ctx).stdout, want)
	})

	// The count alone says a job was hard. Which job each steer landed on says which part of it kept
	// needing a person, which is the half somebody acts on.
	sc.Step(`^the report names the job each steer landed on$`, func(ctx context.Context) error {
		scenario := steersFrom(ctx)
		for _, landed := range []*quaycrewv1.Job{scenario.jobs[0], scenario.child} {
			if err := says("the report", toolFrom(ctx).stdout, landed.GetId()[:8]); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the report says what a steer is$`, func(ctx context.Context) error {
		return says("the report", toolFrom(ctx).stdout, "should have known")
	})

	sc.Step(`^standard error says to name the job$`, func(ctx context.Context) error {
		return says("standard error", toolFrom(ctx).stderr, "krewe steer <job>")
	})

	// The score of a job is not the scored thing's to write.
	sc.Step(`^the session running it tries to record a steer$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), steersFrom(ctx)
		_, err := w.dialAs(scenario.token).RecordSteer(ctx, &quaycrewv1.RecordSteerRequest{
			Job: scenario.jobs[0].GetId(), Text: "score me kindly",
		})
		w.lastErr = err
		return nil
	})

	sc.Step(`^the system refuses it, saying what a session may call$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the call was accepted, so a session can write its own score")
		}
		if status.Code(w.lastErr) != codes.PermissionDenied {
			return fmt.Errorf("the refusal is %v, want it denied", w.lastErr)
		}
		return says("the refusal", w.lastErr.Error(), "job verbs")
	})
}

// steerRole is the role the job at the top runs as. A credential is minted for a job that runs as
// somebody, so a tree with no role at the top has no session to specify.
const steerRole = "transcript-builder"

// aJobToSteer declares one job in flight and keeps the credential a session running it would hold.
func aJobToSteer(ctx context.Context, title string) error {
	w, scenario := worldFrom(ctx), steersFrom(ctx)
	if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: roleFilesThatMay(steerRole, []string{role.VerbJobCreate, role.VerbJobRead}),
	}); err != nil {
		return err
	}
	if _, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: w.workspaceID, Name: steerRole,
	}); err != nil {
		return err
	}
	if _, err := w.client.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: w.workspaceID, MaxDepth: 2},
	}); err != nil {
		return err
	}
	declared, err := w.client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: w.projectID, Title: title, Brief: "build what the design describes", Role: steerRole,
	})
	if err != nil {
		return err
	}
	scenario.jobs = append(scenario.jobs, declared.GetJob())
	token, minted := w.server.JobCredentialForTest(ctx, declared.GetJob().GetId())
	if !minted {
		return fmt.Errorf("the system minted no credential for the job the session runs")
	}
	scenario.token = token
	return nil
}

// steerTheTool marks one moment the way the operator does, through the tool. With no identifier it
// reads where the operator is standing, so the move happens first, in the same home.
func steerTheTool(ctx context.Context, named, said string) error {
	if err := standInTheProject(ctx); err != nil {
		return err
	}
	args := []string{"steer"}
	if named != "" {
		args = append(args, named)
	}
	return runToolIn(ctx, steersFrom(ctx).home, append(args, said)...)
}

// standInTheProject moves the operator into the project, which every command that takes no address
// reads.
func standInTheProject(ctx context.Context) error {
	if err := runToolIn(ctx, steersFrom(ctx).home, "use", whereTheProjectIs(ctx)); err != nil {
		return err
	}
	if code := toolFrom(ctx).exitCode; code != 0 {
		return fmt.Errorf("krewe use exited %d, saying %q", code, toolFrom(ctx).stderr)
	}
	return nil
}

func readJobBack(ctx context.Context, id, want string) error {
	if err := runToolIn(ctx, steersFrom(ctx).home, "job", "show", id); err != nil {
		return err
	}
	return says("standard output", toolFrom(ctx).stdout, want)
}
