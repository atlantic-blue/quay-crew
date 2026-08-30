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

	sc.Step(`^the settings file binds nothing to any event$`, func(ctx context.Context) error {
		body, err := renderedSettings(ctx)
		if err != nil {
			return err
		}
		var document struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if err := json.Unmarshal(body, &document); err != nil {
			return fmt.Errorf("the settings file is not valid json: %w", err)
		}
		if len(document.Hooks) != 0 {
			return fmt.Errorf("a session under no hooks was bound to something anyway:\n%s", body)
		}
		return nil
	})

	sc.Step(`^the settings file binds "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, name, event string) error {
			body, err := renderedSettings(ctx)
			if err != nil {
				return err
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

	sc.Step(`^the task loaded the hooks settings$`, func(ctx context.Context) error {
		last := worldFrom(ctx).runner.lastRequest()
		want := sandbox.HooksPath + "/" + hook.SettingsFile
		if last.Settings != want {
			return fmt.Errorf("the task was told to load %q, want %q", last.Settings, want)
		}
		return nil
	})

	// The line an attached operator reads, said by the same file that binds the hooks.
	sc.Step(`^the settings tell the runtime to draw its status line by running quay$`,
		func(ctx context.Context) error {
			body, err := renderedSettings(ctx)
			if err != nil {
				return err
			}
			var document struct {
				StatusLine struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"statusLine"`
			}
			if err := json.Unmarshal(body, &document); err != nil {
				return fmt.Errorf("the settings file is not valid json: %w", err)
			}
			if document.StatusLine.Type != "command" {
				return fmt.Errorf("the system asks for a status line of type %q, and the runtime only runs a command",
					document.StatusLine.Type)
			}
			if words := strings.Fields(document.StatusLine.Command); len(words) < 2 || words[0] != "quay" {
				return fmt.Errorf("the status line runs %q, which is not this tool", document.StatusLine.Command)
			}
			return nil
		})
}

// renderedSettings is the settings file the system wrote for this session's workspace.
func renderedSettings(ctx context.Context) ([]byte, error) {
	w := worldFrom(ctx)
	at := filepath.Join(w.storage.Dir, "workspaces", w.workspaceID, sandbox.HooksDir, hook.SettingsFile)
	body, err := os.ReadFile(at) //nolint:gosec // a path this test built, under its own temporary directory
	if err != nil {
		return nil, fmt.Errorf("no settings file was written: %w", err)
	}
	return body, nil
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
