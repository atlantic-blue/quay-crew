package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

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

// resolveSession tasks what the operator typed into a session id.
//
// A listing prints two identifiers for every session, the id and the handle, and dispatch takes an
// address on top of those. All three are on the operator's screen, so all three get typed back, and
// each one has to reach the session. Until this took more than the id, the identifier in the session
// column was refused by every command that reads it, which is most of them.
//
// Identifiers are printed shortened, so a prefix counts.
func resolveSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	typed := strings.TrimSpace(reference)
	if typed == "" {
		return "", fmt.Errorf("a session is required: its id, its handle, or its address")
	}
	resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return "", err
	}
	if strings.Contains(typed, workspace.Separator) {
		return sessionAtAddress(ctx, client, typed, resp.GetSessions())
	}
	return sessionWithIdentifier(typed, resp.GetSessions())
}

// sessionAtAddress reads an address the way dispatch does, then tasks the handle it lands on into the
// session id every other command works in.
func sessionAtAddress(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string, sessions []*quaycrewv1.Session) (string, error) {
	path, err := workspace.ParsePath(typed)
	if err != nil {
		return "", err
	}
	if path.Session == "" {
		return "", fmt.Errorf("%q names a project, not a session: add the session from the listing, for example %s/3cb04bf5",
			typed, typed)
	}
	located, err := workspace.ResolvePath(ctx, client, path)
	if err != nil {
		return "", err
	}
	for _, session := range sessions {
		if session.GetHandle() == located.SessionID {
			return session.GetId(), nil
		}
	}
	return "", fmt.Errorf("%q resolved to a session the crew no longer lists", typed)
}

// sessionWithIdentifier matches a bare identifier against both of the ones a listing prints. An exact
// match wins outright, so a short identifier that happens to prefix another session still resolves to
// itself.
func sessionWithIdentifier(typed string, sessions []*quaycrewv1.Session) (string, error) {
	matches := make([]string, 0, 1)
	for _, session := range sessions {
		if session.GetId() == typed || session.GetHandle() == typed {
			return session.GetId(), nil
		}
		if strings.HasPrefix(session.GetId(), typed) || strings.HasPrefix(session.GetHandle(), typed) {
			matches = append(matches, session.GetId())
		}
	}

	switch len(matches) {
	case 0:
		return "", &workspace.NotFoundError{
			What: "session", Name: typed,
			Have: identifiersOf(sessions),
			Make: `start one with quay dispatch "..."`,
		}
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", &workspace.AmbiguousError{What: "sessions", Name: typed, IDs: matches}
	}
}

// identifiersOf is every session the crew holds, written as the listing writes it: the id, then the
// handle beside it. Both, because the operator was refused for typing one of them and has no way to
// tell from the refusal which one the command wanted.
func identifiersOf(sessions []*quaycrewv1.Session) []string {
	have := make([]string, 0, len(sessions))
	for _, session := range sessions {
		have = append(have, fmt.Sprintf("%s (session %s)",
			display.ShortID(session.GetId()), display.ShortID(session.GetHandle())))
	}
	sort.Strings(have)
	return have
}
