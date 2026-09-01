package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// A secret that reaches a session as a file rather than as an environment variable.
func initializeSecretFileSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the workspace mounts the secret "([^"]*)" holding "([^"]*)"$`,
		func(ctx context.Context, name, contents string) error {
			w := worldFrom(ctx)
			_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace:  w.workspaceID,
				Key:        name,
				Value:      contents,
				Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
			})
			return err
		})

	// Kept separate from the step above because this one is about being refused, so it must not fail
	// the scenario when the call comes back with an error.
	sc.Step(`^the operator mounts the secret "([^"]*)" holding "([^"]*)"$`,
		func(ctx context.Context, name, contents string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace:  w.workspaceID,
				Key:        name,
				Value:      contents,
				Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
			})
			return nil
		})

	// The way onto the environment, kept separate for the same reason: this one is about being
	// refused, so it must not fail the scenario when the call comes back with an error.
	sc.Step(`^the operator tries to set the secret "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, name, value string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: w.workspaceID,
				Key:       name,
				Value:     value,
			})
			return nil
		})

	sc.Step(`^the system refuses it, saying to mount the key instead$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the system accepted it, and the key would be in every container's environment")
		}
		// The command, not just the objection. A refusal the operator cannot act on is a dead end.
		if !strings.Contains(w.lastErr.Error(), "krewe secret mount") {
			return fmt.Errorf("the system refused it saying %q, which does not say what to type", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the system refuses it, saying it cannot be a file name$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the system accepted it, and the name would have become a path")
		}
		if !strings.Contains(w.lastErr.Error(), "file name") {
			return fmt.Errorf("the system refused it saying %q, which does not say why", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the sandbox is given the file "([^"]*)" holding "([^"]*)"$`,
		func(ctx context.Context, at, contents string) error {
			written, err := filesWritten(worldFrom(ctx))
			if err != nil {
				return err
			}
			got, given := written[at]
			if !given {
				return fmt.Errorf("the sandbox was given no file at %s, only %v", at, names(written))
			}
			if got != contents {
				return fmt.Errorf("the file at %s holds %q, want %q", at, got, contents)
			}
			return nil
		})

	sc.Step(`^the sandbox is given no files$`, func(ctx context.Context) error {
		written, err := filesWritten(worldFrom(ctx))
		if err != nil {
			return err
		}
		if len(written) != 0 {
			return fmt.Errorf("the sandbox was given %v, and this workspace mounted nothing", names(written))
		}
		return nil
	})

	sc.Step(`^no command run in the sandbox carries "([^"]*)" in its arguments$`,
		func(ctx context.Context, value string) error {
			w := worldFrom(ctx)
			if len(w.provider.Boxes) == 0 {
				return fmt.Errorf("no sandbox was made, so nothing ran in one")
			}
			for _, box := range w.provider.Boxes {
				for _, spec := range box.Ran {
					for _, arg := range spec.Argv {
						if strings.Contains(arg, value) {
							return fmt.Errorf("a command was run with the value in an argument: %v", spec.Argv)
						}
					}
				}
			}
			return nil
		})

	sc.Step(`^the listing says "([^"]*)" is mounted$`, func(ctx context.Context, name string) error {
		return listedAs(ctx, name, quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE)
	})

	sc.Step(`^the listing says "([^"]*)" reaches the environment$`, func(ctx context.Context, name string) error {
		return listedAs(ctx, name, quaycrewv1.SecretProjection_SECRET_PROJECTION_ENV)
	})

	sc.Step(`^the listing says nothing that either secret holds$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		said := w.lastSecrets.String()
		for _, held := range []string{"ghp-1234", "[user] name = operator"} {
			if strings.Contains(said, held) {
				return fmt.Errorf("the listing carries a value: %s", said)
			}
		}
		return nil
	})
}

// listedAs says the listing carries this secret and says it reaches a session this way.
func listedAs(ctx context.Context, name string, want quaycrewv1.SecretProjection) error {
	w := worldFrom(ctx)
	for _, secret := range w.lastSecrets.GetSecrets() {
		if secret.GetName() != name {
			continue
		}
		if secret.GetProjection() != want {
			return fmt.Errorf("the listing says %s reaches a session as %s, want %s",
				name, secret.GetProjection(), want)
		}
		return nil
	}
	return fmt.Errorf("the listing does not name %q at all", name)
}

// filesWritten reads back what the system asked the session's sandbox to write, keyed by the path it
// wrote to. It reads the commands the sandbox was actually given rather than a recording the system
// keeps of its own intent, so a write that never reached the sandbox cannot pass.
func filesWritten(w *world) (map[string]string, error) {
	if len(w.provider.Boxes) != 1 {
		return nil, fmt.Errorf("%d sandboxes were made, want exactly 1", len(w.provider.Boxes))
	}
	written := map[string]string{}
	for _, spec := range w.provider.Boxes[0].Ran {
		at, found := wroteTo(spec.Argv)
		if !found {
			continue
		}
		value, held := valueIn(spec.Env)
		if !held {
			return nil, fmt.Errorf("a file was written to %s with no value to write", at)
		}
		written[at] = value
	}
	return written, nil
}

// wroteTo reads the path out of a script that writes a file. The write is the script's last line and
// it redirects, so the path is what follows the redirection there. A script whose last line
// redirects nowhere wrote no file.
func wroteTo(argv []string) (string, bool) {
	if len(argv) != 3 || argv[0] != "sh" || argv[1] != "-c" {
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(argv[2]), "\n")
	_, after, found := strings.Cut(lines[len(lines)-1], "> ")
	if !found {
		return "", false
	}
	return strings.TrimSpace(after), true
}

// valueIn reads what the write was given to write. The system hands it through the environment of that
// one command rather than as an argument, so this is where it is.
func valueIn(env []string) (string, bool) {
	for _, entry := range env {
		if name, value, found := strings.Cut(entry, "="); found && name == "QC_SECRET_FILE_VALUE" {
			return value, true
		}
	}
	return "", false
}

func names(written map[string]string) []string {
	out := make([]string, 0, len(written))
	for at := range written {
		out = append(out, at)
	}
	return out
}
