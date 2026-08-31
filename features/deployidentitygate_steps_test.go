package features_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/cucumber/godog"
)

// The deploy identity gate, run the way the model runtime runs it: the entry point the system would
// mount, fed a PreToolUse payload on standard input, answering with an exit code.
//
// It runs the real binary rather than calling the hook's own code, because the hook is a separate
// module and the system cannot import it. What is proved here is the file a sandbox mounts.
//
// The change is a real git repository rather than a list of file names, because reading the change is
// half of what the gate does and a double would prove the other half twice.

func initializeDeployIdentityGateSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a change that creates infrastructure$`, func(ctx context.Context) error {
		return aChange(ctx, "infra/main.tf", `resource "aws_s3_bucket" "site" {}`)
	})

	sc.Step(`^a change that creates nothing in the cloud$`, func(ctx context.Context) error {
		return aChange(ctx, "docs/README.md", "the transcript service")
	})

	sc.Step(`^a session is about to open a pull request saying "([^"]*)"$`,
		func(ctx context.Context, body string) error {
			payload, err := json.Marshal(map[string]any{
				"tool_name": "Bash",
				"cwd":       worldFrom(ctx).change,
				"tool_input": map[string]string{
					"command": `gh pr create --title "the transcript service" --body '` + body + `'`,
				},
			})
			if err != nil {
				return err
			}
			return fireDeployIdentityGate(ctx, string(payload))
		})

	sc.Step(`^a session sends the deploy identity gate a payload it cannot read$`, func(ctx context.Context) error {
		return fireDeployIdentityGate(ctx, "this is not the payload a runtime sends")
	})

	sc.Step(`^the deploy identity gate refuses it$`, func(ctx context.Context) error {
		if !worldFrom(ctx).deployGate.refused {
			return errors.New("the gate let it through, and nothing downstream reads the account")
		}
		return nil
	})

	sc.Step(`^the deploy identity gate allows it$`, func(ctx context.Context) error {
		answer := worldFrom(ctx).deployGate
		if answer.refused {
			return fmt.Errorf("the gate refused work a role does on every slice: %s", answer.said)
		}
		return nil
	})

	sc.Step(`^the refusal names the infrastructure it read$`, func(ctx context.Context) error {
		said := worldFrom(ctx).deployGate.said
		if !strings.Contains(said, "main.tf") {
			return fmt.Errorf("the refusal does not say which files it read, so nobody can check it was right:\n%s", said)
		}
		return nil
	})

	sc.Step(`^the refusal says which question to ask and how to ask it$`, func(ctx context.Context) error {
		said := worldFrom(ctx).deployGate.said
		for _, needed := range []string{"iam:SimulatePrincipalPolicy", "simulate-principal-policy", "in one call", "krewe target"} {
			if !strings.Contains(said, needed) {
				return fmt.Errorf("the refusal never says %q, so the session is left guessing:\n%s", needed, said)
			}
		}
		return nil
	})

	sc.Step(`^the refusal says the work is not ready$`, func(ctx context.Context) error {
		said := worldFrom(ctx).deployGate.said
		if !strings.Contains(said, "stops the work being ready") {
			return fmt.Errorf("the refusal does not stop the work, which is the whole rule:\n%s", said)
		}
		return nil
	})

	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if w := worldFrom(ctx); w != nil && w.change != "" {
			_ = os.RemoveAll(w.change)
			w.change = ""
		}
		return ctx, err
	})
}

// aChange is a repository with one file committed on a branch off its default branch, which is what a
// session has in front of it when it opens a pull request.
func aChange(ctx context.Context, name, body string) error {
	dir, err := os.MkdirTemp("", "change")
	if err != nil {
		return err
	}
	steps := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "gate@example.com"},
		{"config", "user.name", "the gate"},
		// Signing is the operator's, and a repository this scenario made has no key.
		{"config", "commit.gpgsign", "false"},
	}
	for _, step := range steps {
		if err := inRepository(ctx, dir, step...); err != nil {
			return err
		}
	}
	if err := write(dir, "start.md", "start"); err != nil {
		return err
	}
	if err := commit(ctx, dir, "chore: start"); err != nil {
		return err
	}
	if err := inRepository(ctx, dir, "checkout", "-b", "519-feat-the-service"); err != nil {
		return err
	}
	if err := write(dir, name, body); err != nil {
		return err
	}
	if err := commit(ctx, dir, "feat: the service"); err != nil {
		return err
	}
	worldFrom(ctx).change = dir
	return nil
}

func write(dir, name, body string) error {
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

func commit(ctx context.Context, dir, message string) error {
	if err := inRepository(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	return inRepository(ctx, dir, "commit", "-m", message)
}

func inRepository(ctx context.Context, dir string, args ...string) error {
	run := exec.CommandContext(ctx, "git", args...)
	run.Dir = dir
	if out, err := run.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

// fireDeployIdentityGate runs the shipped entry point over one payload and records what it said.
func fireDeployIdentityGate(ctx context.Context, payload string) error {
	entry, err := deployIdentityGateEntry()
	if err != nil {
		return err
	}
	run := exec.CommandContext(ctx, entry)
	run.Stdin = strings.NewReader(payload)
	var said strings.Builder
	run.Stderr = &said

	answer := gateAnswer{}
	switch err := run.Run(); {
	case err == nil:
	case isExit(err, 2):
		answer.refused = true
	default:
		return fmt.Errorf("running %s: %w\n%s", entry, err, said.String())
	}
	answer.said = said.String()
	worldFrom(ctx).deployGate = answer
	return nil
}

// deployIdentityGateEntry is the file the system would mount, found through the loader the control
// plane uses, so a manifest that renamed its entry point cannot leave this pointing at the old path.
func deployIdentityGateEntry() (string, error) {
	hooks, err := hook.Load("../hooks")
	if err != nil {
		return "", fmt.Errorf("loading the hooks this build ships (run `make hooks` first): %w", err)
	}
	for _, one := range hooks {
		if one.Name != "deploy-identity-gate" {
			continue
		}
		if len(one.Events) == 0 {
			return "", errors.New("the deploy identity gate is bound to nothing, so the runtime would never call it")
		}
		return filepath.Join("../hooks", one.Name, one.Events[0].Entry), nil
	}
	return "", errors.New("this build ships no deploy identity gate, so these scenarios prove nothing")
}
