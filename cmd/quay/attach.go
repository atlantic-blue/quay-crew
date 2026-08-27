package main

import (
	"context"
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
func runAttach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay attach <session>\n\na session is its id, its handle, or its address")
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	spec, err := client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sessionID})
	if err != nil {
		return err
	}

	command, err := attachCommand(spec)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "attaching to %s\n", display.ShortID(sessionID))

	command.Stdin, command.Stdout, command.Stderr = os.Stdin, out, os.Stderr
	return command.Run()
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
