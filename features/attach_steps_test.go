package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/cucumber/godog"
)

type attachWorld struct {
	spec *quaycrewv1.AttachSessionResponse
	err  error
	// named is the conversation each attach was told to open, in order, so a scenario can say that
	// opening twice lands in one conversation rather than orphaning the first.
	named []string
}

type attachKey struct{}

func attachFrom(ctx context.Context) *attachWorld {
	a, _ := ctx.Value(attachKey{}).(*attachWorld)
	return a
}

// initializeAttachSteps registers the steps for reaching a session's conversation.
func initializeAttachSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, attachKey{}, &attachWorld{}), nil
	})

	sc.Step(`^the operator asks how to attach to the session$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		a.spec, a.err = w.client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: current.sessionID})
		return nil
	})

	sc.Step(`^the operator asks how to attach to the driver$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		if len(w.drivers) == 0 {
			return fmt.Errorf("no driver was opened")
		}
		a.spec, a.err = w.client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: w.drivers[0].GetId()})
		a.named = append(a.named, conversationIn(a.spec))
		return a.err
	})

	// The system's name for the conversation, read from the row rather than from the command, so this
	// says the system knows it rather than only that it passed something down.
	sc.Step(`^the driver has a conversation the system can name$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: w.drivers[0].GetId()})
		if err != nil {
			return err
		}
		if resp.GetSession().GetModelSessionId() == "" {
			return fmt.Errorf("the driver's conversation has no name, so nothing can be attributed to it")
		}
		return nil
	})

	// The name the system holds is the name the sandbox is told, or the system is naming something nobody
	// uses and the transcript on disk still belongs to nobody.
	sc.Step(`^the command opens the conversation the system holds$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		id := ""
		if len(w.drivers) > 0 {
			id = w.drivers[0].GetId()
		} else {
			current, err := w.lastTask()
			if err != nil {
				return err
			}
			id = current.sessionID
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: id})
		if err != nil {
			return err
		}
		held := resp.GetSession().GetModelSessionId()
		if held == "" {
			return fmt.Errorf("the system holds no conversation for this session")
		}
		if got := conversationIn(a.spec); got != held {
			return fmt.Errorf("the sandbox is told to open %q and the system holds %q", got, held)
		}
		return nil
	})

	sc.Step(`^the driver has the same conversation both times$`, func(ctx context.Context) error {
		a := attachFrom(ctx)
		if len(a.named) != 2 {
			return fmt.Errorf("the driver was opened %d times, want 2", len(a.named))
		}
		if a.named[0] == "" {
			return fmt.Errorf("the first open named no conversation at all")
		}
		if a.named[0] != a.named[1] {
			return fmt.Errorf("opening twice gave %q then %q, so the history from the first is orphaned",
				a.named[0], a.named[1])
		}
		return nil
	})

	// While the task runs, not after it. Read off the task itself rather than off the session, so this
	// says the operator lands where the job is happening rather than only that two of the system's own
	// fields agree with each other.
	sc.Step(`^the command opens the conversation the task is running in$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		running, err := w.conversationOfFirstTask()
		if err != nil {
			return err
		}
		if got := conversationIn(a.spec); got != running {
			return fmt.Errorf("the command opens conversation %q and the task is running in %q, "+
				"so the operator is watching an empty conversation beside the job", got, running)
		}
		return nil
	})

	// The name came from the system, before the task, which is what makes it knowable while the task
	// runs. A task that resumed instead would be asking for a conversation nothing has written yet.
	sc.Step(`^the system named the conversation before the task started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		first, found := w.runner.task(0)
		if !found {
			return fmt.Errorf("no task has reached the model runner")
		}
		if first.ModelSessionID == "" {
			return fmt.Errorf("the task is running in a conversation nobody named")
		}
		if first.ConversationStarted {
			return fmt.Errorf("the task resumed conversation %q, which nothing has written yet: "+
				"the model exits saying no conversation was found", first.ModelSessionID)
		}
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		if got := resp.GetSession().GetModelSessionId(); got != first.ModelSessionID {
			return fmt.Errorf("the session holds conversation %q while its task runs in %q", got, first.ModelSessionID)
		}
		return nil
	})

	sc.Step(`^the operator asks how to attach to a session that has never had a task$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		// A session exists only once a task creates it, so a failing runner gives us one with no
		// conversation behind it, which is exactly the state this refusal is about.
		w.runner.failNext = true
		_ = w.dispatch(ctx, w.projectID, "", "this task fails")
		sessions, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if len(sessions.GetSessions()) != 1 {
			return fmt.Errorf("expected one session with no conversation, got %d", len(sessions.GetSessions()))
		}
		only := sessions.GetSessions()[0]
		// Recorded as the session in play, so the steps that name the sandbox and read the system's
		// conversation back can be asked about it. The dispatch failed, so nothing else recorded it.
		w.tasks = append(w.tasks, task{sessionID: only.GetId(), handle: only.GetHandle()})
		a.spec, a.err = w.client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: only.GetId()})
		return nil
	})

	sc.Step(`^the control plane names the session's sandbox$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		if got, want := a.spec.GetSandbox(), sandbox.ContainerName(current.sessionID); got != want {
			return fmt.Errorf("the sandbox is %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the command resumes the conversation the task started$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		// The conversation the task itself ran in, read off the task rather than off the session, so
		// this says the operator lands where the job happened rather than only that two of the system's
		// own fields agree.
		ran, err := w.conversationOfFirstTask()
		if err != nil {
			return err
		}
		if got := conversationIn(a.spec); got != ran {
			return fmt.Errorf("the command opens conversation %q and the task ran in %q", got, ran)
		}
		return nil
	})

	// Ending the conversation was the only way back to the console, so an open session runs inside a
	// terminal multiplexer in its sandbox: detaching leaves the model running and returns the
	// operator to the list. Opening it again attaches to the one already there rather than starting a
	// second beside it, which is what -A is for.
	sc.Step(`^the command runs it inside a terminal the operator can leave$`, func(ctx context.Context) error {
		a := attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		line := strings.Join(a.spec.GetArgv(), " ")
		for _, want := range []string{
			"tmux new-session -A -s " + sandbox.AttachedSessionName,
			// Not claude directly: ending a conversation used to take the whole terminal with it.
			sandbox.OpenConversation,
		} {
			if !strings.Contains(line, want) {
				return fmt.Errorf("the command is %q, want it to carry %q", line, want)
			}
		}
		return nil
	})

	// An attached session that ignored the session's mode would stop and ask the moment it was opened,
	// on a session the operator had deliberately armed, which reads as the toggle not working.
	sc.Step(`^the command runs in permission mode "([^"]*)"$`, func(ctx context.Context, want string) error {
		a := attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		line := strings.Join(a.spec.GetArgv(), " ")
		// open-conversation takes the mode as its second argument and passes it to the model, which is
		// what keeps the whole command one readable line.
		if !strings.HasSuffix(line, " "+want) {
			return fmt.Errorf("the command is %q, want it to run as %q", line, want)
		}
		return nil
	})

	sc.Step(`^the answer carries no credential$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		// Whatever the workspace's token is, it must not appear anywhere in the answer: a secret the
		// backend holds should not become readable through the API.
		token, err := w.secrets.Get(ctx, w.workspaceID, "CLAUDE_CODE_OAUTH_TOKEN")
		if err == nil && token != "" {
			if strings.Contains(a.spec.String(), token) {
				return fmt.Errorf("the answer leaks the subscription token")
			}
		}
		if strings.Contains(a.spec.String(), "OAUTH") {
			return fmt.Errorf("the answer mentions a credential: %s", a.spec.String())
		}
		return nil
	})

	// Closing the sandbox without going through StopSession is what an upgrade, a prune or anything
	// else that removes a container does: the container goes, the control plane is not told, and its
	// own handle carries on claiming the container is there.
	sc.Step(`^the session's sandbox is removed without telling the control plane$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Boxes) == 0 {
			return fmt.Errorf("no sandbox has been made yet")
		}
		return w.provider.Boxes[len(w.provider.Boxes)-1].Close(ctx)
	})

	// The model keeps its conversations on the host now, so losing one is deleting the file, which is
	// exactly what happened to every conversation from a sandbox built before those mounts existed.
	sc.Step(`^the conversation the model kept is lost$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		session := resp.GetSession()
		path := filepath.Join(w.conversationDir(session.GetWorkspace()),
			session.GetModelSessionId()+sandbox.ConversationFile)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("losing the conversation: %w", err)
		}
		return nil
	})

	sc.Step(`^the refusal says the conversation is gone, in the operator.s words$`, func(ctx context.Context) error {
		a := attachFrom(ctx)
		if a.err == nil {
			return fmt.Errorf("attaching was allowed, expected a refusal")
		}
		// A refusal that only says no leaves the operator staring at a session they cannot open, and one
		// written in the system's own vocabulary leaves them asking what it means. It says what to do
		// instead, in words that appear on their screen.
		for _, want := range []string{"no conversation left", "quay task"} {
			if !strings.Contains(a.err.Error(), want) {
				return fmt.Errorf("the refusal is %q, want it to say %q", a.err.Error(), want)
			}
		}
		return nil
	})
}

// initializeSandboxEnvSteps covers what a session's sandbox is created with.
func initializeSandboxEnvSteps(sc *godog.ScenarioContext) {
	sc.Step(`^every sandbox was created for the same workspace and project$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Created) < 2 {
			return fmt.Errorf("%d sandboxes were created, want at least 2 to compare", len(w.provider.Created))
		}
		first := w.provider.Created[0]
		for _, created := range w.provider.Created[1:] {
			if created.Workspace != first.Workspace || created.Project != first.Project {
				return fmt.Errorf("one sandbox was created for %s/%s and another for %s/%s",
					first.Workspace, first.Project, created.Workspace, created.Project)
			}
		}
		return nil
	})

	sc.Step(`^the sandboxes were created for one workspace but different projects$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Created) != 2 {
			return fmt.Errorf("%d sandboxes were created, want 2", len(w.provider.Created))
		}
		first, second := w.provider.Created[0], w.provider.Created[1]
		if first.Workspace != second.Workspace {
			return fmt.Errorf("they were created for workspaces %q and %q, want one", first.Workspace, second.Workspace)
		}
		if first.Project == second.Project {
			return fmt.Errorf("both were created for project %q, want the two projects kept apart", first.Project)
		}
		return nil
	})

	sc.Step(`^the session's sandbox was created with the subscription token "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			w := worldFrom(ctx)
			if len(w.provider.Created) != 1 {
				return fmt.Errorf("%d sandboxes were created, want 1", len(w.provider.Created))
			}
			entry := "CLAUDE_CODE_OAUTH_TOKEN=" + want
			created := w.provider.Created[0]
			for _, got := range created.Env {
				if got == entry {
					return nil
				}
			}
			return fmt.Errorf("the sandbox was created with %v, want it to carry %s", created.Env, entry)
		})

	// A session is told which session it is whatever else the workspace holds, so this is what a
	// sandbox with no credential on it carries: that one line and nothing else.
	sc.Step(`^the session's sandbox was created with nothing but its own identifier$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Created) != 1 {
			return fmt.Errorf("%d sandboxes were created, want 1", len(w.provider.Created))
		}
		created := w.provider.Created[0]
		want := sandbox.SessionIDEnv + "=" + created.ID
		if len(created.Env) != 1 || created.Env[0] != want {
			return fmt.Errorf("the sandbox was created with %v, want only %s", created.Env, want)
		}
		return nil
	})
}

// conversationIn is the conversation the sandbox is told to open, which is the argument after the
// command that opens one.
func conversationIn(spec *quaycrewv1.AttachSessionResponse) string {
	argv := spec.GetArgv()
	for index, arg := range argv {
		if arg == sandbox.OpenConversation && index+1 < len(argv) {
			return argv[index+1]
		}
	}
	return ""
}
