package features_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/cucumber/godog"
)

// The merge gate, run the way the model runtime runs it: the entry point the system would mount, fed a
// PreToolUse payload on standard input, answering with an exit code.
//
// It runs the real binary rather than calling the hook's own code, because the hook is a separate
// module and the system cannot import it. That is the point of the shape: what is proved here is the
// file a sandbox mounts, so a gate whose entry point was never built fails this rather than passing
// over nothing.

// gateAnswer is what one firing of the gate came back with.
type gateAnswer struct {
	refused bool
	said    string
}

func initializeMergeGateSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a session is about to run: (.+)$`, func(ctx context.Context, command string) error {
		payload, err := json.Marshal(map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]string{"command": command},
		})
		if err != nil {
			return err
		}
		return fireMergeGate(ctx, string(payload))
	})

	sc.Step(`^a session sends the merge gate a payload it cannot read$`, func(ctx context.Context) error {
		return fireMergeGate(ctx, "this is not the payload a runtime sends")
	})

	sc.Step(`^the merge gate refuses it$`, func(ctx context.Context) error {
		answer := worldFrom(ctx).mergeGate
		if !answer.refused {
			return fmt.Errorf("the gate let it through, and it merges")
		}
		return nil
	})

	sc.Step(`^the merge gate allows it$`, func(ctx context.Context) error {
		answer := worldFrom(ctx).mergeGate
		if answer.refused {
			return fmt.Errorf("the gate refused it, and it is work a role does on every slice: %s", answer.said)
		}
		return nil
	})

	sc.Step(`^the refusal says to open a pull request and leave the merge to the operator$`,
		func(ctx context.Context) error {
			said := worldFrom(ctx).mergeGate.said
			for _, needed := range []string{"open a pull request", "the operator's"} {
				if !strings.Contains(said, needed) {
					return fmt.Errorf("the refusal does not say %q, so the session is left guessing:\n%s",
						needed, said)
				}
			}
			return nil
		})

	sc.Step(`^the workspace runs under the "([^"]*)" hook$`, func(ctx context.Context, name string) error {
		return workspaceHook(ctx, name, true)
	})

	sc.Step(`^the workspace runs under no "([^"]*)" hook$`, func(ctx context.Context, name string) error {
		return workspaceHook(ctx, name, false)
	})
}

// fireMergeGate runs the shipped entry point over one payload and records what it said.
func fireMergeGate(ctx context.Context, payload string) error {
	entry, err := mergeGateEntry()
	if err != nil {
		return err
	}
	run := exec.CommandContext(ctx, entry)
	run.Stdin = strings.NewReader(payload)
	var said strings.Builder
	run.Stderr = &said

	answer := gateAnswer{said: said.String()}
	switch err := run.Run(); {
	case err == nil:
	case isExit(err, 2):
		answer.refused = true
	default:
		return fmt.Errorf("running %s: %w\n%s", entry, err, said.String())
	}
	answer.said = said.String()
	worldFrom(ctx).mergeGate = answer
	return nil
}

// mergeGateEntry is the file the system would mount, found through the loader the control plane uses,
// so a manifest that renamed its entry point cannot leave this pointing at the old path.
func mergeGateEntry() (string, error) {
	hooks, err := hook.Load("../hooks")
	if err != nil {
		return "", fmt.Errorf("loading the hooks this build ships (run `make hooks` first): %w", err)
	}
	for _, one := range hooks {
		if one.Name != "merge-gate" {
			continue
		}
		if len(one.Events) == 0 {
			return "", errors.New("the merge gate is bound to nothing, so the runtime would never call it")
		}
		return filepath.Join("../hooks", one.Name, one.Events[0].Entry), nil
	}
	return "", errors.New("this build ships no merge gate, so these scenarios prove nothing")
}

// isExit says whether the command ended with this exit code, which is how a hook answers.
func isExit(err error, code int) bool {
	var ended *exec.ExitError
	return errors.As(err, &ended) && ended.ExitCode() == code
}

func workspaceHook(ctx context.Context, name string, want bool) error {
	w := worldFrom(ctx)
	listed, err := w.client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: w.workspaceID})
	if err != nil {
		return err
	}
	held := false
	for _, one := range listed.GetHooks() {
		if one.GetName() == name {
			held = true
		}
	}
	if held != want {
		return fmt.Errorf("the workspace runs under %q: %t, want %t", name, held, want)
	}
	return nil
}
