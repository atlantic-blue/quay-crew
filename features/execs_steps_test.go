package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// initializeExecsSteps registers the steps for reading a session's history back.
func initializeExecsSteps(sc *godog.ScenarioContext) {

	sc.Step(`^the session has (\d+) execs?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		if len(w.execs) == 0 {
			return fmt.Errorf("no exec has been dispatched, so there is no session to ask about")
		}
		return sessionHasExecs(ctx, w, w.execs[0].sessionID, want)
	})

	sc.Step(`^each session has (\d+) execs?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		seen := map[string]bool{}
		for _, exec := range w.execs {
			if seen[exec.sessionID] {
				continue
			}
			seen[exec.sessionID] = true
			if err := sessionHasExecs(ctx, w, exec.sessionID, want); err != nil {
				return err
			}
		}
		if len(seen) < 2 {
			return fmt.Errorf("%d sessions were dispatched to, so this proves nothing about separation", len(seen))
		}
		return nil
	})

	sc.Step(`^the first exec says "([^"]*)" and the second says "([^"]*)"$`,
		func(ctx context.Context, first, second string) error {
			w := worldFrom(ctx)
			execs, err := listExecs(ctx, w, w.execs[0].sessionID)
			if err != nil {
				return err
			}
			if len(execs) < 2 {
				return fmt.Errorf("%d execs came back, want at least 2", len(execs))
			}
			if execs[0].GetPrompt() != first {
				return fmt.Errorf("the first exec says %q, want %q", execs[0].GetPrompt(), first)
			}
			if execs[1].GetPrompt() != second {
				return fmt.Errorf("the second exec says %q, want %q", execs[1].GetPrompt(), second)
			}
			if execs[0].GetReply() != w.execs[0].reply {
				return fmt.Errorf("the first exec's reply is %q, want the one the operator got, %q",
					execs[0].GetReply(), w.execs[0].reply)
			}
			return nil
		})

	sc.Step(`^the one exec on that session is recorded as failed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		// A failed dispatch returns no session id, so the session is found through the listing.
		sessions, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if len(sessions.GetSessions()) != 1 {
			return fmt.Errorf("%d sessions exist, want exactly one", len(sessions.GetSessions()))
		}
		execs, err := listExecs(ctx, w, sessions.GetSessions()[0].GetId())
		if err != nil {
			return err
		}
		if len(execs) != 1 {
			return fmt.Errorf("%d execs came back, want 1", len(execs))
		}
		if execs[0].GetStatus() != "failed" {
			return fmt.Errorf("the exec is recorded as %q, want failed", execs[0].GetStatus())
		}
		if execs[0].GetFailure() == "" {
			return fmt.Errorf("the exec does not say what went wrong")
		}
		return nil
	})

	sc.Step(`^the first exec of the session says "([^"]*)" was asked and "([^"]*)" came back$`, func(ctx context.Context, prompt, reply string) error {
		w := worldFrom(ctx)
		if len(w.execs) == 0 {
			return fmt.Errorf("no exec has been dispatched")
		}
		execs, err := listExecs(ctx, w, w.execs[0].sessionID)
		if err != nil {
			return err
		}
		if len(execs) == 0 {
			return fmt.Errorf("the session has no history")
		}
		if execs[0].GetPrompt() != prompt || execs[0].GetReply() != reply {
			return fmt.Errorf("the first exec says %q and %q, want %q and %q",
				execs[0].GetPrompt(), execs[0].GetReply(), prompt, reply)
		}
		return nil
	})

	sc.Step(`^the operator asks for the history of a session that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ListExecs(ctx, &quaycrewv1.ListExecsRequest{Session: "no-such-session"})
		return nil
	})
}

func listExecs(ctx context.Context, w *world, session string) ([]*quaycrewv1.Exec, error) {
	resp, err := w.client.ListExecs(ctx, &quaycrewv1.ListExecsRequest{Session: session})
	if err != nil {
		return nil, err
	}
	return resp.GetExecs(), nil
}

func sessionHasExecs(ctx context.Context, w *world, session string, want int) error {
	execs, err := listExecs(ctx, w, session)
	if err != nil {
		return err
	}
	if len(execs) != want {
		return fmt.Errorf("session %s has %d execs, want %d", session, len(execs), want)
	}
	return nil
}
