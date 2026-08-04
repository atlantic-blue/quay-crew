package features_test

import (
	"context"
	"errors"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/projection"
	"github.com/cucumber/godog"
)

// initializeTurnsSteps registers the steps for reading a session's history back.
//
// The projection is driven directly rather than left running in a goroutine: a scenario that slept
// until a consumer caught up would be slow when it passed and flaky when it did not.
func initializeTurnsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the projection has caught up$`, func(ctx context.Context) error {
		return worldFrom(ctx).runProjection(ctx)
	})

	sc.Step(`^every record on the log is delivered again$`, func(ctx context.Context) error {
		// At least once delivery, made to happen on purpose rather than waited for.
		return worldFrom(ctx).runProjection(ctx)
	})

	sc.Step(`^the session has (\d+) turns?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		if len(w.turns) == 0 {
			return fmt.Errorf("no turn has been dispatched, so there is no session to ask about")
		}
		return sessionHasTurns(ctx, w, w.turns[0].sessionID, want)
	})

	sc.Step(`^each session has (\d+) turns?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		seen := map[string]bool{}
		for _, turn := range w.turns {
			if seen[turn.sessionID] {
				continue
			}
			seen[turn.sessionID] = true
			if err := sessionHasTurns(ctx, w, turn.sessionID, want); err != nil {
				return err
			}
		}
		if len(seen) < 2 {
			return fmt.Errorf("%d sessions were dispatched to, so this proves nothing about separation", len(seen))
		}
		return nil
	})

	sc.Step(`^the first turn says "([^"]*)" and the second says "([^"]*)"$`,
		func(ctx context.Context, first, second string) error {
			w := worldFrom(ctx)
			turns, err := listTurns(ctx, w, w.turns[0].sessionID)
			if err != nil {
				return err
			}
			if len(turns) < 2 {
				return fmt.Errorf("%d turns came back, want at least 2", len(turns))
			}
			if turns[0].GetPrompt() != first {
				return fmt.Errorf("the first turn says %q, want %q", turns[0].GetPrompt(), first)
			}
			if turns[1].GetPrompt() != second {
				return fmt.Errorf("the second turn says %q, want %q", turns[1].GetPrompt(), second)
			}
			if turns[0].GetReply() != w.turns[0].reply {
				return fmt.Errorf("the first turn's reply is %q, want the one the operator got, %q",
					turns[0].GetReply(), w.turns[0].reply)
			}
			return nil
		})

	sc.Step(`^the one turn on that session is recorded as failed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		// A failed dispatch returns no session id, so the session is found through the listing.
		sessions, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if len(sessions.GetSessions()) != 1 {
			return fmt.Errorf("%d sessions exist, want exactly one", len(sessions.GetSessions()))
		}
		turns, err := listTurns(ctx, w, sessions.GetSessions()[0].GetId())
		if err != nil {
			return err
		}
		if len(turns) != 1 {
			return fmt.Errorf("%d turns came back, want 1", len(turns))
		}
		if turns[0].GetStatus() != "failed" {
			return fmt.Errorf("the turn is recorded as %q, want failed", turns[0].GetStatus())
		}
		if turns[0].GetFailure() == "" {
			return fmt.Errorf("the turn does not say what went wrong")
		}
		return nil
	})

	sc.Step(`^the operator asks for the history of a session that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Session: "no-such-session"})
		return nil
	})
}

// runProjection consumes everything on the log into the store, then returns, rather than blocking
// the way it does in the running crew.
//
// The in memory log is set to stop once it has handed over everything it holds, so this returns when
// the projection has caught up rather than when a timer says it probably has.
func (w *world) runProjection(ctx context.Context) error {
	if w.events == nil {
		return fmt.Errorf("this scenario has no event log, so there is nothing to project")
	}
	err := projection.New(w.events, w.store, nil).Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func listTurns(ctx context.Context, w *world, session string) ([]*quaycrewv1.Turn, error) {
	resp, err := w.client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Session: session})
	if err != nil {
		return nil, err
	}
	return resp.GetTurns(), nil
}

func sessionHasTurns(ctx context.Context, w *world, session string, want int) error {
	turns, err := listTurns(ctx, w, session)
	if err != nil {
		return err
	}
	if len(turns) != want {
		return fmt.Errorf("session %s has %d turns, want %d", session, len(turns), want)
	}
	return nil
}
