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
// is the copy, so these read both: a record that is only on the log is a record a system with no
// broker would never have.
func initializeJobEventsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job titled "([^"]*)" is declared$`, func(ctx context.Context, title string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: title, Brief: "open the bill and say when it is due",
		})
	})

	sc.Step(`^the system's event log refuses every record$`, func(ctx context.Context) error {
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

	sc.Step(`^a job whose title carries that secret is declared$`, func(ctx context.Context) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "deploy with " + theSecretValue, Brief: "open the bill and say when it is due",
		})
	})

	sc.Step(`^the log carries a "([^"]*)" record for that job$`, func(ctx context.Context, kind string) error {
		records, err := jobRecords(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.GetKind() == kind {
				return nil
			}
		}
		return fmt.Errorf("the log carries %v, and not %q", jobKindsOf(records), kind)
	})

	sc.Step(`^that record is keyed by the job, so one job keeps its order$`,
		func(ctx context.Context) error {
			w := worldFrom(ctx)
			declared, err := lastJob(ctx)
			if err != nil {
				return err
			}
			for _, record := range w.events.RecordsOn(jobTopic(w)) {
				if string(record.Key) != declared.GetId() {
					return fmt.Errorf("a record is keyed by %q, and a broker keeps order inside one "+
						"partition and nowhere else", string(record.Key))
				}
			}
			return nil
		})

	sc.Step(`^that record carries the trace the job belongs to$`, func(ctx context.Context) error {
		declared, err := lastJob(ctx)
		if err != nil {
			return err
		}
		if declared.GetTraceId() == "" {
			return fmt.Errorf("the job itself carries no trace")
		}
		records, err := jobRecords(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.GetTraceId() != declared.GetTraceId() {
				return fmt.Errorf("a %s record traces %q and the job traces %q",
					record.GetKind(), record.GetTraceId(), declared.GetTraceId())
			}
		}
		return nil
	})

	sc.Step(`^the log carries "([^"]*)", "([^"]*)", "([^"]*)" and "([^"]*)" for that job$`,
		func(ctx context.Context, first, second, third, fourth string) error {
			records, err := jobRecords(ctx)
			if err != nil {
				return err
			}
			want := []string{first, second, third, fourth}
			got := jobKindsOf(records)
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
		records, err := jobRecords(ctx)
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

	sc.Step(`^the task that ran the job carries the job's own trace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		declared, err := firstJob(ctx)
		if err != nil {
			return err
		}
		found, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetId()})
		if err != nil {
			return err
		}
		if found.GetJob().GetSession() == "" {
			return fmt.Errorf("the job says no session ran it")
		}
		tasks, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{
			Session: found.GetJob().GetSession(),
		})
		if err != nil {
			return err
		}
		if len(tasks.GetTasks()) == 0 {
			return fmt.Errorf("no task was recorded against the session that did the job")
		}
		for _, task := range tasks.GetTasks() {
			if task.GetTraceId() != found.GetJob().GetTraceId() {
				return fmt.Errorf("the task traces %q and the job traces %q, so nothing joins them",
					task.GetTraceId(), found.GetJob().GetTraceId())
			}
		}
		return nil
	})

	sc.Step(`^the record for that job does not carry the secret$`, func(ctx context.Context) error {
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

// jobTopic is where a workspace's job records are published.
func jobTopic(w *world) string { return w.workspaceName + ".job" }

// jobRecords is every job record on the log, decoded, in the order it was published.
func jobRecords(ctx context.Context) ([]*quaycrewv1.JobEvent, error) {
	w := worldFrom(ctx)
	if w.events == nil {
		return nil, fmt.Errorf("this system has no event log, so nothing was exported")
	}
	var events []*quaycrewv1.JobEvent
	for _, record := range w.events.RecordsOn(jobTopic(w)) {
		var event quaycrewv1.JobEvent
		if err := proto.Unmarshal(record.Value, &event); err != nil {
			return nil, fmt.Errorf("a record on %s does not decode as a job event: %w", record.Topic, err)
		}
		events = append(events, &event)
	}
	return events, nil
}

// storedDetails is what the store holds about the last job, which is the record a system
// with no broker keeps.
func storedDetails(ctx context.Context) ([]string, error) {
	w := worldFrom(ctx)
	declared, err := lastJob(ctx)
	if err != nil {
		return nil, err
	}
	kept, err := w.store.ListJobEvents(ctx, declared.GetId())
	if err != nil {
		return nil, err
	}
	details := make([]string, 0, len(kept))
	for _, event := range kept {
		details = append(details, event.Detail)
	}
	if len(details) == 0 {
		return nil, fmt.Errorf("the store holds no records for that job")
	}
	return details, nil
}

func jobKindsOf(events []*quaycrewv1.JobEvent) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.GetKind())
	}
	return kinds
}
