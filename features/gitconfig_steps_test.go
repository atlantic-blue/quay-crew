package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
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

	// A gpg key is not usable where it lands, which is the difference between the two formats: git
	// reads an ssh key as a file and reads a gpg key out of a keyring, so a sandbox that never
	// imports the mounted key is pointed at nothing.
	sc.Step(`^the sandbox imports the mounted gpg key$`, func(ctx context.Context) error {
		return sandboxRan(ctx, "gpg --quiet --import "+sandbox.SecretFilePath(controlplane.OpenPGPKeySecret))
	})

	sc.Step(`^the sandbox tells gpg to work in batch$`, func(ctx context.Context) error {
		if err := sandboxRan(ctx, "batch"); err != nil {
			return err
		}
		return sandboxRan(ctx, "no-tty")
	})

	sc.Step(`^the sandbox points gpg at the passphrase file "([^"]*)"$`,
		func(ctx context.Context, at string) error {
			if err := sandboxRan(ctx, "passphrase-file "+at); err != nil {
				return err
			}
			return sandboxRan(ctx, "pinentry-mode loopback")
		})

	sc.Step(`^the sandbox does not point gpg at a passphrase file$`, func(ctx context.Context) error {
		if err := sandboxRan(ctx, "passphrase-file"); err == nil {
			return fmt.Errorf("a workspace that mounted no passphrase was pointed at one anyway:\n%s", scripts(ctx))
		}
		return nil
	})
}

// sandboxRan answers whether the system asked the session's sandbox to do something, by reading the
// commands it was actually given. What the system meant to run and never sent cannot pass.
func sandboxRan(ctx context.Context, want string) error {
	did := scripts(ctx)
	if !strings.Contains(did, want) {
		return fmt.Errorf("the sandbox was never told %q. It ran:\n%s", want, did)
	}
	return nil
}

// scripts is every shell script the system ran in the session's sandbox, in order.
func scripts(ctx context.Context) string {
	var all []string
	for _, box := range worldFrom(ctx).provider.Boxes {
		for _, spec := range box.Ran {
			all = append(all, strings.Join(spec.Argv, " "))
		}
	}
	return strings.Join(all, "\n")
}

// gitConfigured reads back every global git setting the system asked the session's sandbox to write,
// keyed by name. It reads the commands the sandbox was actually given, so a setting the system meant
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
