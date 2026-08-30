package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cucumber/godog"
)

// The steps that run the name the tool used to have.
//
// It is a second binary beside the tool, and what it does is only visible from outside the process:
// the stream it wrote on, and the status it exited with. So these scenarios build it and run it, the
// way a shell finds it on the path.

// oldNameBinary builds the old name once and answers with the path to it.
func oldNameBinary() (string, error) {
	oldNameOnce.Do(func() {
		dir, err := os.MkdirTemp("", "krewe-old-name")
		if err != nil {
			oldNameErr = err
			return
		}
		oldName = filepath.Join(dir, "quay")
		out, err := exec.Command("go", "build", "-o", oldName, "../cmd/quay").CombinedOutput()
		if err != nil {
			oldNameErr = fmt.Errorf("building the old name: %w: %s", err, out)
		}
	})
	return oldName, oldNameErr
}

var (
	oldNameOnce sync.Once
	oldName     string
	oldNameErr  error
)

func initializeToolNameSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller types the old name with "([^"]*)"$`, func(ctx context.Context, typed string) error {
		binary, err := oldNameBinary()
		if err != nil {
			return err
		}
		return runBinarySaying(ctx, binary, "", strings.Fields(typed)...)
	})

	// On its own, which is the one an operator types most: the old name with no arguments opened the
	// console, so it is in more fingers than any command is.
	sc.Step(`^the caller types the old name on its own$`, func(ctx context.Context) error {
		binary, err := oldNameBinary()
		if err != nil {
			return err
		}
		return runBinarySaying(ctx, binary, "")
	})

	sc.Step(`^standard error says to type "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("standard error", toolFrom(ctx).stderr, `"`+want+`"`)
	})

	// The refusal must never send the operator back to the word it is refusing. A refusal that names
	// the old name again is a circle, and this repository has been round one before.
	sc.Step(`^standard error never tells the operator to type the old name$`, func(ctx context.Context) error {
		said := toolFrom(ctx).stderr
		if strings.Contains(said, `"quay`) {
			return fmt.Errorf("standard error tells the operator to type the old name again: %q", said)
		}
		return nil
	})
}
