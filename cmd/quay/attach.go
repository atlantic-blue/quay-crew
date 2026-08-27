package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

// runAttach opens a session's conversation: it asks the control plane where the conversation is, then
// hands the terminal to the model inside that session's sandbox. Shelling in shows you the room; this
// puts you in the conversation, with its history, able to keep typing.
func runAttach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string,
	out io.Writer, in io.Reader) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay attach <session>\n\na session is its id, its handle, or its address")
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return sayAndWait(out, in, err)
	}
	spec, err := client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sessionID})
	if err != nil {
		return sayAndWait(out, in, cannotOpen(sessionID, err))
	}

	command, err := attachCommand(spec)
	if err != nil {
		return sayAndWait(out, in, cannotOpen(sessionID, err))
	}
	fmt.Fprintf(out, "attaching to %s\n", display.ShortID(sessionID))

	command.Stdin, command.Stdout, command.Stderr = in, out, os.Stderr
	if err := command.Run(); err != nil {
		return sayAndWait(out, in, cannotOpen(sessionID, err))
	}
	return nil
}

// ErrSaid marks a failure the command has already put in front of the operator, so nothing prints it
// a second time. The exit status is still a failure.
var ErrSaid = errors.New("already said")

// sayAndWait puts the reason on the screen and keeps the screen there.
//
// Attach is usually the whole command of a tmux pane, beside the console or in the right half of the
// panel, and a pane closes the moment its command exits. So a refusal printed the reason and had it
// destroyed in the same instant: measured against tmux 3.3a the pane is gone before anything else can
// even list it. The operator pressed a key, the screen flickered, and nothing on it said why.
//
// The sandbox already solves this for the conversation itself, in open-conversation, which is where
// the shape comes from: say what happened, then wait rather than exit.
//
// It reads the terminal rather than asking whether there is one. Nothing is attached to a pipeline's
// standard input, so a scripted attach reads the end of it and returns at once instead of hanging.
func sayAndWait(out io.Writer, in io.Reader, reason error) error {
	fmt.Fprintf(out, "\n  %s\n\n  Press enter to go back.\n", reason)
	_, _ = bufio.NewReader(in).ReadString('\n')
	return fmt.Errorf("%w: %w", ErrSaid, reason)
}

// cannotOpen says why a conversation did not open, and what to do instead.
//
// A few short lines, because it is read in half the width of a terminal. A reason nobody can act on
// is worth no more than no reason at all: the operator is looking at a session they cannot reach, and
// whatever they do next is a guess.
func cannotOpen(sessionID string, err error) error {
	where := display.ShortID(sessionID)
	var missing *exec.Error
	if errors.As(err, &missing) && errors.Is(missing.Err, exec.ErrNotFound) {
		return fmt.Errorf("cannot open the conversation in %s: it runs in that session's own "+
			"container, and %s is not on this machine. Open it from the machine the crew runs on",
			where, missing.Name)
	}
	return fmt.Errorf("cannot open the conversation in %s: %w. Check the crew is up with "+
		"quay sessions, then open it again", where, err)
}

// attachCommand builds the command that opens the conversation.
//
// It carries no credential. The session's sandbox already holds the workspace's environment, set when
// the sandbox was created, so everything started inside it is authenticated without this tool ever
// handling a token.
func attachCommand(spec *quaycrewv1.AttachSessionResponse) (*exec.Cmd, error) {
	if spec.GetSandbox() == "" || len(spec.GetArgv()) == 0 {
		return nil, fmt.Errorf("the control plane did not say how to attach")
	}
	args := []string{"exec", "--interactive", "--tty", spec.GetSandbox()}
	args = append(args, spec.GetArgv()...)
	return exec.Command("docker", args...), nil
}

// resolveSession turns what the operator typed into a session id, which is what every command here
// works in. Reading it is the crew's job, not this tool's, so it goes through the one resolver.
func resolveSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	session, err := workspace.Session(ctx, client, reference)
	if err != nil {
		return "", err
	}
	return session.GetId(), nil
}
