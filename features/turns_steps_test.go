package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// initializeTurnsSteps registers the steps for reading a session's history back.
func initializeTurnsSteps(sc *godog.ScenarioContext) {

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
		sessions, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if len(sessions.GetThreads()) != 1 {
			return fmt.Errorf("%d sessions exist, want exactly one", len(sessions.GetThreads()))
		}
		turns, err := listTurns(ctx, w, sessions.GetThreads()[0].GetId())
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

	sc.Step(`^the first turn of the session says "([^"]*)" was asked and "([^"]*)" came back$`, func(ctx context.Context, prompt, reply string) error {
		w := worldFrom(ctx)
		if len(w.turns) == 0 {
			return fmt.Errorf("no turn has been dispatched")
		}
		turns, err := listTurns(ctx, w, w.turns[0].sessionID)
		if err != nil {
			return err
		}
		if len(turns) == 0 {
			return fmt.Errorf("the session has no history")
		}
		if turns[0].GetPrompt() != prompt || turns[0].GetReply() != reply {
			return fmt.Errorf("the first turn says %q and %q, want %q and %q",
				turns[0].GetPrompt(), turns[0].GetReply(), prompt, reply)
		}
		return nil
	})

	sc.Step(`^the operator asks for the history of a session that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Thread: "no-such-session"})
		return nil
	})
}

func listTurns(ctx context.Context, w *world, session string) ([]*quaycrewv1.Turn, error) {
	resp, err := w.client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Thread: session})
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
