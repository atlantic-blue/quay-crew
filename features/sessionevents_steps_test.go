package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/messaging"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// The session lifecycle, driven through the same interface every other caller uses. What these prove
// is that the system says what happened to a session, in order, whether or not a broker is listening.

// sessionEventsOf reads back one session's lifecycle from the store, oldest first.
// A task that did not land leaves no record of itself in the scenario's own list, which is exactly
// the case where the events matter most. So a scenario with no landed task asks for the whole system's
// events instead, which these scenarios run one session at a time to keep honest.
func sessionEventsOf(ctx context.Context, w *world) ([]*quaycrewv1.SessionEvent, error) {
	session := ""
	if current, err := w.lastTask(); err == nil {
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

// sessionEventsOn decodes every session event published to a workspace's stream, oldest first.
func sessionEventsOn(w *world, workspaceName string) ([]*quaycrewv1.SessionEvent, error) {
	topic, err := messaging.Topic(workspaceName, "sessions")
	if err != nil {
		return nil, err
	}
	if w.events == nil {
		return nil, fmt.Errorf("this scenario has no event log, so nothing can be on it")
	}
	events := make([]*quaycrewv1.SessionEvent, 0)
	for _, record := range w.events.RecordsOn(topic) {
		event := &quaycrewv1.SessionEvent{}
		if err := proto.Unmarshal(record.Value, event); err != nil {
			return nil, fmt.Errorf("a record on %s is not a session event: %w", topic, err)
		}
		events = append(events, event)
	}
	return events, nil
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

	sc.Step(`^(\d+) session events are on the log for "([^"]*)"$`, func(ctx context.Context, want int, workspaceName string) error {
		w := worldFrom(ctx)
		if err := w.settled(ctx); err != nil {
			return err
		}
		events, err := sessionEventsOn(w, workspaceName)
		if err != nil {
			return err
		}
		if len(events) != want {
			return fmt.Errorf("%d session events are on the log for %q, want %d: %v",
				len(events), workspaceName, want, kindsOf(events))
		}
		return nil
	})

	sc.Step(`^every published session event is keyed by its session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		topic, err := messaging.Topic(w.workspaceName, "sessions")
		if err != nil {
			return err
		}
		records := w.events.RecordsOn(topic)
		if len(records) == 0 {
			return fmt.Errorf("nothing is on %s", topic)
		}
		for _, record := range records {
			event := &quaycrewv1.SessionEvent{}
			if err := proto.Unmarshal(record.Value, event); err != nil {
				return err
			}
			if string(record.Key) != event.GetSession() {
				return fmt.Errorf("a record is keyed %q while its event is for session %q, so one session's records could land on two partitions",
					record.Key, event.GetSession())
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
