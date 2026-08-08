package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/cucumber/godog"
)

// The skills a crew has, written as files the way an operator writes them and read by the control
// plane the way it reads them at startup.
func initializeSkillSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the crew has a skill "([^"]*)" that says "([^"]*)"$`,
		func(ctx context.Context, name, brief string) error {
			return giveTheCrewASkill(ctx, name, brief, "", "")
		})

	sc.Step(`^the crew has a skill "([^"]*)" needing the secret "([^"]*)"$`,
		func(ctx context.Context, name, secret string) error {
			return giveTheCrewASkill(ctx, name, "Do the thing.", secret, "")
		})

	sc.Step(`^the crew has a skill "([^"]*)" needing the binary "([^"]*)"$`,
		func(ctx context.Context, name, binary string) error {
			return giveTheCrewASkill(ctx, name, "Do the thing.", "", binary)
		})

	// Everything beside the brief, which the model opens when it needs it and pays nothing for until
	// then.
	sc.Step(`^the ([^ ]+) skill has a file "([^"]*)" saying "([^"]*)"$`,
		func(ctx context.Context, name, file, body string) error {
			w := worldFrom(ctx)
			if err := os.WriteFile(filepath.Join(w.skillsDir, name, file), []byte(body+"\n"), 0o666); err != nil {
				return err
			}
			return reloadSkills(ctx)
		})

	sc.Step(`^the sandbox image does not carry "([^"]*)"$`, func(ctx context.Context, binary string) error {
		w := worldFrom(ctx)
		w.provider.Missing = append(w.provider.Missing, binary)
		return nil
	})

	sc.Step(`^the session's memory file mentions no skill$`, func(ctx context.Context) error {
		body, err := sessionMemory(ctx)
		if err != nil {
			// No memory file at all is no skill mentioned, which is the point.
			return nil
		}
		if strings.Contains(body, "quay:skill:") {
			return fmt.Errorf("a crew with no skills wrote one into the file anyway:\n%s", body)
		}
		return nil
	})

	sc.Step(`^the session's memory file says where the rest of the ([^ ]+) skill is$`,
		func(ctx context.Context, name string) error {
			body, err := sessionMemory(ctx)
			if err != nil {
				return err
			}
			where := filepath.Join(sandbox.SkillsPath, name)
			if !strings.Contains(body, where) {
				return fmt.Errorf("the brief never says the rest is in %s, so nothing will go and read it:\n%s",
					where, body)
			}
			return nil
		})

	sc.Step(`^the session's memory file does not carry "([^"]*)"$`, func(ctx context.Context, absent string) error {
		body, err := sessionMemory(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(body, absent) {
			return fmt.Errorf("the memory file carries %q, which was meant to stay on disk until it is needed:\n%s",
				absent, body)
		}
		return nil
	})

	// Read only, because a session that can rewrite its own instructions can give itself a capability
	// nobody approved.
	sc.Step(`^the sandbox mounts the ([^ ]+) skill read only$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		if len(w.provider.Created) != 1 {
			return fmt.Errorf("%d sandboxes were made, want exactly 1", len(w.provider.Created))
		}
		want := filepath.Join(sandbox.SkillsPath, name)
		for _, mount := range w.provider.Created[0].Mounts {
			if mount.Target != want {
				continue
			}
			if !mount.ReadOnly {
				return fmt.Errorf("the %s skill is mounted writable at %s", name, mount.Target)
			}
			if mount.Source != filepath.Join(w.skillsDir, name) {
				return fmt.Errorf("the %s skill is mounted from %s", name, mount.Source)
			}
			return nil
		}
		return fmt.Errorf("the sandbox has no %s skill mounted at all", name)
	})

	sc.Step(`^the refusal names the secret and how to set it$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the turn was allowed to run")
		}
		for _, want := range []string{"GH_TOKEN", "quay secret set"} {
			if !strings.Contains(w.lastErr.Error(), want) {
				return fmt.Errorf("the refusal is %q, want it to say %q", w.lastErr, want)
			}
		}
		return nil
	})

	// Which image to go and fix is most of the message: without it the operator knows only that
	// something somewhere is missing a command.
	sc.Step(`^the refusal names the binary and the image to add it to$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the turn was allowed to run")
		}
		for _, want := range []string{"gh", "quaycrew-sandbox:test"} {
			if !strings.Contains(w.lastErr.Error(), want) {
				return fmt.Errorf("the refusal is %q, want it to name %q", w.lastErr, want)
			}
		}
		return nil
	})

	// The store, not the file: a brief that reached the store is a brief the crew now thinks the
	// session wrote, and it will be rendered beside the real one on every turn from here.
	sc.Step(`^the session's own context says nothing about the ([^ ]+) skill$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			current, err := w.lastTurn()
			if err != nil {
				return err
			}
			kept, err := w.store.GetContext(ctx, store.ContextSession, current.sessionID)
			if err != nil {
				return nil
			}
			if strings.TrimSpace(kept) != "" {
				return fmt.Errorf("the %s skill's brief was taken into the session's own context, "+
					"so the crew will render it beside itself from now on:\n%s", name, kept)
			}
			return nil
		})

	sc.Step(`^the session's memory file carries "([^"]*)" exactly once$`,
		func(ctx context.Context, want string) error {
			body, err := sessionMemory(ctx)
			if err != nil {
				return err
			}
			if count := strings.Count(body, want); count != 1 {
				return fmt.Errorf("the memory file says %q %d times, want once:\n%s", want, count, body)
			}
			return nil
		})

	sc.Step(`^no sandbox has been created$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if made := len(w.provider.Created); made != 0 {
			return fmt.Errorf("%d sandboxes were made, and the refusal was meant to come first", made)
		}
		return nil
	})
}

// giveTheCrewASkill writes one to disk and restarts the control plane over it, which is how a crew
// picks up a skill: they are read when it starts.
func giveTheCrewASkill(ctx context.Context, name, brief, secret, binary string) error {
	w := worldFrom(ctx)
	at := filepath.Join(w.skillsDir, name)
	if err := os.MkdirAll(at, 0o777); err != nil {
		return err
	}

	manifest := "name: " + name + "\nversion: 1\nsummary: a skill\n"
	if binary != "" {
		manifest += "binaries: [" + binary + "]\n"
	}
	if secret != "" {
		manifest += "secrets:\n  " + secret + ": what it is for\n"
	}
	if err := os.WriteFile(filepath.Join(at, skill.ManifestFile), []byte(manifest), 0o666); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(at, skill.BriefFile), []byte(brief+"\n"), 0o666); err != nil {
		return err
	}
	return reloadSkills(ctx)
}

// reloadSkills reads the directory again and restarts the crew over it.
func reloadSkills(ctx context.Context) error {
	w := worldFrom(ctx)
	read, err := skill.Load(w.skillsDir)
	if err != nil {
		return err
	}
	w.skills = read
	return w.restart()
}
