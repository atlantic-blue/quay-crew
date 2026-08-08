package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/cucumber/godog"
)

// clones are the clone commands a session's sandbox was asked to run. It reads what the sandbox was
// actually told rather than what the crew meant to tell it.
func clones(ctx context.Context) []sandbox.Spec {
	w := worldFrom(ctx)
	var out []sandbox.Spec
	for _, box := range w.provider.Boxes {
		for _, spec := range box.Ran {
			// A clone is a shell running a script that says clone, with the remote as an argument. Setup
			// scripts and binary checks also run through a shell, so the script is what tells them apart.
			if len(spec.Argv) >= 3 && spec.Argv[0] == "sh" && strings.Contains(spec.Argv[2], "clone") {
				out = append(out, spec)
			}
		}
	}
	return out
}

// initializeCloneSteps covers where the code a session works in comes from.
func initializeCloneSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the project works in "([^"]*)"$`, func(ctx context.Context, remote string) error {
		w := worldFrom(ctx)
		_, err := w.client.SetProjectRemote(ctx, &quaycrewv1.SetProjectRemoteRequest{
			Project: w.projectID, Remote: remote,
		})
		return err
	})

	sc.Step(`^the operator sets the project's remote to "([^"]*)"$`, func(ctx context.Context, remote string) error {
		w := worldFrom(ctx)
		_, err := w.client.SetProjectRemote(ctx, &quaycrewv1.SetProjectRemoteRequest{
			Project: w.projectID, Remote: remote,
		})
		w.lastErr = err
		return nil
	})

	sc.Step(`^the operator clears the project's remote$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, err := w.client.SetProjectRemote(ctx, &quaycrewv1.SetProjectRemoteRequest{Project: w.projectID})
		return err
	})

	sc.Step(`^the project has no remote$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: w.projectID})
		if err != nil {
			return err
		}
		if got := resp.GetProject().GetRemote(); got != "" {
			return fmt.Errorf("the project still works in %q", got)
		}
		return nil
	})

	sc.Step(`^the clone will fail saying "([^"]*)"$`, func(ctx context.Context, said string) error {
		w := worldFrom(ctx)
		w.provider.Stderr = said
		w.provider.ExitErr = fmt.Errorf("exit status 128")
		return nil
	})

	sc.Step(`^the session cloned nothing$`, func(ctx context.Context) error {
		if ran := clones(ctx); len(ran) != 0 {
			return fmt.Errorf("it cloned anyway: %v", ran[0].Argv)
		}
		return nil
	})

	sc.Step(`^the session cloned "([^"]*)"$`, func(ctx context.Context, remote string) error {
		ran := clones(ctx)
		if len(ran) == 0 {
			return fmt.Errorf("nothing was cloned, so the session has no repository to work in")
		}
		for _, argument := range ran[0].Argv {
			if argument == remote {
				return nil
			}
		}
		return fmt.Errorf("it cloned %v, want %q among the arguments", ran[0].Argv, remote)
	})

	sc.Step(`^every clone it asked for was conditional on there being no checkout$`, func(ctx context.Context) error {
		ran := clones(ctx)
		if len(ran) == 0 {
			return fmt.Errorf("nothing was cloned at all")
		}
		for _, spec := range ran {
			// The guard is in the command, so a second turn asking again costs an exec and changes nothing.
			// Without it, the second turn either fails or throws away what the first one did.
			if !strings.Contains(spec.Argv[2], "-d") || !strings.Contains(spec.Argv[2], ".git") {
				return fmt.Errorf("this clone runs whatever is already there: %q", spec.Argv[2])
			}
		}
		return nil
	})

	// The working directory already holds the memory file the model reads, and git refuses to clone into
	// somewhere that is not empty.
	sc.Step(`^it cloned into a directory of its own under the working directory$`, func(ctx context.Context) error {
		ran := clones(ctx)
		if len(ran) == 0 {
			return fmt.Errorf("nothing was cloned")
		}
		into := ran[0].Argv[len(ran[0].Argv)-1]
		if into == sandbox.WorkingPath {
			return fmt.Errorf("it cloned into %s itself, which already has a memory file in it", into)
		}
		if !strings.HasPrefix(into, sandbox.WorkingPath+"/") {
			return fmt.Errorf("it cloned into %s, which is not under the working directory", into)
		}
		if strings.Count(strings.TrimPrefix(into, sandbox.WorkingPath+"/"), "/") != 0 {
			return fmt.Errorf("it cloned into %s, want one directory directly under the working directory", into)
		}
		return nil
	})

	// An argument list is readable by anything that can inspect the container, and it ends up in whatever
	// logs a command. The credential is read from the environment instead, by git, at the moment it asks.
	sc.Step(`^the clone never carried the credential in its arguments$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		ran := clones(ctx)
		if len(ran) == 0 {
			return fmt.Errorf("nothing was cloned")
		}
		joined := strings.Join(ran[0].Argv, " ")
		// Whatever the workspace actually holds, by name, so this cannot pass by the value being absent
		// from the store rather than from the arguments.
		for _, name := range []string{sandbox.CredentialEnv} {
			value, err := w.secrets.Get(ctx, w.workspaceID, name)
			if err == nil && value != "" && strings.Contains(joined, value) {
				return fmt.Errorf("the clone command carries the value of %s, which publishes it: %q", name, joined)
			}
		}
		if !strings.Contains(joined, "$"+sandbox.CredentialEnv) {
			return fmt.Errorf("the clone does not read the credential from the environment at all: %q", joined)
		}
		return nil
	})
}
