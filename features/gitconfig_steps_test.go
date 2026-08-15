package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/cucumber/godog"
)

// Where a session's git identity comes from, and who decides whether it signs.
func initializeGitConfigSteps(sc *godog.ScenarioContext) {
	// Read from the file the image ships rather than from a constant, because the include is what
	// joins the two halves: a mounted secret that lands somewhere git is not reading is a secret
	// that does nothing, and both halves would still pass their own tests.
	sc.Step(`^the image reads its git configuration from that file$`, func(ctx context.Context) error {
		shipped, err := os.ReadFile(filepath.Join("..", "deploy", "sandbox", "gitconfig"))
		if err != nil {
			return fmt.Errorf("read the configuration the image ships: %w", err)
		}
		at := sandbox.SecretFilePath(sandbox.GitConfigSecret)
		for _, line := range strings.Split(string(shipped), "\n") {
			name, value, found := strings.Cut(line, "=")
			if !found || strings.TrimSpace(name) != "path" {
				continue
			}
			if strings.TrimSpace(value) == at {
				return nil
			}
		}
		return fmt.Errorf("the image's git configuration includes nothing at %s:\n%s", at, shipped)
	})

	sc.Step(`^the sandbox is told to set "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, name, want string) error {
			settings, err := gitConfigured(worldFrom(ctx))
			if err != nil {
				return err
			}
			got, set := settings[name]
			if !set {
				return fmt.Errorf("the sandbox was never told about %s, only %v", name, settingNames(settings))
			}
			if got != want {
				return fmt.Errorf("the sandbox was told %s is %q, want %q", name, got, want)
			}
			return nil
		})
}

// gitConfigured reads back every global git setting the crew asked the session's sandbox to write,
// keyed by name. It reads the commands the sandbox was actually given, so a setting the crew meant
// to write and never sent cannot pass.
//
// Only --global is counted. A setting written anywhere else does not sit in the file the operator's
// own configuration is included from, so it would not beat what that configuration asks for, which
// is the whole reason these are written at all.
func gitConfigured(w *world) (map[string]string, error) {
	if len(w.provider.Boxes) != 1 {
		return nil, fmt.Errorf("%d sandboxes were made, want exactly 1", len(w.provider.Boxes))
	}
	settings := map[string]string{}
	for _, spec := range w.provider.Boxes[0].Ran {
		if len(spec.Argv) != 3 || spec.Argv[0] != "sh" || spec.Argv[1] != "-c" {
			continue
		}
		for _, line := range strings.Split(spec.Argv[2], "\n") {
			words := strings.Fields(line)
			if len(words) != 5 || words[0] != "git" || words[1] != "config" || words[2] != "--global" {
				continue
			}
			settings[words[3]] = words[4]
		}
	}
	return settings, nil
}

func settingNames(settings map[string]string) []string {
	out := make([]string, 0, len(settings))
	for name := range settings {
		out = append(out, name)
	}
	return out
}
