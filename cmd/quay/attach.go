package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
)

// runAttach opens a session's conversation: it asks the control plane where the conversation is, then
// hands the terminal to the model inside that session's sandbox. Shelling in shows you the room; this
// puts you in the conversation, with its history, able to keep typing.
func runAttach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay attach <session id>")
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	spec, err := client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sessionID})
	if err != nil {
		return err
	}

	command, err := attachCommand(spec, os.Getenv(model.ClaudeCodeOAuthTokenEnv))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "attaching to %s\n", display.ShortID(sessionID))

	command.Stdin, command.Stdout, command.Stderr = os.Stdin, out, os.Stderr
	return command.Run()
}

// attachCommand builds the command that opens the conversation.
//
// The control plane deliberately returns no credential, so the token comes from here. Without one the
// model would start and fail on its first call with an authentication error, which reads like a
// broken tool rather than a missing setting, so refuse early and say what to do.
func attachCommand(spec *quaycrewv1.AttachSessionResponse, token string) (*exec.Cmd, error) {
	if spec.GetSandbox() == "" || len(spec.GetArgv()) == 0 {
		return nil, fmt.Errorf("the control plane did not say how to attach")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf(
			"attaching needs %s in your environment (the same token you set as the workspace secret)",
			model.ClaudeCodeOAuthTokenEnv)
	}

	args := []string{"exec", "--interactive", "--tty",
		"--env", model.ClaudeCodeOAuthTokenEnv + "=" + token,
		spec.GetSandbox()}
	args = append(args, spec.GetArgv()...)
	return exec.Command("docker", args...), nil
}

// resolveSession turns what the operator typed into a session id. Listings print identifiers
// shortened, so the thing on their screen is a prefix, and typing it back has to work.
func resolveSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	if strings.TrimSpace(reference) == "" {
		return "", fmt.Errorf("a session id is required")
	}
	resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return "", err
	}

	matches := make([]string, 0, 1)
	for _, session := range resp.GetSessions() {
		if session.GetId() == reference {
			return reference, nil
		}
		if strings.HasPrefix(session.GetId(), reference) {
			matches = append(matches, session.GetId())
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no session with id or prefix %q", reference)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%q matches %d sessions, type more of the id", reference, len(matches))
	}
}
