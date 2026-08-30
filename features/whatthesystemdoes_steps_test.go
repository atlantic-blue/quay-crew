package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/atlantic-blue/krewe/features"
	"github.com/atlantic-blue/krewe/internal/console"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// The steps for the one list of what this build does: the command that prints it, and the console
// that runs the command rather than holding a view of its own.

func initializeWhatTheSystemDoesSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller asks what this build does$`, func(ctx context.Context) error {
		return runTool(ctx, "features")
	})

	sc.Step(`^standard output names a feature of this build$`, func(ctx context.Context) error {
		first, err := firstFeature()
		if err != nil {
			return err
		}
		return says("standard output", toolFrom(ctx).stdout, first.Title)
	})

	sc.Step(`^standard output names a scenario under that feature$`, func(ctx context.Context) error {
		first, err := firstFeature()
		if err != nil {
			return err
		}
		return says("standard output", toolFrom(ctx).stdout, first.Scenarios[0])
	})

	// The whole way through: the console starts the real tool, waits for it, and shows what it said.
	// Stopping at the command the bar produced would leave the useful half unchecked, which is the
	// half that decides whether the list is still reachable.
	sc.Step(`^the operator types "([^"]*)" in the console$`, func(ctx context.Context, typed string) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx)); err != nil {
			return err
		}
		c.model = c.model.WithCommandRunner(theRealTool(ctx))
		// Tall and wide enough for the whole listing, which is what a real terminal usually is.
		if err := c.press(tea.WindowSizeMsg{Width: 170, Height: 60}); err != nil {
			return err
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")}); err != nil {
			return err
		}
		for _, letter := range typed {
			if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{letter}}); err != nil {
				return err
			}
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	sc.Step(`^the console shows what this build does$`, func(ctx context.Context) error {
		first, err := firstFeature()
		if err != nil {
			return err
		}
		view := consoleFrom(ctx).model.View()
		for _, want := range []string{first.Title, first.Scenarios[0]} {
			if !strings.Contains(view, want) {
				return fmt.Errorf("the console never says %q:\n%s", want, view)
			}
		}
		return nil
	})

	sc.Step(`^the console says to type "([^"]*)"$`, func(ctx context.Context, want string) error {
		view := consoleFrom(ctx).model.View()
		if !strings.Contains(view, "type "+want) {
			return fmt.Errorf("the console never says to type %q:\n%s", want, view)
		}
		return nil
	})

	// The word had to be refused before it reached the tool, which has no command by that name. What
	// the tool says to an unknown command is what would be on the screen if it had.
	sc.Step(`^the console never reached the tool$`, func(ctx context.Context) error {
		if view := consoleFrom(ctx).model.View(); strings.Contains(view, "unknown command") {
			return fmt.Errorf("the word reached the tool, which has no command by that name:\n%s", view)
		}
		return nil
	})
}

// firstFeature is the feature this build lists first, which is what the top of the listing says.
func firstFeature() (features.Feature, error) {
	all := features.All()
	if len(all) == 0 || len(all[0].Scenarios) == 0 {
		return features.Feature{}, fmt.Errorf("this build carries no specification, so nothing here proves anything")
	}
	return all[0], nil
}

// theRealTool is what the console's command bar runs: a tool built from this checkout, in its own
// process. The runner the console ships with resolves the running program, which under a test is the
// test binary, so this is the nearest honest thing to it.
//
// The home directory is a temporary one, because the tool keeps where the operator is standing on
// the machine it runs on and a scenario must not read or write the operator's own.
func theRealTool(ctx context.Context) console.CommandRunner {
	address, token := toolFrom(ctx).address, worldFrom(ctx).token
	return func(ctx context.Context, args []string) (string, error) {
		binary, err := kreweBinary()
		if err != nil {
			return "", err
		}
		home, err := os.MkdirTemp("", "quaycrew-console-")
		if err != nil {
			return "", err
		}
		defer func() { _ = os.RemoveAll(home) }()

		command := exec.CommandContext(ctx, binary, args...)
		command.Env = append(os.Environ(),
			"QC_GRPC_ADDR="+address,
			"QC_TOKEN="+token,
			"QUAY_HOME="+home,
			"HOME="+home,
		)
		// Both streams, because a refusal comes back on the error one and that is what the operator
		// most needs to read. This is what the shipped runner does too.
		output, err := command.CombinedOutput()
		return string(output), err
	}
}
