package features_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// Steps for reading an answer back out of the crew as data.
//
// These run the real command line tool in its own process, because what is specified is which stream
// each thing goes to and what the exit status is, and neither of those exists inside the test process.
// Every other scenario dials the crew over an in memory connection, which a second process cannot
// reach, so the scenario asks the crew for a network address of its own first.

type answerKey struct{}

// answerWorld is what one scenario asked for and what came back out of the tool.
type answerWorld struct {
	address   string
	sessionID string
	handle    string
	// replies are what the model said, oldest first, so an assertion names the value rather than
	// repeating the double's arithmetic.
	replies  []string
	stdout   string
	stderr   string
	exitCode int
	ran      bool
}

func answerFrom(ctx context.Context) *answerWorld {
	a, _ := ctx.Value(answerKey{}).(*answerWorld)
	return a
}

func initializeAnswerSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, answerKey{}, &answerWorld{}), nil
	})

	sc.Step(`^the crew listens on an address the tool can dial$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), answerFrom(ctx)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("listen for the tool: %w", err)
		}
		go func() { _ = w.grpcServer.Serve(listener) }()
		a.address = listener.Addr().String()
		return nil
	})

	sc.Step(`^a session that was asked "([^"]*)"$`, func(ctx context.Context, text string) error {
		return answerDispatch(ctx, text)
	})

	sc.Step(`^a session that was asked "([^"]*)" and then "([^"]*)"$`,
		func(ctx context.Context, first, second string) error {
			if err := answerDispatch(ctx, first); err != nil {
				return err
			}
			return answerDispatch(ctx, second)
		})

	// The listing shortens at 120 characters, so the reply here has to be longer than that by enough
	// that a shortened one is unmistakable.
	sc.Step(`^a session that was asked for 400 characters$`, func(ctx context.Context) error {
		return answerDispatch(ctx, strings.Repeat("x", 400))
	})

	// A driver session is one the crew opens and nobody has dispatched to, which is the only way a
	// session exists with an empty history.
	sc.Step(`^a session that was opened and never asked anything$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), answerFrom(ctx)
		opened, err := w.client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		a.sessionID, a.handle = opened.GetSession().GetId(), opened.GetSession().GetHandle()
		tasks, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: a.sessionID})
		if err != nil {
			return err
		}
		if len(tasks.GetTasks()) != 0 {
			return fmt.Errorf("the opened session already has %d tasks, so it proves nothing about an empty history",
				len(tasks.GetTasks()))
		}
		return nil
	})

	sc.Step(`^a session whose task failed$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), answerFrom(ctx)
		w.runner.failNext = true
		_, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "read the repository",
		})
		if err == nil {
			return fmt.Errorf("the dispatch was expected to fail and did not")
		}
		// A failed dispatch answers with no session, so the session is found through the listing.
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 1 {
			return fmt.Errorf("the crew has %d sessions, want exactly one", len(listed.GetSessions()))
		}
		a.sessionID = listed.GetSessions()[0].GetId()
		a.handle = listed.GetSessions()[0].GetHandle()
		return nil
	})

	sc.Step(`^the caller asks for the answer of that session$`, func(ctx context.Context) error {
		session, err := answerSubject(ctx)
		if err != nil {
			return err
		}
		return answerRun(ctx, "answer", session)
	})

	sc.Step(`^the caller asks for the answer by the handle of that session$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if a.handle == "" {
			return fmt.Errorf("this scenario has no session to name by its handle")
		}
		return answerRun(ctx, "answer", a.handle)
	})

	sc.Step(`^the caller asks for every answer of that session$`, func(ctx context.Context) error {
		session, err := answerSubject(ctx)
		if err != nil {
			return err
		}
		return answerRun(ctx, "answer", session, "--all")
	})

	sc.Step(`^standard output is the reply and one newline$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if len(a.replies) == 0 {
			return fmt.Errorf("this scenario recorded no reply, so there is nothing to compare against")
		}
		want := a.replies[len(a.replies)-1] + "\n"
		if a.stdout != want {
			return fmt.Errorf("standard output is %q, want %q", a.stdout, want)
		}
		return nil
	})

	sc.Step(`^standard output is both replies, oldest first$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if len(a.replies) != 2 {
			return fmt.Errorf("this scenario recorded %d replies, want 2", len(a.replies))
		}
		want := a.replies[0] + "\n" + a.replies[1] + "\n"
		if a.stdout != want {
			return fmt.Errorf("standard output is %q, want %q", a.stdout, want)
		}
		return nil
	})

	sc.Step(`^standard output carries all 400 characters$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if !strings.Contains(a.stdout, strings.Repeat("x", 400)) {
			return fmt.Errorf("standard output is %d characters and does not carry the answer whole: %q",
				len(a.stdout), a.stdout)
		}
		return nil
	})

	sc.Step(`^standard output carries what went wrong$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if !strings.Contains(a.stdout, "the model refused this task") {
			return fmt.Errorf("standard output is %q, want what the task failed with", a.stdout)
		}
		return nil
	})

	sc.Step(`^standard output is empty$`, func(ctx context.Context) error {
		if got := answerFrom(ctx).stdout; got != "" {
			return fmt.Errorf("standard output carries %q, and a caller reading it would take that for the answer", got)
		}
		return nil
	})

	sc.Step(`^standard error says nothing$`, func(ctx context.Context) error {
		if got := answerFrom(ctx).stderr; got != "" {
			return fmt.Errorf("standard error says %q", got)
		}
		return nil
	})

	sc.Step(`^standard error says there is no landed task$`, func(ctx context.Context) error {
		return answerStderrSays(ctx, "no landed task")
	})

	sc.Step(`^standard error says the task is still running$`, func(ctx context.Context) error {
		return answerStderrSays(ctx, "still running")
	})

	sc.Step(`^the command succeeds$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if !a.ran {
			return fmt.Errorf("the command was never run")
		}
		if a.exitCode != 0 {
			return fmt.Errorf("the command exited %d, saying %q", a.exitCode, a.stderr)
		}
		return nil
	})

	sc.Step(`^the command fails$`, func(ctx context.Context) error {
		a := answerFrom(ctx)
		if !a.ran {
			return fmt.Errorf("the command was never run")
		}
		if a.exitCode == 0 {
			return fmt.Errorf("the command succeeded, so a caller cannot tell the answer is missing")
		}
		return nil
	})
}

// answerDispatch runs one task and keeps what the model said, so an assertion names the reply rather
// than repeating how the double builds one.
func answerDispatch(ctx context.Context, text string) error {
	w, a := worldFrom(ctx), answerFrom(ctx)
	resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: w.projectID, Handle: a.handle, Text: text,
	})
	if err != nil {
		return err
	}
	a.sessionID, a.handle = resp.GetId(), resp.GetHandle()
	a.replies = append(a.replies, resp.GetReply())
	return nil
}

// answerSubject is the session the scenario is about: the one its own steps made, or the one a
// dispatch step shared with the rest of the suite left behind.
func answerSubject(ctx context.Context) (string, error) {
	if a := answerFrom(ctx); a.sessionID != "" {
		return a.sessionID, nil
	}
	last, err := worldFrom(ctx).lastTask()
	if err != nil {
		return "", err
	}
	return last.sessionID, nil
}

func answerStderrSays(ctx context.Context, want string) error {
	a := answerFrom(ctx)
	if !strings.Contains(a.stderr, want) {
		return fmt.Errorf("standard error says %q, want it to say %q", a.stderr, want)
	}
	return nil
}

// answerRun runs the tool the way a caller runs it, with its streams kept apart.
//
// The home directory is a temporary one: this tool keeps where the operator is standing on the
// machine it runs on, and a scenario must not read or write the operator's own.
func answerRun(ctx context.Context, args ...string) error {
	a := answerFrom(ctx)
	if a.address == "" {
		return fmt.Errorf("the crew has no address the tool can dial")
	}
	binary, err := quayBinary()
	if err != nil {
		return err
	}
	home, err := os.MkdirTemp("", "quaycrew-answer-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(home) }()

	command := exec.CommandContext(ctx, binary, args...)
	command.Env = append(os.Environ(),
		"QC_GRPC_ADDR="+a.address,
		"QC_TOKEN="+worldFrom(ctx).token,
		"QUAY_HOME="+home,
		"HOME="+home,
	)
	var out, said bytes.Buffer
	command.Stdout, command.Stderr = &out, &said
	runErr := command.Run()
	a.ran, a.stdout, a.stderr = true, out.String(), said.String()

	var exit *exec.ExitError
	switch {
	case runErr == nil:
		a.exitCode = 0
	case errors.As(runErr, &exit):
		a.exitCode = exit.ExitCode()
	default:
		return fmt.Errorf("running the tool: %w", runErr)
	}
	return nil
}
