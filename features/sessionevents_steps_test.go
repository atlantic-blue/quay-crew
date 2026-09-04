package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/encoding/prototext"
)

// The session lifecycle, driven through the same interface every other caller uses. What these prove
// is that the system says what happened to a session, in order, whether or not a broker is listening.

// sessionEventsOf reads back one session's lifecycle from the store, oldest first.
// An exec that did not land leaves no record of itself in the scenario's own list, which is exactly
// the case where the events matter most. So a scenario with no landed exec asks for the whole system's
// events instead, which these scenarios run one session at a time to keep honest.
func sessionEventsOf(ctx context.Context, w *world) ([]*quaycrewv1.SessionEvent, error) {
	session := ""
	if current, err := w.lastExec(); err == nil {
		session = current.sessionID
	}
	resp, err := w.client.ListSessionEvents(ctx, &quaycrewv1.ListSessionEventsRequest{Session: session})
	if err != nil {
		return nil, err
	}
	return resp.GetEvents(), nil
}

// kindsOf is the events as the words a consumer switches on, which is what a scenario asserts.
func kindsOf(events []*quaycrewv1.SessionEvent) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.GetKind())
	}
	return kinds
}

func initializeSessionEventsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the session's events read (.+)$`, func(ctx context.Context, listed string) error {
		w := worldFrom(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		events, err := sessionEventsOf(ctx, w)
		if err != nil {
			return err
		}
		want := quotedWords(listed)
		got := kindsOf(events)
		if strings.Join(got, ", ") != strings.Join(want, ", ") {
			return fmt.Errorf("the session says %v, want %v", got, want)
		}
		return nil
	})

	sc.Step(`^the session's events end with (.+)$`, func(ctx context.Context, listed string) error {
		w := worldFrom(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		events, err := sessionEventsOf(ctx, w)
		if err != nil {
			return err
		}
		want := quotedWords(listed)
		got := kindsOf(events)
		if len(got) < len(want) {
			return fmt.Errorf("the session says %v, which is shorter than %v", got, want)
		}
		if strings.Join(got[len(got)-len(want):], ", ") != strings.Join(want, ", ") {
			return fmt.Errorf("the session ends %v, want it to end %v", got, want)
		}
		return nil
	})

	sc.Step(`^the completed event carries what the model replied$`, func(ctx context.Context) error {
		events, err := sessionEventsOf(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.GetKind() != "session.completed" {
				continue
			}
			if event.GetDetail() == "" {
				return fmt.Errorf("the completed event carries nothing, so a consumer learns only that it ended")
			}
			return nil
		}
		return fmt.Errorf("no event says the job completed: %v", kindsOf(events))
	})

	sc.Step(`^the errored event says why$`, func(ctx context.Context) error {
		events, err := sessionEventsOf(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.GetKind() != "session.errored" {
				continue
			}
			if event.GetDetail() == "" {
				return fmt.Errorf("the errored event says nothing about what went wrong")
			}
			return nil
		}
		return fmt.Errorf("no event says the job errored: %v", kindsOf(events))
	})

	sc.Step(`^no event's kind is "([^"]*)" or "([^"]*)"$`, func(ctx context.Context, first, second string) error {
		events, err := sessionEventsOf(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.GetKind() == first || event.GetKind() == second {
				return fmt.Errorf("%q is a state rather than something that happened, and it is on the log", event.GetKind())
			}
		}
		return nil
	})

	sc.Step(`^nothing in the session's events says "([^"]*)"$`, func(ctx context.Context, secret string) error {
		w := worldFrom(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		events, err := sessionEventsOf(ctx, w)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return fmt.Errorf("the session says nothing happened to it, so this proves nothing")
		}
		for _, event := range events {
			// The whole record rather than the detail alone: a value that reached any field of it is a
			// value that reached the store and the log.
			if strings.Contains(prototext.Format(event), secret) {
				return fmt.Errorf("a %s event carries the secret", event.GetKind())
			}
		}
		return nil
	})
}

// quotedWords reads a list written the way a scenario writes one, "a", "b", "c", into the words
// themselves, so a step can compare what happened against what the scenario says.
func quotedWords(listed string) []string {
	words := make([]string, 0, 3)
	for _, part := range strings.Split(listed, ",") {
		words = append(words, strings.Trim(strings.TrimSpace(part), `"`))
	}
	return words
}
