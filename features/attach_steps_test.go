package features_test

import (
	"context"
	"fmt"
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
		if line != "claude --resume conversation-1" {
			return fmt.Errorf("the command is %q, want it to resume the turn's conversation", line)
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
	sc.Step(`^the session's sandbox was created with the subscription token "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			w := worldFrom(ctx)
			if len(w.provider.CreatedEnv) != 1 {
				return fmt.Errorf("%d sandboxes were created, want 1", len(w.provider.CreatedEnv))
			}
			entry := "CLAUDE_CODE_OAUTH_TOKEN=" + want
			for _, got := range w.provider.CreatedEnv[0] {
				if got == entry {
					return nil
				}
			}
			return fmt.Errorf("the sandbox was created with %v, want it to carry %s", w.provider.CreatedEnv[0], entry)
		})

	sc.Step(`^the session's sandbox was created with no environment$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.CreatedEnv) != 1 {
			return fmt.Errorf("%d sandboxes were created, want 1", len(w.provider.CreatedEnv))
		}
		if len(w.provider.CreatedEnv[0]) != 0 {
			return fmt.Errorf("the sandbox was created with %v, want nothing", w.provider.CreatedEnv[0])
		}
		return nil
	})
}
