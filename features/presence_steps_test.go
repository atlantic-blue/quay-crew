package features_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/cucumber/godog"
)

// Steps for what a listing says is inside a session's sandbox.
//
// The word is read out of the cells a listing prints rather than off the session's status field,
// because the cell is what the operator reads and the field is what lied to them: status says
// whether a dispatched task is open, and a conversation nobody dispatched is not one.

// presenceWorld is the listing the scenario just drew, and what the sandboxes had been asked before
// it was drawn.
type presenceWorld struct {
	listed []*quaycrewv1.Session
	// askedBefore is the question count taken the moment before the listing, so a Then step measures
	// what the listing itself cost rather than everything the scenario did.
	askedBefore int
	drawn       bool
}

type presenceKey struct{}

func presenceFrom(ctx context.Context) *presenceWorld {
	held, _ := ctx.Value(presenceKey{}).(*presenceWorld)
	return held
}

// listedWord is what the listing says about the session the scenario is about.
func listedWord(ctx context.Context) (string, error) {
	w, drawn := worldFrom(ctx), presenceFrom(ctx)
	if !drawn.drawn {
		return "", fmt.Errorf("nothing has been listed, so there is no word on the screen to read")
	}
	current, err := w.lastTask()
	if err != nil {
		return "", err
	}
	for _, session := range drawn.listed {
		if session.GetId() != current.sessionID {
			continue
		}
		// The status cell of the row a listing prints, which is what both the console and the command
		// line put on the screen.
		return display.SessionCells(session, w.workspaceName, w.projectName)[statusCell], nil
	}
	return "", fmt.Errorf("the listing does not carry the session the scenario is about")
}

// statusCell is where the status sits in a session's row, found from the columns rather than counted
// by hand, so a column added in front of it does not quietly move what this reads.
var statusCell = func() int {
	for index, column := range display.SessionColumns() {
		if column == "status" {
			return index
		}
	}
	return -1
}()

func initializePresenceSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, presenceKey{}, &presenceWorld{}), nil
	})

	// A conversation answering with nobody watching it. The provider is asked, the way the real one
	// reads the container's own process table.
	sc.Step(`^that session's sandbox is running a model runtime$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		w.provider.Wake(current.sessionID)
		return nil
	})

	// Said out loud rather than left implied, because the scenario it belongs to is about somebody
	// sitting in a conversation they have already closed.
	sc.Step(`^that session's sandbox is running nothing$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		if w.provider.Running[current.sessionID] {
			return fmt.Errorf("the sandbox is running a model runtime, and this step says it is not")
		}
		return nil
	})

	sc.Step(`^the daemon will not say what is inside a sandbox$`, func(ctx context.Context) error {
		worldFrom(ctx).provider.RuntimeErr = errors.New("cannot connect to the docker daemon")
		return nil
	})

	sc.Step(`^the operator lists the sessions$`, func(ctx context.Context) error {
		return list(ctx, true)
	})

	sc.Step(`^the operator lists the sessions without asking what is inside them$`, func(ctx context.Context) error {
		return list(ctx, false)
	})

	sc.Step(`^the listing says the session is "([^"]*)"$`, func(ctx context.Context, want string) error {
		word, err := listedWord(ctx)
		if err != nil {
			return err
		}
		if word != want {
			return fmt.Errorf("the listing says %q, want %q", word, want)
		}
		return nil
	})

	// Said separately from the word above, because this is the failure the slice exists to stop and it
	// should fail by name rather than as a mismatch between two words.
	sc.Step(`^the listing does not say the session is idle$`, func(ctx context.Context) error {
		word, err := listedWord(ctx)
		if err != nil {
			return err
		}
		if word == display.StatusIdle {
			return fmt.Errorf("the listing says idle over a container that is somebody's, which is " +
				"the word that invites a restart, a drain or a reclaim")
		}
		return nil
	})

	sc.Step(`^no sandbox was asked what is inside it$`, func(ctx context.Context) error {
		w, drawn := worldFrom(ctx), presenceFrom(ctx)
		if asked := w.provider.Questions() - drawn.askedBefore; asked != 0 {
			return fmt.Errorf("the listing put %d questions to the sandboxes, and it should have "+
				"put none: a console redraws every three seconds", asked)
		}
		return nil
	})
}

// list draws the listing the operator reads, asking what is inside each sandbox or not.
func list(ctx context.Context, presence bool) error {
	w, drawn := worldFrom(ctx), presenceFrom(ctx)
	drawn.askedBefore = w.provider.Questions()
	listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Presence: presence})
	w.lastErr = err
	if err != nil {
		return err
	}
	drawn.listed, drawn.drawn = listed.GetSessions(), true
	// The same rows reach the scenarios about what a conversation cost, so there is one listing in the
	// specification rather than two that could disagree about what a session says.
	usageFrom(ctx).listed = listed.GetSessions()
	return nil
}

// The command line surface, run as its own process. A scenario over the API cannot say whether `krewe
// sessions` asks the sandboxes what is in them, and asking is opt in, so this drives the binary.
func initializePresenceToolSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller lists the sessions$`, func(ctx context.Context) error {
		return runTool(ctx, "sessions")
	})
}

// The negative half of the same reading. The word being wrong is the defect, so the scenario says
// the wrong word is gone rather than only that the right one arrived.
func initializePresenceToolReadingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^standard output does not carry "([^"]*)"$`, func(ctx context.Context, unwanted string) error {
		if out := toolFrom(ctx).stdout; strings.Contains(out, unwanted) {
			return fmt.Errorf("standard output still carries %q:\n%s", unwanted, out)
		}
		return nil
	})
}
