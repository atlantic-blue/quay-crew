package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

// The front door's scenarios read README.md and check it against the things it makes claims about.
// They touch no control plane: what a reader is promised, and whether the crew can deliver it, is a
// behaviour somebody sees the moment they type the first line.
//
// The checks themselves live in frontdoor_test.go, next to the plain Go cases that run them without
// the Gherkin. One implementation, two ways in.

type frontDoorWorld struct {
	// body is the front door as the reader has it.
	body string
}

type frontDoorKey struct{}

func frontDoorFrom(ctx context.Context) *frontDoorWorld {
	f, _ := ctx.Value(frontDoorKey{}).(*frontDoorWorld)
	return f
}

func initializeFrontDoorSteps(sc *godog.ScenarioContext) {
	initializeFrontDoorDifferenceSteps(sc)

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, frontDoorKey{}, &frontDoorWorld{}), nil
	})

	sc.Step(`^a reader opens the front door$`, func(ctx context.Context) error {
		body, err := os.ReadFile(theFrontDoor)
		if err != nil {
			return err
		}
		frontDoorFrom(ctx).body = string(body)
		return nil
	})

	sc.Step(`^it holds those three parts and no other section$`, func(ctx context.Context) error {
		if wrong := theShapeOf(frontDoorFrom(ctx).body); len(wrong) > 0 {
			return fmt.Errorf("%s", strings.Join(wrong, "\n"))
		}
		return nil
	})

	sc.Step(`^it is shorter than the length a person gives it$`, func(ctx context.Context) error {
		if held := linesIn(frontDoorFrom(ctx).body); held > theLongestFrontDoorWorthReading {
			return fmt.Errorf("it is %d lines, and nobody reads more than %d of them",
				held, theLongestFrontDoorWorthReading)
		}
		return nil
	})

	sc.Step(`^every command it says to run is one the crew has$`, func(ctx context.Context) error {
		commands, err := quayCommands()
		if err != nil {
			return err
		}
		named := namedAfter("quay", codeIn(frontDoorFrom(ctx).body))
		if len(named) == 0 {
			return fmt.Errorf("the front door names no command at all, so this proved nothing")
		}
		var missing []string
		for _, one := range named {
			if !commands[one] {
				missing = append(missing, "quay "+one)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("it tells a reader to run %s, and the crew has no such command",
				strings.Join(missing, ", "))
		}
		return nil
	})

	sc.Step(`^every make target it says to run is one the Makefile declares$`, func(ctx context.Context) error {
		targets, err := makeTargets()
		if err != nil {
			return err
		}
		named := namedAfter("make", codeIn(frontDoorFrom(ctx).body))
		if len(named) == 0 {
			return fmt.Errorf("the front door names no target at all, so this proved nothing")
		}
		var missing []string
		for _, one := range named {
			if !targets[one] {
				missing = append(missing, "make "+one)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("it tells a reader to run %s, and there is no such target",
				strings.Join(missing, ", "))
		}
		return nil
	})

	sc.Step(`^every document it points at is there$`, func(ctx context.Context) error {
		links := linkedFiles(frontDoorFrom(ctx).body)
		if len(links) == 0 {
			return fmt.Errorf("the front door points at nothing, so this proved nothing")
		}
		for _, link := range links {
			if _, err := os.Stat(filepath.Join("..", link)); err != nil {
				return fmt.Errorf("it points a reader at %s, which is not there", link)
			}
		}
		return nil
	})

	sc.Step(`^the quick start is one command to a running crew$`, func(ctx context.Context) error {
		quickStart, err := sectionOf(frontDoorFrom(ctx).body, "## Quick start")
		if err != nil {
			return err
		}
		if !strings.Contains(quickStart, "make install") {
			return fmt.Errorf("the quick start does not name make install:\n%s", quickStart)
		}
		for _, retired := range []string{"make config", "make sandbox-image", "make up"} {
			if strings.Contains(quickStart, retired) {
				return fmt.Errorf("the quick start still says to run %q, so a first run is more than "+
					"one command again", strings.TrimSpace(retired))
			}
		}
		return nil
	})

	sc.Step(`^the document it names for the work carries a picture of one, through the controller, the lease, the session and the role$`, func(ctx context.Context) error {
		if wrong := thePictureOfAPieceOfWork(frontDoorFrom(ctx).body); len(wrong) > 0 {
			return fmt.Errorf("%s", strings.Join(wrong, "\n"))
		}
		return nil
	})

	sc.Step(`^it holds no blockquote, no table and no dash used as punctuation$`, func(ctx context.Context) error {
		if found := unreusableMarkdownIn(frontDoorFrom(ctx).body); len(found) > 0 {
			return fmt.Errorf("a reader cannot copy this back out:\n%s", strings.Join(found, "\n"))
		}
		return nil
	})
}

// The steps below are registered from initializeFrontDoorSteps, and are kept here rather than inside
// it so the rule they hold up reads on its own.

// initializeFrontDoorDifferenceSteps holds the front door to sending a reader somewhere that answers
// the question they ask first: not what a piece of work is, but how it differs from the task they
// already know how to send. The answer itself lives in the document, not in the front door.
func initializeFrontDoorDifferenceSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the document it names for the words tells a task and a piece of work apart$`, func(ctx context.Context) error {
		if wrong := theDifferenceBetweenATaskAndAPieceOfWork(frontDoorFrom(ctx).body); len(wrong) > 0 {
			return fmt.Errorf("%s", strings.Join(wrong, "\n"))
		}
		return nil
	})
}
