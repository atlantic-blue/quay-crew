package features_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/cucumber/godog"
)

// The prose gate, run the way the model runtime runs it: the entry point the system would mount, fed
// a PreToolUse payload on standard input, answering with an exit code.
//
// It runs the real binary rather than calling the hook's own code, because the hook is a separate
// module and the system cannot import it. That is the point of the shape: what is proved here is the
// file a sandbox mounts, so a gate whose entry point was never built fails this rather than passing
// over nothing.

func initializeProseGateSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a session is about to write "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, prose, path string) error {
			payload, err := json.Marshal(map[string]any{
				"tool_name":  "Write",
				"tool_input": map[string]string{"file_path": path, "content": prose},
			})
			if err != nil {
				return err
			}
			return fireProseGate(ctx, string(payload))
		})

	sc.Step(`^a session runs the command: (.+)$`, func(ctx context.Context, command string) error {
		payload, err := json.Marshal(map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]string{"command": command},
		})
		if err != nil {
			return err
		}
		return fireProseGate(ctx, string(payload))
	})

	sc.Step(`^a session sends the prose gate a payload it cannot read$`, func(ctx context.Context) error {
		return fireProseGate(ctx, "this is not the payload a runtime sends")
	})

	sc.Step(`^the prose gate refuses it$`, func(ctx context.Context) error {
		if !worldFrom(ctx).proseGate.refused {
			return errors.New("the gate let it through, and the standard refuses it")
		}
		return nil
	})

	sc.Step(`^the prose gate allows it$`, func(ctx context.Context) error {
		answer := worldFrom(ctx).proseGate
		if answer.refused {
			return fmt.Errorf("the gate refused prose the standard allows, which is how a gate gets turned off:\n%s",
				answer.said)
		}
		return nil
	})

	sc.Step(`^the refusal quotes the prose and says what to do to it$`, func(ctx context.Context) error {
		said := worldFrom(ctx).proseGate.said
		if !strings.Contains(said, `"`) {
			return fmt.Errorf("the refusal quotes nothing, so the writer has to go and find it:\n%s", said)
		}
		// One of the four things a writer can actually do about what this gate measures.
		for _, act := range []string{"Split it", "Start a new paragraph", "Write it in the", "Use a comma"} {
			if strings.Contains(said, act) {
				return nil
			}
		}
		return fmt.Errorf("the refusal does not say what to do about it, so it is worth nothing:\n%s", said)
	})

	sc.Step(`^the refusal names the standard and says the vocabulary and the idioms are a person's$`,
		func(ctx context.Context) error {
			said := worldFrom(ctx).proseGate.said
			for _, needed := range []string{"asd-ste100.org", "vocabulary", "idiom"} {
				if !strings.Contains(said, needed) {
					return fmt.Errorf("the refusal does not say %q, so a session reads this gate as the whole standard:\n%s",
						needed, said)
				}
			}
			return nil
		})
}

// fireProseGate runs the shipped entry point over one payload and records what it said.
func fireProseGate(ctx context.Context, payload string) error {
	entry, err := shippedEntry("prose-gate")
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
	worldFrom(ctx).proseGate = answer
	return nil
}

// shippedEntry is the file the system would mount for one shipped hook, found through the loader the
// control plane uses, so a manifest that renamed its entry point cannot leave this pointing at the
// old path.
func shippedEntry(name string) (string, error) {
	hooks, err := hook.Load("../hooks")
	if err != nil {
		return "", fmt.Errorf("loading the hooks this build ships (run `make hooks` first): %w", err)
	}
	for _, one := range hooks {
		if one.Name != name {
			continue
		}
		if len(one.Events) == 0 {
			return "", fmt.Errorf("%s is bound to nothing, so the runtime would never call it", name)
		}
		return filepath.Join("../hooks", one.Name, one.Events[0].Entry), nil
	}
	return "", fmt.Errorf("this build ships no %s, so these scenarios prove nothing", name)
}
