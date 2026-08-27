package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/proto"
)

// theSecretValue is what a scenario seals and then types into a title, so it can prove the value
// never reaches a record. It looks like a credential because that is what it stands for.
const theSecretValue = "sk-ant-the-value-nobody-should-store"

// Steps for the scenarios about the record every movement leaves. The store is the truth and the log
// is the copy, so these read both: a record that is only on the log is a record a crew with no
// broker would never have.
func initializeWorkEventsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a piece of work titled "([^"]*)" is declared$`, func(ctx context.Context, title string) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: title, Brief: "open the bill and say when it is due",
		})
	})

	sc.Step(`^the crew's event log refuses every record$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.eventsRefuse = true
		return w.restart()
	})

	sc.Step(`^the workspace holds the secret "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace: w.workspaceID, Key: name, Value: theSecretValue,
		})
		return err
	})

	sc.Step(`^a piece of work whose title carries that secret is declared$`, func(ctx context.Context) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "deploy with " + theSecretValue, Brief: "open the bill and say when it is due",
		})
	})

	sc.Step(`^the log carries a "([^"]*)" record for that work$`, func(ctx context.Context, kind string) error {
		records, err := workRecords(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.GetKind() == kind {
				return nil
			}
		}
		return fmt.Errorf("the log carries %v, and not %q", workKindsOf(records), kind)
	})

	sc.Step(`^that record is keyed by the work, so one piece of work keeps its order$`,
		func(ctx context.Context) error {
			w := worldFrom(ctx)
			declared, err := lastWork(ctx)
			if err != nil {
				return err
			}
			for _, record := range w.events.RecordsOn(workTopic(w)) {
				if string(record.Key) != declared.GetId() {
					return fmt.Errorf("a record is keyed by %q, and a broker keeps order inside one "+
						"partition and nowhere else", string(record.Key))
				}
			}
			return nil
		})

	sc.Step(`^that record carries the trace the work belongs to$`, func(ctx context.Context) error {
		declared, err := lastWork(ctx)
		if err != nil {
			return err
		}
		if declared.GetTraceId() == "" {
			return fmt.Errorf("the work itself carries no trace")
		}
		records, err := workRecords(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.GetTraceId() != declared.GetTraceId() {
				return fmt.Errorf("a %s record traces %q and the work traces %q",
					record.GetKind(), record.GetTraceId(), declared.GetTraceId())
			}
		}
		return nil
	})

	sc.Step(`^the log carries "([^"]*)", "([^"]*)", "([^"]*)" and "([^"]*)" for that work$`,
		func(ctx context.Context, first, second, third, fourth string) error {
			records, err := workRecords(ctx)
			if err != nil {
				return err
			}
			want := []string{first, second, third, fourth}
			got := workKindsOf(records)
			if len(got) != len(want) {
				return fmt.Errorf("the log carries %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					return fmt.Errorf("the log carries %v, want %v", got, want)
				}
			}
			return nil
		})

	sc.Step(`^every record on the log carries the same trace$`, func(ctx context.Context) error {
		records, err := workRecords(ctx)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return fmt.Errorf("nothing is on the log, so there is no trace to read")
		}
		first := records[0].GetTraceId()
		if first == "" {
			return fmt.Errorf("the first record carries no trace")
		}
		for _, record := range records {
			if record.GetTraceId() != first {
				return fmt.Errorf("a %s record traces %q and the first traces %q",
					record.GetKind(), record.GetTraceId(), first)
			}
		}
		return nil
	})

	sc.Step(`^the task that ran the work carries the work's own trace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		declared, err := firstWork(ctx)
		if err != nil {
			return err
		}
		found, err := w.client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: declared.GetId()})
		if err != nil {
			return err
		}
		if found.GetWork().GetSession() == "" {
			return fmt.Errorf("the work says no session ran it")
		}
		tasks, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{
			Session: found.GetWork().GetSession(),
		})
		if err != nil {
			return err
		}
		if len(tasks.GetTasks()) == 0 {
			return fmt.Errorf("no task was recorded against the session that did the work")
		}
		for _, task := range tasks.GetTasks() {
			if task.GetTraceId() != found.GetWork().GetTraceId() {
				return fmt.Errorf("the task traces %q and the work traces %q, so nothing joins them",
					task.GetTraceId(), found.GetWork().GetTraceId())
			}
		}
		return nil
	})

	sc.Step(`^the record for that work does not carry the secret$`, func(ctx context.Context) error {
		details, err := storedDetails(ctx)
		if err != nil {
			return err
		}
		for _, detail := range details {
			if strings.Contains(detail, theSecretValue) {
				return fmt.Errorf("a record carries the secret: %q", detail)
			}
		}
		return nil
	})

	sc.Step(`^the record names the secret that was taken out$`, func(ctx context.Context) error {
		details, err := storedDetails(ctx)
		if err != nil {
			return err
		}
		for _, detail := range details {
			if strings.Contains(detail, "ANTHROPIC_API_KEY") {
				return nil
			}
		}
		return fmt.Errorf("no record says which secret was taken out: %v", details)
	})

	sc.Step(`^the log does not carry the secret$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		for _, record := range w.events.Records() {
			if strings.Contains(string(record.Value), theSecretValue) {
				return fmt.Errorf("a record on %s carries the secret", record.Topic)
			}
		}
		return nil
	})
}

// workTopic is where a workspace's work records are published.
func workTopic(w *world) string { return w.workspaceName + ".work" }

// workRecords is every work record on the log, decoded, in the order it was published.
func workRecords(ctx context.Context) ([]*quaycrewv1.WorkEvent, error) {
	w := worldFrom(ctx)
	if w.events == nil {
		return nil, fmt.Errorf("this crew has no event log, so nothing was exported")
	}
	var events []*quaycrewv1.WorkEvent
	for _, record := range w.events.RecordsOn(workTopic(w)) {
		var event quaycrewv1.WorkEvent
		if err := proto.Unmarshal(record.Value, &event); err != nil {
			return nil, fmt.Errorf("a record on %s does not decode as a work event: %w", record.Topic, err)
		}
		events = append(events, &event)
	}
	return events, nil
}

// storedDetails is what the store holds about the last piece of work, which is the record a crew
// with no broker keeps.
func storedDetails(ctx context.Context) ([]string, error) {
	w := worldFrom(ctx)
	declared, err := lastWork(ctx)
	if err != nil {
		return nil, err
	}
	kept, err := w.store.ListWorkEvents(ctx, declared.GetId())
	if err != nil {
		return nil, err
	}
	details := make([]string, 0, len(kept))
	for _, event := range kept {
		details = append(details, event.Detail)
	}
	if len(details) == 0 {
		return nil, fmt.Errorf("the store holds no records for that work")
	}
	return details, nil
}

func workKindsOf(events []*quaycrewv1.WorkEvent) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.GetKind())
	}
	return kinds
}
