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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type attachWorld struct {
	spec *quaycrewv1.AttachSessionResponse
	err  error
}

type attachKey struct{}

func attachFrom(ctx context.Context) *attachWorld {
	a, _ := ctx.Value(attachKey{}).(*attachWorld)
	return a
}

// initializeAttachSteps registers the steps for reaching a thread's conversation.
func initializeAttachSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, attachKey{}, &attachWorld{}), nil
	})

	sc.Step(`^the operator asks how to attach to the session$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		a.spec, a.err = w.client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: current.sessionID})
		return nil
	})

	sc.Step(`^the operator asks how to attach to a session that has never had a turn$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		// A session exists only once a turn creates it, so a failing runner gives us one with no
		// conversation behind it, which is exactly the state this refusal is about.
		w.runner.failNext = true
		_ = w.dispatch(ctx, w.projectID, "", "this turn fails")
		sessions, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if len(sessions.GetSessions()) != 1 {
			return fmt.Errorf("expected one session with no conversation, got %d", len(sessions.GetSessions()))
		}
		a.spec, a.err = w.client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sessions.GetSessions()[0].GetId()})
		return nil
	})

	sc.Step(`^the control plane names the session's sandbox$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		if got, want := a.spec.GetSandbox(), sandbox.ContainerName(current.sessionID); got != want {
			return fmt.Errorf("the sandbox is %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the command resumes the conversation the turn started$`, func(ctx context.Context) error {
		a := attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		line := strings.Join(a.spec.GetArgv(), " ")
		// conversation-1 is what the recording runner hands back for a first turn.
		if !strings.HasPrefix(line, "claude --resume conversation-1") {
			return fmt.Errorf("the command is %q, want it to resume the turn's conversation", line)
		}
		return nil
	})

	// An attached session that ignored the thread's mode would stop and ask the moment it was opened,
	// on a thread the operator had deliberately armed, which reads as the toggle not working.
	sc.Step(`^the command runs in permission mode "([^"]*)"$`, func(ctx context.Context, want string) error {
		a := attachFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("attaching was refused: %w", a.err)
		}
		line := strings.Join(a.spec.GetArgv(), " ")
		if !strings.Contains(line, "--permission-mode "+want) {
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

	// The model keeps its conversations on the host now, so losing one is deleting the file, which is
	// exactly what happened to every conversation from a sandbox built before those mounts existed.
	sc.Step(`^the conversation the model kept is lost$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
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
		// A refusal that only says no leaves the operator staring at a thread they cannot open, and one
		// written in our words leaves them asking what it means. Julian, reading the first version of
		// this sentence: "it predates state?"
		for _, want := range []string{"no conversation left", "quay dispatch"} {
			if !strings.Contains(a.err.Error(), want) {
				return fmt.Errorf("the refusal is %q, want it to say %q", a.err.Error(), want)
			}
		}
		return nil
	})

	sc.Step(`^the control plane refuses it as not yet ready$`, func(ctx context.Context) error {
		a := attachFrom(ctx)
		if a.err == nil {
			return fmt.Errorf("attaching was allowed, expected a refusal")
		}
		if got := status.Code(a.err); got != codes.FailedPrecondition {
			return fmt.Errorf("refused as %s, want failed precondition", got)
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

	sc.Step(`^the session's sandbox was created with no environment$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Created) != 1 {
			return fmt.Errorf("%d sandboxes were created, want 1", len(w.provider.Created))
		}
		if len(w.provider.Created[0].Env) != 0 {
			return fmt.Errorf("the sandbox was created with %v, want nothing", w.provider.Created[0].Env)
		}
		return nil
	})
}
