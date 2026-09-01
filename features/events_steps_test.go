package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/messaging"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// eventsWorld is the state of one event log scenario, beside the shared world.
type eventsWorld struct {
	// secondProject is a project in the second workspace, so a scenario can prove two workspaces do
	// not share a stream.
	secondProject string
}

type eventsKey struct{}

func eventsFrom(ctx context.Context) *eventsWorld {
	e, _ := ctx.Value(eventsKey{}).(*eventsWorld)
	return e
}

// tasksOn decodes every task event published to a workspace's stream, oldest first.
func tasksOn(w *world, workspaceName string) ([]*quaycrewv1.TaskEvent, error) {
	topic, err := messaging.Topic(workspaceName, "tasks")
	if err != nil {
		return nil, err
	}
	if w.events == nil {
		return nil, fmt.Errorf("this scenario has no event log, so nothing can be on it")
	}
	events := make([]*quaycrewv1.TaskEvent, 0)
	for _, record := range w.events.RecordsOn(topic) {
		event := &quaycrewv1.TaskEvent{}
		if err := proto.Unmarshal(record.Value, event); err != nil {
			return nil, fmt.Errorf("a record on %s is not a task event: %w", topic, err)
		}
		events = append(events, event)
	}
	return events, nil
}

// onlyTaskOn is the single task on a workspace's stream, and an error when there is not exactly one,
// so a step asserting on "the published task" cannot quietly assert on the first of several.
func onlyTaskOn(w *world, workspaceName string) (*quaycrewv1.TaskEvent, error) {
	events, err := tasksOn(w, workspaceName)
	if err != nil {
		return nil, err
	}
	if len(events) != 1 {
		return nil, fmt.Errorf("%d tasks are on the log for %q, want exactly one", len(events), workspaceName)
	}
	return events[0], nil
}

// initializeEventsSteps registers the steps for what lands on the event log.
func initializeEventsSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, eventsKey{}, &eventsWorld{}), nil
	})

	sc.Step(`^the next task will fail$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.failTheNextTask()
		return nil
	})

	sc.Step(`^the system has no event log configured$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.events = nil
		w.info.Events = ""
		// Standing the control plane up again over the same store is what starting the stack without
		// a broker looks like: the workspace and project from the background are still there.
		return w.restart()
	})

	sc.Step(`^a second workspace named "([^"]*)" with a project$`, func(ctx context.Context, name string) error {
		w, e := worldFrom(ctx), eventsFrom(ctx)
		workspace, err := w.client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: name})
		if err != nil {
			return err
		}
		w.secondWorkspaceID = workspace.GetWorkspace().GetId()
		project, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
			Workspace: w.secondWorkspaceID, Name: "somewhere-else",
		})
		if err != nil {
			return err
		}
		e.secondProject = project.GetProject().GetId()
		return nil
	})

	sc.Step(`^the operator dispatches "([^"]*)" to the second workspace's project$`, func(ctx context.Context, text string) error {
		w, e := worldFrom(ctx), eventsFrom(ctx)
		if e.secondProject == "" {
			return fmt.Errorf("no project was created in the second workspace")
		}
		return w.dispatch(ctx, e.secondProject, "", text)
	})

	sc.Step(`^(\d+) tasks? (?:is|are) on the log for "([^"]*)"$`, func(ctx context.Context, want int, workspaceName string) error {
		events, err := tasksOn(worldFrom(ctx), workspaceName)
		if err != nil {
			return err
		}
		if len(events) != want {
			return fmt.Errorf("%d tasks are on the log for %q, want %d", len(events), workspaceName, want)
		}
		return nil
	})

	sc.Step(`^the published task says "([^"]*)" was asked and "([^"]*)" came back$`,
		func(ctx context.Context, prompt, reply string) error {
			event, err := onlyTaskOn(worldFrom(ctx), "acme")
			if err != nil {
				return err
			}
			if event.GetPrompt() != prompt {
				return fmt.Errorf("the record says %q was asked, want %q", event.GetPrompt(), prompt)
			}
			if event.GetReply() != reply {
				return fmt.Errorf("the record says %q came back, want %q", event.GetReply(), reply)
			}
			if event.GetStatus() != "idle" {
				return fmt.Errorf("the record says the session is %q, want idle", event.GetStatus())
			}
			return nil
		})

	sc.Step(`^the published task is keyed by its session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		topic, err := messaging.Topic("acme", "tasks")
		if err != nil {
			return err
		}
		records := w.events.RecordsOn(topic)
		if len(records) != 1 {
			return fmt.Errorf("%d records are on %s, want exactly one", len(records), topic)
		}
		if len(w.tasks) != 1 {
			return fmt.Errorf("%d tasks ran, want exactly one", len(w.tasks))
		}
		if got, want := string(records[0].Key), w.tasks[0].sessionID; got != want {
			return fmt.Errorf("the record is keyed %q, want the session %q", got, want)
		}
		return nil
	})

	sc.Step(`^the published task carries the workspace, the project and the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		event, err := onlyTaskOn(w, "acme")
		if err != nil {
			return err
		}
		if event.GetWorkspace() != w.workspaceID {
			return fmt.Errorf("the record names workspace %q, want %q", event.GetWorkspace(), w.workspaceID)
		}
		if event.GetProject() != w.projectID {
			return fmt.Errorf("the record names project %q, want %q", event.GetProject(), w.projectID)
		}
		if len(w.tasks) != 1 {
			return fmt.Errorf("%d tasks ran, want exactly one", len(w.tasks))
		}
		if event.GetHandle() != w.tasks[0].handle {
			return fmt.Errorf("the record names session %q, want %q", event.GetHandle(), w.tasks[0].handle)
		}
		if event.GetSession() != w.tasks[0].sessionID {
			return fmt.Errorf("the record names session %q, want %q", event.GetSession(), w.tasks[0].sessionID)
		}
		if event.GetOccurredAt() == nil {
			return fmt.Errorf("the record does not say when the task happened")
		}
		return nil
	})

	sc.Step(`^the published task failed and says why$`, func(ctx context.Context) error {
		event, err := onlyTaskOn(worldFrom(ctx), "acme")
		if err != nil {
			return err
		}
		if event.GetStatus() != "failed" {
			return fmt.Errorf("the record says the session is %q, want failed", event.GetStatus())
		}
		if event.GetFailure() == "" {
			return fmt.Errorf("the record does not say what went wrong")
		}
		if event.GetReply() != "" {
			return fmt.Errorf("the record carries a reply %q for a task that failed", event.GetReply())
		}
		return nil
	})

	sc.Step(`^nothing on the published task says "([^"]*)"$`, func(ctx context.Context, secret string) error {
		event, err := onlyTaskOn(worldFrom(ctx), "acme")
		if err != nil {
			return err
		}
		// The whole record rather than the fields this step happens to know about, so a field added
		// later cannot quietly start carrying the value.
		if rendered := prototext.Format(event); strings.Contains(rendered, secret) {
			return fmt.Errorf("the published task carries %q: %s", secret, rendered)
		}
		return nil
	})

	sc.Step(`^the published task names "([^"]*)" as redacted$`, func(ctx context.Context, name string) error {
		event, err := onlyTaskOn(worldFrom(ctx), "acme")
		if err != nil {
			return err
		}
		if !strings.Contains(event.GetPrompt(), "<redacted "+name+">") {
			return fmt.Errorf("the published prompt %q does not name %s as redacted", event.GetPrompt(), name)
		}
		return nil
	})

	sc.Step(`^the session's history carries no "([^"]*)"$`, func(ctx context.Context, secret string) error {
		w := worldFrom(ctx)
		last, err := w.lastTask()
		if err != nil {
			return err
		}
		tasks, err := listTasks(ctx, w, last.sessionID)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			return fmt.Errorf("the session has no history, so this proves nothing")
		}
		for _, task := range tasks {
			if rendered := prototext.Format(task); strings.Contains(rendered, secret) {
				return fmt.Errorf("the history carries %q: %s", secret, rendered)
			}
		}
		return nil
	})

	sc.Step(`^the tasks on the log are in the order they were asked$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		events, err := tasksOn(w, "acme")
		if err != nil {
			return err
		}
		if len(events) != len(w.tasks) {
			return fmt.Errorf("%d tasks are on the log and %d tasks ran", len(events), len(w.tasks))
		}
		for i, event := range events {
			if event.GetSession() != w.tasks[i].sessionID {
				return fmt.Errorf("record %d names session %q, want %q", i, event.GetSession(), w.tasks[i].sessionID)
			}
			if event.GetReply() != w.tasks[i].reply {
				return fmt.Errorf("record %d carries reply %q, want %q", i, event.GetReply(), w.tasks[i].reply)
			}
		}
		return nil
	})

	sc.Step(`^the system reports that nothing is connected to the event log$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.GetInfo(ctx, &quaycrewv1.GetInfoRequest{})
		if err != nil {
			return err
		}
		if resp.GetEvents() != "" {
			return fmt.Errorf("the system reports the events engine as %q, want nothing", resp.GetEvents())
		}
		return nil
	})
}
