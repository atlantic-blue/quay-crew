package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/cucumber/godog"
)

// The scenarios about why a task failed run the real model adapter, because the explanation is built
// out of what came back from the sandbox and a double has nothing to build it from.

// theToken is the value these scenarios set, so a Then step can say the refusal does not carry it
// without the feature file having to repeat it into every assertion.
const theToken = "sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP"

// refusalStream is the shape the model reports a refused task in: on standard output, on the same
// result event a reply arrives on. Taken from a real task against a rejected token.
func refusalStream(reason string) string {
	return fmt.Sprintf(
		`{"type":"result","is_error":true,"api_error_status":401,"result":%q,"session_id":"c1"}`, reason)
}

func initializeFailureSteps(sc *godog.ScenarioContext) {
	// useTheRealRunner swaps the recording double for the real adapter and restarts, so the task goes
	// through the code that builds the explanation.
	useTheRealRunner := func(w *world) error {
		w.realRunner = model.NewClaudeCodeRunner()
		return w.restart()
	}

	sc.Step(`^the model refuses the task saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		w := worldFrom(ctx)
		w.provider.Output = refusalStream(reason)
		w.provider.ExitErr = fmt.Errorf("exit status 1")
		return useTheRealRunner(w)
	})

	sc.Step(`^the model refuses the task quoting the token back$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.provider.Output = refusalStream("rejected the token " + theToken)
		w.provider.ExitErr = fmt.Errorf("exit status 1")
		return useTheRealRunner(w)
	})

	sc.Step(`^the sandbox fails with nothing on standard output, saying "([^"]*)"$`,
		func(ctx context.Context, said string) error {
			w := worldFrom(ctx)
			w.provider.Output = ""
			w.provider.Stderr = said
			w.provider.ExitErr = fmt.Errorf("exit status 127")
			return useTheRealRunner(w)
		})

	sc.Step(`^the refusal says "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the task reported success, so there is no refusal to read")
		}
		if !strings.Contains(w.lastErr.Error(), want) {
			return fmt.Errorf("the refusal is %q, want it to say %q", w.lastErr, want)
		}
		return nil
	})

	sc.Step(`^the refusal carries no token$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the task reported success, so there is no refusal to read")
		}
		if strings.Contains(w.lastErr.Error(), theToken) {
			return fmt.Errorf("the refusal carries the subscription token")
		}
		return nil
	})

	// Saying nothing at all would also pass the check above, so the refusal has to admit that
	// something was removed rather than quietly dropping it.
	sc.Step(`^the refusal says something was taken out$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the task reported success, so there is no refusal to read")
		}
		if !strings.Contains(w.lastErr.Error(), "redacted") {
			return fmt.Errorf("the refusal is %q, and never says anything was taken out", w.lastErr)
		}
		return nil
	})
}
