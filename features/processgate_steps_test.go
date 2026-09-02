package features_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cucumber/godog"
)

// The process gate, run the way the model runtime runs it: the entry point the system would mount,
// fed a PreToolUse payload on standard input, answering with an exit code.
//
// It runs the real binary rather than calling the hook's own code, because the hook is a separate
// module and the system cannot import it. That is the point of the shape: what is proved here is the
// file a sandbox mounts, so a gate whose entry point was never built fails this rather than passing
// over nothing.

func initializeProcessGateSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a session is about to run the command: (.+)$`, func(ctx context.Context, command string) error {
		payload, err := json.Marshal(map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]string{"command": command},
		})
		if err != nil {
			return err
		}
		return fireProcessGate(ctx, string(payload))
	})

	sc.Step(`^a session sends the process gate a payload it cannot read$`, func(ctx context.Context) error {
		return fireProcessGate(ctx, "this is not the payload a runtime sends")
	})

	sc.Step(`^the process gate refuses it$`, func(ctx context.Context) error {
		if !worldFrom(ctx).processGate.refused {
			return errors.New("the gate let it through, and it ends something the operator is using")
		}
		return nil
	})

	sc.Step(`^the process gate allows it$`, func(ctx context.Context) error {
		answer := worldFrom(ctx).processGate
		if answer.refused {
			return fmt.Errorf("the gate refused work a session does, which is how a gate gets turned off:\n%s",
				answer.said)
		}
		return nil
	})

	sc.Step(`^the refusal says to end the work in the record, or to ask the operator$`,
		func(ctx context.Context) error {
			said := worldFrom(ctx).processGate.said
			for _, needed := range []string{"krewe job stop", "ask the operator"} {
				if !strings.Contains(said, needed) {
					return fmt.Errorf("the refusal does not say %q, so the session is left guessing:\n%s",
						needed, said)
				}
			}
			return nil
		})

	sc.Step(`^the refusal names the variable the operator sets$`, func(ctx context.Context) error {
		said := worldFrom(ctx).processGate.said
		if !strings.Contains(said, "KREWE_MAY_END_A_PROCESS") {
			return fmt.Errorf("the refusal does not name the variable, so nobody knows what happened:\n%s", said)
		}
		return nil
	})
}

// fireProcessGate runs the shipped entry point over one payload and records what it said.
func fireProcessGate(ctx context.Context, payload string) error {
	entry, err := shippedEntry("process-gate")
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
	worldFrom(ctx).processGate = answer
	return nil
}
