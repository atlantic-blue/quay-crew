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
	sc.Step(`^the workspace works in "([^"]*)"$`, func(ctx context.Context, remote string) error {
		w := worldFrom(ctx)
		_, err := w.client.AddRepository(ctx, &quaycrewv1.AddRepositoryRequest{
			Workspace: w.workspaceID, Remote: remote,
		})
		return err
	})

	sc.Step(`^the operator adds the repository "([^"]*)"$`, func(ctx context.Context, remote string) error {
		w := worldFrom(ctx)
		_, err := w.client.AddRepository(ctx, &quaycrewv1.AddRepositoryRequest{
			Workspace: w.workspaceID, Remote: remote,
		})
		w.lastErr = err
		return nil
	})

	sc.Step(`^the operator stops working in "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		_, err := w.client.RemoveRepository(ctx, &quaycrewv1.RemoveRepositoryRequest{
			Workspace: w.workspaceID, Name: name,
		})
		return err
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
		var cloned []string
		for _, spec := range ran {
			for _, argument := range spec.Argv {
				if argument == remote {
					return nil
				}
			}
			cloned = append(cloned, spec.Argv[len(spec.Argv)-2])
		}
		return fmt.Errorf("it cloned %v, want %q among them", cloned, remote)
	})

	sc.Step(`^every clone it asked for was conditional on there being no checkout$`, func(ctx context.Context) error {
		ran := clones(ctx)
		if len(ran) == 0 {
			return fmt.Errorf("nothing was cloned at all")
		}
		for _, spec := range ran {
			// The guard is in the command, so a second turn asking again costs an exec and changes nothing.
			// Without it, the second turn either fails or throws away what the first one did.
			if !strings.Contains(spec.Argv[2], `"$2/.git"`) || !strings.Contains(spec.Argv[2], "||") {
				return fmt.Errorf("this clone runs whatever is already there: %q", spec.Argv[2])
			}
		}
		return nil
	})

	// Into the volume every session in the workspace shares, so a second conversation costs no second
	// copy of the history.
	sc.Step(`^it cloned into the workspace's volume$`, func(ctx context.Context) error {
		ran := clones(ctx)
		if len(ran) == 0 {
			return fmt.Errorf("nothing was cloned")
		}
		into := ran[0].Argv[len(ran[0].Argv)-1]
		if !strings.HasPrefix(into, sandbox.SharedPath+"/") {
			return fmt.Errorf("it cloned into %s, which is not the workspace's volume", into)
		}
		if strings.HasPrefix(into, sandbox.WorkingPath) {
			return fmt.Errorf("it cloned into %s, which belongs to one session, so every session would get its own copy", into)
		}
		return nil
	})

	// Its own, because git allows one working tree per branch and two conversations in one directory
	// share an index.
	sc.Step(`^the session was given its own working tree of "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		for _, box := range w.provider.Boxes {
			for _, spec := range box.Ran {
				if len(spec.Argv) < 3 || !strings.Contains(spec.Argv[2], "worktree add") {
					continue
				}
				at := strings.Join(spec.Argv, " ")
				if !strings.Contains(at, sandbox.WorktreePath(current.sessionID, name)) {
					continue
				}
				if !strings.Contains(at, sandbox.SessionBranch(current.sessionID)) {
					return fmt.Errorf("the working tree is not on this session's own branch: %v", spec.Argv)
				}
				return nil
			}
		}
		return fmt.Errorf("the session was never given a working tree of %s", name)
	})

	sc.Step(`^both sessions were pointed at the same clone in the volume$`, func(ctx context.Context) error {
		ran := clones(ctx)
		if len(ran) < 2 {
			return fmt.Errorf("only %d sessions were asked to clone, want both", len(ran))
		}
		into := ran[0].Argv[len(ran[0].Argv)-1]
		for _, spec := range ran[1:] {
			if got := spec.Argv[len(spec.Argv)-1]; got != into {
				return fmt.Errorf("one clones into %s and another into %s, so the workspace keeps two copies", into, got)
			}
		}
		return nil
	})

	sc.Step(`^the two sessions were given different working trees on different branches$`, func(ctx context.Context) error {
		var at, branches []string
		for _, box := range worldFrom(ctx).provider.Boxes {
			for _, spec := range box.Ran {
				if len(spec.Argv) < 7 || !strings.Contains(spec.Argv[2], "worktree add") {
					continue
				}
				// The registered path, which is what the shared clone records, and the branch. The
				// arguments after the script are: the shell's name, the clone, the tree, the link, the
				// branch.
				at = append(at, spec.Argv[5])
				branches = append(branches, spec.Argv[len(spec.Argv)-1])
			}
		}
		if len(at) < 2 {
			return fmt.Errorf("only %d working trees were made, want one per session", len(at))
		}
		if at[0] == at[1] {
			return fmt.Errorf("both trees are registered at %s in one shared clone, so the second prunes the first", at[0])
		}
		// The one thing git refuses outright: the same branch checked out in two worktrees.
		if branches[0] == branches[1] {
			return fmt.Errorf("both working trees are on %s, which git will not allow", branches[0])
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
