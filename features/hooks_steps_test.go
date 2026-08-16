package features_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/cucumber/godog"
)

// The hooks a crew holds, driven over the control plane's real interface. What a hook does once it is
// inside a sandbox is a different question, proved against a real container.
func initializeHookSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator imports a hook "([^"]*)" firing on "([^"]*)" for "([^"]*)"$`,
		func(ctx context.Context, name, event, matcher string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.ImportHook(ctx, importOf(name, 1, event, matcher, "exit 0"))
			return nil
		})

	sc.Step(`^a hook "([^"]*)" imported firing on "([^"]*)"$`,
		func(ctx context.Context, name, event string) error {
			w := worldFrom(ctx)
			_, err := w.client.ImportHook(ctx, importOf(name, 1, event, "", "exit 0"))
			w.lastErr = err
			return err
		})

	// The same name and version carrying different files, which is the import that has to be refused:
	// a workspace pins a version, so changing one underneath it changes a constraint a session is
	// already running under.
	sc.Step(`^the operator imports "([^"]*)" again at the same version carrying something different$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.ImportHook(ctx, importOf(name, 1, "PreToolUse", "", "exit 1"))
			return nil
		})

	sc.Step(`^the operator attaches the hook "([^"]*)" to the workspace$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return nil
		})

	sc.Step(`^the operator attaches the hook "([^"]*)" to the crew$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
				Scope: "crew", Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^the operator detaches the hook "([^"]*)" from the workspace$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.DetachHook(ctx, &quaycrewv1.DetachHookRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^the operator takes the hook "([^"]*)" off the crew$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.DetachHook(ctx, &quaycrewv1.DetachHookRequest{
				Scope: "crew", Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^another workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		created, err := w.client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: name})
		if err != nil {
			return err
		}
		w.otherWorkspaceID = created.GetWorkspace().GetId()
		return nil
	})

	sc.Step(`^the crew holds (\d+) hooks?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetHooks()); got != want {
			return fmt.Errorf("the crew holds %d hooks, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the workspace runs under (\d+) hooks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		return hooksHeld(ctx, w, w.workspaceID, want)
	})

	sc.Step(`^the other workspace runs under (\d+) hooks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		return hooksHeld(ctx, w, w.otherWorkspaceID, want)
	})

	sc.Step(`^the hook "([^"]*)" fires on "([^"]*)"$`,
		func(ctx context.Context, name, event string) error {
			listed, err := worldFrom(ctx).client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
			if err != nil {
				return err
			}
			for _, one := range listed.GetHooks() {
				if one.GetName() != name {
					continue
				}
				for _, binding := range one.GetEvents() {
					if binding.GetOn() == event {
						return nil
					}
				}
				return fmt.Errorf("%s fires on %+v, not on %s", name, one.GetEvents(), event)
			}
			return fmt.Errorf("the crew does not hold a hook called %s", name)
		})

	// Where a hook came from, so a listing does not leave the operator guessing why a workspace they
	// attached nothing to is under three constraints.
	sc.Step(`^that hook is reported as the crew's$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{
			Workspace: w.otherWorkspaceID,
		})
		if err != nil {
			return err
		}
		if len(listed.GetHooks()) == 0 {
			return fmt.Errorf("the workspace is under no hooks at all")
		}
		if !listed.GetHooks()[0].GetCrew() {
			return fmt.Errorf("the hook does not say it came from the crew: %+v", listed.GetHooks()[0])
		}
		return nil
	})

	sc.Step(`^the refusal names "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		if !strings.Contains(w.lastErr.Error(), want) {
			return fmt.Errorf("the refusal is %q and does not name %q", w.lastErr.Error(), want)
		}
		return nil
	})

	sc.Step(`^the refusal says to raise the version$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		// A refusal that only says no leaves the operator guessing. It has to name what would work.
		if !strings.Contains(w.lastErr.Error(), "Raise the version") {
			return fmt.Errorf("the refusal is %q and does not say to raise the version", w.lastErr.Error())
		}
		return nil
	})
}

func hooksHeld(ctx context.Context, w *world, workspace string, want int) error {
	listed, err := w.client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: workspace})
	if err != nil {
		return err
	}
	if got := len(listed.GetHooks()); got != want {
		return fmt.Errorf("the workspace runs under %d hooks, want %d", got, want)
	}
	return nil
}

// importOf is a whole hook on the wire: a manifest, and an entry point that has to stay executable.
// The body varies so two imports at one version can be made to differ.
func importOf(name string, version int, event, matcher, body string) *quaycrewv1.ImportHookRequest {
	manifest := fmt.Sprintf("name: %s\nversion: %d\nsummary: Refuses what nobody approved.\nevents:\n  - on: %s\n    entry: bin/hook\n",
		name, version, event)
	if matcher != "" {
		manifest += fmt.Sprintf("    matcher: %s\n", matcher)
	}
	return &quaycrewv1.ImportHookRequest{Files: []*quaycrewv1.HookFile{
		{Path: "hook.yaml", Body: []byte(manifest)},
		{Path: "bin/hook", Body: []byte("#!/bin/sh\n" + body + "\n"), Executable: true},
	}}
}

// The hooks this build ships, seeded the way the real main seeds them at startup.
func initializeSeededHookSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a crew seeded with the hooks this build ships$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.seedHooks = true
		w.server.SeedHooks(ctx, "../hooks", slog.New(slog.NewTextHandler(io.Discard, nil)))
		return nil
	})

	sc.Step(`^the crew holds the "([^"]*)" hook$`, func(ctx context.Context, name string) error {
		listed, err := worldFrom(ctx).client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
		if err != nil {
			return err
		}
		for _, one := range listed.GetHooks() {
			if one.GetName() == name {
				return nil
			}
		}
		return fmt.Errorf("the crew does not hold %s: %+v", name, listed.GetHooks())
	})
}

// Versions of a shipped hook, which is how a fix reaches a crew that already has one.
func initializeHookVersionSteps(sc *godog.ScenarioContext) {
	// The crew as it was before this build: holding, and under, an earlier version of a hook this
	// build also ships. Seeded by hand at version 1, so the shipped one is genuinely newer and the
	// upgrade has something to do.
	sc.Step(`^a crew already under version (\d+) of "([^"]*)"$`,
		func(ctx context.Context, version int, name string) error {
			w := worldFrom(ctx)
			w.seedHooks = true
			if _, err := w.client.ImportHook(ctx, importAt(name, version)); err != nil {
				return err
			}
			if _, err := w.client.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
				Scope: "crew", Name: name,
			}); err != nil {
				return err
			}
			return nil
		})

	sc.Step(`^the crew holds "([^"]*)" at the version this build ships$`,
		func(ctx context.Context, name string) error {
			want, err := shippedVersion(name)
			if err != nil {
				return err
			}
			got, err := newestHeld(ctx, name)
			if err != nil {
				return err
			}
			if got != want {
				// The catalogue is what the crew could run. A fix that never reaches it can never be
				// taken by anybody.
				return fmt.Errorf("the newest %s the crew holds is version %d, want %d", name, got, want)
			}
			return nil
		})

	sc.Step(`^the workspace is still under "([^"]*)" at version (\d+)$`,
		func(ctx context.Context, name string, want int) error {
			got, err := workspaceVersion(ctx, name)
			if err != nil {
				return err
			}
			if got != int32(want) {
				return fmt.Errorf("an upgrade moved the crew to version %d on its own, and a hook is pinned so it cannot change under a running session",
					got)
			}
			return nil
		})

	sc.Step(`^attaching it again moves the workspace to the version this build ships$`,
		func(ctx context.Context) error {
			want, err := shippedVersion("prompt-analyser")
			if err != nil {
				return err
			}
			w := worldFrom(ctx)
			if _, err := w.client.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
				Scope: "crew", Name: "prompt-analyser",
			}); err != nil {
				return err
			}
			got, err := workspaceVersion(ctx, "prompt-analyser")
			if err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("attaching again left the workspace on version %d, want %d", got, want)
			}
			return nil
		})
}

// shippedVersion is what version of a hook this build carries, read from the hooks directory the
// crew seeds from.
//
// Read rather than written into the scenario, because the behaviour being described is "newer than
// what the crew already held", not a particular number. Written down, every one of these scenarios
// fails the next time somebody edits the hook, which teaches the reader that the number is the point
// when it never was.
func shippedVersion(name string) (int32, error) {
	shipped, err := hook.Load("../hooks")
	if err != nil {
		return 0, fmt.Errorf("reading the hooks this build ships: %w", err)
	}
	for _, one := range shipped {
		if one.Name == name {
			if one.Version < 2 {
				// The scenario seeds version 1 by hand so the shipped one is genuinely newer. At
				// version 1 there is nothing to upgrade and the scenario would pass without proving it.
				return 0, fmt.Errorf("this build ships %s at version %d, so the upgrade scenarios prove nothing",
					name, one.Version)
			}
			return int32(one.Version), nil
		}
	}
	return 0, fmt.Errorf("this build ships no hook called %s", name)
}

// newestHeld is the newest version of a hook in the crew's catalogue.
func newestHeld(ctx context.Context, name string) (int32, error) {
	listed, err := worldFrom(ctx).client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
	if err != nil {
		return 0, err
	}
	for _, one := range listed.GetHooks() {
		if one.GetName() == name {
			return one.GetVersion(), nil
		}
	}
	return 0, fmt.Errorf("the crew does not hold %s at all", name)
}

// workspaceVersion is the version of a hook the workspace's sessions actually run under.
func workspaceVersion(ctx context.Context, name string) (int32, error) {
	w := worldFrom(ctx)
	listed, err := w.client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: w.workspaceID})
	if err != nil {
		return 0, err
	}
	for _, one := range listed.GetHooks() {
		if one.GetName() == name {
			return one.GetVersion(), nil
		}
	}
	return 0, fmt.Errorf("the workspace is not under %s", name)
}

// importAt is a hook on the wire at a given version, standing in for one an earlier build shipped.
func importAt(name string, version int) *quaycrewv1.ImportHookRequest {
	manifest := fmt.Sprintf("name: %s\nversion: %d\nsummary: An earlier build's version.\nevents:\n  - on: UserPromptSubmit\n    entry: bin/hook\n",
		name, version)
	return &quaycrewv1.ImportHookRequest{Files: []*quaycrewv1.HookFile{
		{Path: "hook.yaml", Body: []byte(manifest)},
		{Path: "bin/hook", Body: []byte("#!/bin/sh\nexit 0\n"), Executable: true},
	}}
}
