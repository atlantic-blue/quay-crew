package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for the one word that sends a task, and for the three words it replaced.
//
// They run the real tool through the harness in tool_steps_test.go, because what is specified here
// is what a caller receives: which stream a thing went to, and what the exit status was. A refusal
// that exits zero is the failure this specification exists to catch, and neither the stream nor the
// status exists inside the test process.

type taskWordKey struct{}

// taskWordWorld is what one scenario sent, and to which session.
type taskWordWorld struct {
	sessionID string
	handle    string
	reply     string
	// message is the text the scenario tried to send, so a step can say it never reached the model.
	message string
}

func taskWordFrom(ctx context.Context) *taskWordWorld {
	t, _ := ctx.Value(taskWordKey{}).(*taskWordWorld)
	return t
}

func initializeTaskWordSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, taskWordKey{}, &taskWordWorld{}), nil
	})

	// One step for every shape, so the specification names the words a person actually types and a
	// word that is gone is driven exactly the way a person's fingers would drive it.
	sc.Step(`^the caller types "([^"]*)" against the project with "([^"]*)"$`,
		func(ctx context.Context, typed, text string) error {
			taskWordFrom(ctx).message = text
			args := append(strings.Fields(typed), whereTheProjectIs(ctx), text)
			return runTool(ctx, args...)
		})

	sc.Step(`^a session that was sent "([^"]*)"$`, func(ctx context.Context, text string) error {
		w, t := worldFrom(ctx), taskWordFrom(ctx)
		resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: w.projectID, Text: text})
		if err != nil {
			return err
		}
		t.sessionID, t.handle, t.reply = resp.GetId(), resp.GetHandle(), resp.GetReply()
		return nil
	})

	sc.Step(`^the caller reads that session's tasks back$`, func(ctx context.Context) error {
		t := taskWordFrom(ctx)
		if t.handle == "" {
			return fmt.Errorf("this scenario sent nothing, so there is no session to read back")
		}
		return runTool(ctx, "task", "list", t.handle)
	})

	sc.Step(`^the caller names that session with nothing to say$`, func(ctx context.Context) error {
		t := taskWordFrom(ctx)
		if t.handle == "" {
			return fmt.Errorf("this scenario sent nothing, so there is no session to name")
		}
		return runTool(ctx, "task", t.handle[:8])
	})

	sc.Step(`^standard output carries the reply$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, "you said:")
	})

	sc.Step(`^standard output carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("standard output", toolFrom(ctx).stdout, want)
	})

	sc.Step(`^standard output says the system has it$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, "the system has it")
	})

	sc.Step(`^standard output says to read it back with "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			return says("standard output", toolFrom(ctx).stdout, want)
		})

	sc.Step(`^standard error says "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("standard error", toolFrom(ctx).stderr, want)
	})

	// The one that matters most, and the one a test for the replacement never covers: a removed word
	// whose value is absorbed into the next argument reads as a command that worked.
	sc.Step(`^standard error does not carry the message$`, func(ctx context.Context) error {
		t := taskWordFrom(ctx)
		if t.message == "" {
			return fmt.Errorf("this scenario sent no message, so there is nothing to look for")
		}
		if strings.Contains(toolFrom(ctx).stderr, t.message) {
			return fmt.Errorf("the refusal took the message with it: %q", toolFrom(ctx).stderr)
		}
		return nil
	})

	sc.Step(`^no session was started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 0 {
			return fmt.Errorf("%d sessions exist, so a refused word started work anyway",
				len(listed.GetSessions()))
		}
		return nil
	})

	sc.Step(`^that session was sent nothing more$`, func(ctx context.Context) error {
		w, t := worldFrom(ctx), taskWordFrom(ctx)
		tasks, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: t.sessionID})
		if err != nil {
			return err
		}
		if len(tasks.GetTasks()) != 1 {
			return fmt.Errorf("the session holds %d tasks, want the one it was sent",
				len(tasks.GetTasks()))
		}
		// The identifier reaching the model as a message is the whole defect, so look for it by name.
		if prompt := tasks.GetTasks()[0].GetPrompt(); strings.Contains(prompt, t.handle[:8]) {
			return fmt.Errorf("the session's identifier was sent as a message: %q", prompt)
		}
		return nil
	})
}

// whereTheProjectIs is the address a scenario types, built from the names it made rather than from
// identifiers, because that is what an operator has in front of them.
func whereTheProjectIs(ctx context.Context) string {
	w := worldFrom(ctx)
	return w.workspaceName + "/" + w.projectName
}
