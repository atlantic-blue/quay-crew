package features_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/cucumber/godog"
)

// What a session is actually built with. Both halves have to be true: the files mounted, and the
// settings binding them. Either one alone is a hook that does nothing.
func initializeHookSandboxSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the session's sandbox carries the hooks directory$`, func(ctx context.Context) error {
		_, err := hooksMount(ctx)
		return err
	})

	sc.Step(`^the hooks directory is mounted read only$`, func(ctx context.Context) error {
		mount, err := hooksMount(ctx)
		if err != nil {
			return err
		}
		// A session that can edit the file binding its own constraints is a session with no
		// constraints.
		if !mount.ReadOnly {
			return fmt.Errorf("the hooks are mounted read write, so a session can rewrite what binds it")
		}
		return nil
	})

	sc.Step(`^the session's sandbox carries no hooks directory$`, func(ctx context.Context) error {
		if _, err := hooksMount(ctx); err == nil {
			return fmt.Errorf("a session under no hooks was given a hooks directory anyway")
		}
		return nil
	})

	sc.Step(`^the settings file binds "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, name, event string) error {
			w := worldFrom(ctx)
			path := filepath.Join(w.storage.Dir, "workspaces", w.workspaceID,
				sandbox.HooksDir, hook.SettingsFile)
			body, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("no settings file was written: %w", err)
			}
			var document struct {
				Hooks map[string][]struct {
					Hooks []struct {
						Command string `json:"command"`
					} `json:"hooks"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(body, &document); err != nil {
				return fmt.Errorf("the settings file is not valid json: %w", err)
			}
			for _, group := range document.Hooks[event] {
				for _, run := range group.Hooks {
					if strings.Contains(run.Command, "/"+name+"/") {
						return nil
					}
				}
			}
			return fmt.Errorf("nothing binds %s to %s:\n%s", name, event, body)
		})

	sc.Step(`^the turn loaded the hooks settings$`, func(ctx context.Context) error {
		last := worldFrom(ctx).runner.lastRequest()
		want := sandbox.HooksPath + "/" + hook.SettingsFile
		if last.Settings != want {
			return fmt.Errorf("the turn was told to load %q, want %q", last.Settings, want)
		}
		return nil
	})

	sc.Step(`^the turn was not told to load any settings$`, func(ctx context.Context) error {
		// Pointing the runtime at a file that is not there fails the turn before it starts.
		if last := worldFrom(ctx).runner.lastRequest(); last.Settings != "" {
			return fmt.Errorf("a session under no hooks was told to load %q", last.Settings)
		}
		return nil
	})
}

// hooksMount is the mount carrying the hooks into the session's sandbox, or an error saying there is
// none.
func hooksMount(ctx context.Context) (sandbox.Mount, error) {
	w := worldFrom(ctx)
	if len(w.provider.Created) == 0 {
		return sandbox.Mount{}, fmt.Errorf("no sandbox was created")
	}
	for _, mount := range w.provider.Created[len(w.provider.Created)-1].Mounts {
		if mount.Target == sandbox.HooksPath {
			return mount, nil
		}
	}
	return sandbox.Mount{}, fmt.Errorf("the sandbox has no mount at %s", sandbox.HooksPath)
}
