package main

import (
	"bufio"
	"context"
	"errors"
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

// resolveSession turns what the operator typed into a session id.
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
	live, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return "", err
	}
	id, refused := sessionIn(ctx, client, typed, live.GetSessions())
	if refused == nil {
		return id, nil
	}
	// The archived listing second. A flow run puts its own session away when it finishes, and that
	// session's history is the first thing somebody investigating the run asks for, so which listing
	// this happens to read must not decide whether a session can be named at all. Nothing about
	// reading a history needs the session to be live, and a command that does need it live refuses
	// on its own terms: attach says to restore it first.
	//
	// Asked for second rather than merged in, so an identifier that names a live session today names
	// the same one tomorrow.
	putAway, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Archived: true})
	if err != nil {
		return "", refused
	}
	if found, missing := sessionIn(ctx, client, typed, putAway.GetSessions()); missing == nil {
		return found, nil
	}
	return "", refused
}

// sessionIn reads what the operator typed against one listing: an address, or one of the two
// identifiers that listing prints.
func sessionIn(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string, sessions []*quaycrewv1.Session) (string, error) {
	if strings.Contains(typed, workspace.Separator) {
		return sessionAtAddress(ctx, client, typed, sessions)
	}
	return sessionWithIdentifier(typed, sessions)
}

// sessionAtAddress reads an address the way dispatch does, then turns the handle it lands on into the
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
