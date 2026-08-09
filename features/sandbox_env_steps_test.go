package features_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/cucumber/godog"
)

// The crew's own address, handed to a session so `quay` inside a sandbox needs no arguments. It is an
// address rather than a credential, and reaching it also needs a network that can, which is the same
// decision made once in configuration.
func initializeReachableSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a crew that sessions can reach at "([^"]*)"$`, func(ctx context.Context, at string) error {
		w := worldFrom(ctx)
		w.reachable = at
		return w.restart()
	})

	// Which of a workspace's secrets a session is given. Named here, set separately: the crew carries
	// a name only when there is a value behind it.
	sc.Step(`^a crew that gives its sessions the secret "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		w.sandboxSecrets = append(w.sandboxSecrets, name)
		return w.restart()
	})

	sc.Step(`^the workspace has the secret "([^"]*)" set to "([^"]*)"$`,
		func(ctx context.Context, name, value string) error {
			w := worldFrom(ctx)
			_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: w.workspaceID, Key: name, Value: value,
			})
			return err
		})

	sc.Step(`^the sandbox carries "([^"]*)" set to "([^"]*)"$`,
		func(ctx context.Context, name, value string) error {
			env, err := onlySandboxEnv(worldFrom(ctx))
			if err != nil {
				return err
			}
			if got := env[name]; got != value {
				return fmt.Errorf("the sandbox carries %s=%q, want %q", name, got, value)
			}
			return nil
		})

	sc.Step(`^the sandbox carries nothing called "([^"]*)"$`, func(ctx context.Context, name string) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		if got, set := env[name]; set {
			return fmt.Errorf("the sandbox carries %s=%q, and it was never named", name, got)
		}
		return nil
	})

	sc.Step(`^a crew whose commits are by "([^"]*)" at "([^"]*)"$`,
		func(ctx context.Context, name, email string) error {
			w := worldFrom(ctx)
			w.gitAuthor = controlplane.Identity{Name: name, Email: email}
			return w.restart()
		})

	sc.Step(`^a crew whose commits are by "([^"]*)" at no address$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		w.gitAuthor = controlplane.Identity{Name: name}
		return w.restart()
	})

	// All four, because git wants an author and a committer and refuses on either missing.
	sc.Step(`^the sandbox can commit as "([^"]*)" at "([^"]*)"$`,
		func(ctx context.Context, name, email string) error {
			env, err := onlySandboxEnv(worldFrom(ctx))
			if err != nil {
				return err
			}
			for key, want := range map[string]string{
				"GIT_AUTHOR_NAME": name, "GIT_AUTHOR_EMAIL": email,
				"GIT_COMMITTER_NAME": name, "GIT_COMMITTER_EMAIL": email,
			} {
				if got := env[key]; got != want {
					return fmt.Errorf("the sandbox carries %s=%q, want %q, and git refuses without all four",
						key, got, want)
				}
			}
			return nil
		})

	sc.Step(`^the sandbox carries no part of an identity$`, func(ctx context.Context) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		for key := range env {
			if strings.HasPrefix(key, "GIT_AUTHOR_") || strings.HasPrefix(key, "GIT_COMMITTER_") {
				return fmt.Errorf("the sandbox carries %s, and half an identity is refused the same as none", key)
			}
		}
		return nil
	})

	sc.Step(`^the sandbox carries the address of the crew$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		env, err := onlySandboxEnv(w)
		if err != nil {
			return err
		}
		if got := env["QC_GRPC_ADDR"]; got != w.reachable {
			return fmt.Errorf("the sandbox was told the crew is at %q, want %q", got, w.reachable)
		}
		return nil
	})

	sc.Step(`^the sandbox carries the driver's own token, not the operator's$`,
		func(ctx context.Context) error {
			w := worldFrom(ctx)
			env, err := onlySandboxEnv(w)
			if err != nil {
				return err
			}
			if got := env["QC_TOKEN"]; got != w.driverToken {
				return fmt.Errorf("the sandbox carries the token %q, want the driver's", got)
			}
			if env["QC_TOKEN"] == w.token {
				return fmt.Errorf("the sandbox carries the operator's token, and a driver must hold strictly less")
			}
			return nil
		})

	sc.Step(`^the sandbox carries no crew token$`, func(ctx context.Context) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		if got, set := env["QC_TOKEN"]; set {
			return fmt.Errorf("the sandbox carries the token %q, and an ordinary session gets none", got)
		}
		return nil
	})

	sc.Step(`^the sandbox carries no address at all$`, func(ctx context.Context) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		if got, set := env["QC_GRPC_ADDR"]; set {
			return fmt.Errorf("the sandbox was told the crew is at %q, and it was not meant to be reachable", got)
		}
		return nil
	})

	// Saying nothing would pass the check above too, so the session still has to have been given the
	// things it is meant to have.
	sc.Step(`^the sandbox carries no address it was not given$`, func(ctx context.Context) error {
		env, err := onlySandboxEnv(worldFrom(ctx))
		if err != nil {
			return err
		}
		for key, value := range env {
			if key == "QC_GRPC_ADDR" {
				continue
			}
			if strings.Contains(value, "://") || strings.Contains(value, ":50051") {
				return fmt.Errorf("the sandbox carries %s=%q, which is an address nobody asked for", key, value)
			}
		}
		return nil
	})
}

// onlySandboxEnv is the environment of the one sandbox the scenario made, as a map.
func onlySandboxEnv(w *world) (map[string]string, error) {
	if len(w.provider.Created) != 1 {
		return nil, fmt.Errorf("%d sandboxes were made, want exactly 1", len(w.provider.Created))
	}
	env := make(map[string]string, len(w.provider.Created[0].Env))
	for _, entry := range w.provider.Created[0].Env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		env[key] = value
	}
	return env, nil
}

// The driver: the one session that drives the crew rather than doing work inside it.
func initializeDriverSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator opens the driver$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		opened, err := w.client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		w.drivers = append(w.drivers, opened.GetThread())
		return nil
	})

	sc.Step(`^the operator opens the driver again$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		opened, err := w.client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		w.drivers = append(w.drivers, opened.GetThread())
		return nil
	})

	sc.Step(`^the driver is sent "([^"]*)"$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if len(w.drivers) == 0 {
			return fmt.Errorf("no driver was opened")
		}
		return w.dispatch(ctx, w.projectID, w.drivers[0].GetHandle(), text)
	})

	sc.Step(`^the driver is set to permission mode "([^"]*)"$`, func(ctx context.Context, mode string) error {
		w := worldFrom(ctx)
		if len(w.drivers) == 0 {
			return fmt.Errorf("no driver was opened")
		}
		_, err := w.client.SetThreadPermissionMode(ctx,
			&quaycrewv1.SetThreadPermissionModeRequest{Id: w.drivers[0].GetId(), Mode: mode})
		return err
	})

	sc.Step(`^it is the same driver both times$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.drivers) != 2 {
			return fmt.Errorf("the driver was opened %d times, want 2", len(w.drivers))
		}
		if w.drivers[0].GetId() != w.drivers[1].GetId() {
			return fmt.Errorf("opening the crew twice gave two drivers, %s and %s",
				w.drivers[0].GetId(), w.drivers[1].GetId())
		}
		if !w.drivers[0].GetDriver() {
			return fmt.Errorf("the session opened is not marked as the driver")
		}
		return nil
	})

	// One per project. Two would each think they were the one, and the second would be reached by
	// nobody.
	sc.Step(`^the crew has one driver$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		drivers := 0
		for _, session := range listed.GetThreads() {
			if session.GetDriver() {
				drivers++
			}
		}
		if drivers != 1 {
			return fmt.Errorf("the project has %d drivers, want 1", drivers)
		}
		return nil
	})
}

// What the driver has been told, which is the crew describing itself.
func initializeDriverContextSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the driver has been told what quay is$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.drivers) == 0 {
			return fmt.Errorf("no driver was opened")
		}
		told, err := w.store.GetContext(ctx, store.ContextSession, w.drivers[0].GetId())
		if err != nil {
			return fmt.Errorf("read what the driver was told: %w", err)
		}
		if !strings.Contains(told, "quay context set") {
			return fmt.Errorf("the driver was not told how anything gets told anything:\n%s", told)
		}
		return nil
	})

	sc.Step(`^what it was told names the words a crew is made of$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		told, err := w.store.GetContext(ctx, store.ContextSession, w.drivers[0].GetId())
		if err != nil {
			return err
		}
		for _, word := range []string{"workspace", "project", "thread", "session", "sandbox"} {
			if !strings.Contains(told, word) {
				return fmt.Errorf("the driver was never told what a %s is", word)
			}
		}
		return nil
	})

	// A driver from before the crew described itself: it exists, and nothing has ever been written
	// into its context.
	sc.Step(`^a driver made before the crew described itself$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		opened, err := w.client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		w.drivers = append(w.drivers, opened.GetThread())
		return w.store.SetContext(ctx, store.ContextSession, opened.GetThread().GetId(), "")
	})

	sc.Step(`^its memory file already says "([^"]*)"$`, func(ctx context.Context, body string) error {
		return sandbox.WriteMemory(driverWorkingDir(ctx), body)
	})

	sc.Step(`^the driver's memory file says what quay is$`, func(ctx context.Context) error {
		body, found := sandbox.ReadMemory(driverWorkingDir(ctx))
		if !found {
			return fmt.Errorf("the driver has no memory file at all")
		}
		if !strings.Contains(body, "quay context set") {
			return fmt.Errorf("the driver's memory file does not say what quay is:\n%s", body)
		}
		return nil
	})

	sc.Step(`^the driver's memory file still says "([^"]*)"$`, func(ctx context.Context, want string) error {
		body, found := sandbox.ReadMemory(driverWorkingDir(ctx))
		if !found {
			return fmt.Errorf("the driver has no memory file at all")
		}
		if !strings.Contains(body, want) {
			return fmt.Errorf("the driver's memory file no longer says %q:\n%s", want, body)
		}
		return nil
	})

	sc.Step(`^the operator writes their own instructions into the driver$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		return w.store.SetContext(ctx, store.ContextSession, w.drivers[0].GetId(), "my own instructions")
	})

	sc.Step(`^the driver still carries their own instructions$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		told, err := w.store.GetContext(ctx, store.ContextSession, w.drivers[len(w.drivers)-1].GetId())
		if err != nil {
			return err
		}
		if told != "my own instructions" {
			return fmt.Errorf("opening the driver again overwrote what the operator wrote:\n%s", told)
		}
		return nil
	})
}

// driverWorkingDir is the driver's own working directory on disk, which is where the file it reads
// as its memory lives.
func driverWorkingDir(ctx context.Context) string {
	w := worldFrom(ctx)
	if len(w.drivers) == 0 {
		return ""
	}
	return filepath.Join(w.storage.Dir, "workspaces", w.workspaceID,
		"projects", w.projectID, "sessions", w.drivers[0].GetId(), "workspace")
}
