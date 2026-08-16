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
		return fmt.Errorf("usage: quay attach <thread>\n\na thread is its id, its handle, or its address")
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	spec, err := client.AttachThread(ctx, &quaycrewv1.AttachThreadRequest{Id: sessionID})
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
func attachCommand(spec *quaycrewv1.AttachThreadResponse) (*exec.Cmd, error) {
	if spec.GetSandbox() == "" || len(spec.GetArgv()) == 0 {
		return nil, fmt.Errorf("the control plane did not say how to attach")
	}
	args := []string{"exec", "--interactive", "--tty", spec.GetSandbox()}
	args = append(args, spec.GetArgv()...)
	return exec.Command("docker", args...), nil
}

// resolveSession turns what the operator typed into a thread id.
//
// A listing prints two identifiers for every thread, the id and the handle, and dispatch takes an
// address on top of those. All three are on the operator's screen, so all three get typed back, and
// each one has to reach the thread. Until this took more than the id, the identifier in the thread
// column was refused by every command that reads it, which is most of them.
//
// Identifiers are printed shortened, so a prefix counts.
func resolveSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	typed := strings.TrimSpace(reference)
	if typed == "" {
		return "", fmt.Errorf("a thread is required: its id, its handle, or its address")
	}
	live, err := client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{})
	if err != nil {
		return "", err
	}
	id, refused := threadIn(ctx, client, typed, live.GetThreads())
	if refused == nil {
		return id, nil
	}
	// The archived listing second. A flow run puts its own thread away when it finishes, and that
	// thread's history is the first thing somebody investigating the run asks for, so which listing
	// this happens to read must not decide whether a thread can be named at all. Nothing about
	// reading a history needs the thread to be live, and a command that does need it live refuses on
	// its own terms: attach says to restore it first.
	//
	// Asked for second rather than merged in, so an identifier that names a live thread today names
	// the same one tomorrow.
	putAway, err := client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Archived: true})
	if err != nil {
		return "", refused
	}
	if found, missing := threadIn(ctx, client, typed, putAway.GetThreads()); missing == nil {
		return found, nil
	}
	return "", refused
}

// threadIn reads what the operator typed against one listing: an address, or one of the two
// identifiers that listing prints.
func threadIn(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string, threads []*quaycrewv1.Thread) (string, error) {
	if strings.Contains(typed, workspace.Separator) {
		return threadAtAddress(ctx, client, typed, threads)
	}
	return threadWithIdentifier(typed, threads)
}

// threadAtAddress reads an address the way dispatch does, then turns the handle it lands on into the
// thread id every other command works in.
func threadAtAddress(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string, threads []*quaycrewv1.Thread) (string, error) {
	path, err := workspace.ParsePath(typed)
	if err != nil {
		return "", err
	}
	if path.Thread == "" {
		return "", fmt.Errorf("%q names a project, not a thread: add the thread from the listing, for example %s/3cb04bf5",
			typed, typed)
	}
	located, err := workspace.ResolvePath(ctx, client, path)
	if err != nil {
		return "", err
	}
	for _, thread := range threads {
		if thread.GetHandle() == located.ThreadID {
			return thread.GetId(), nil
		}
	}
	return "", fmt.Errorf("%q resolved to a thread the crew no longer lists", typed)
}

// threadWithIdentifier matches a bare identifier against both of the ones a listing prints. An exact
// match wins outright, so a short identifier that happens to prefix another thread still resolves to
// itself.
func threadWithIdentifier(typed string, threads []*quaycrewv1.Thread) (string, error) {
	matches := make([]string, 0, 1)
	for _, thread := range threads {
		if thread.GetId() == typed || thread.GetHandle() == typed {
			return thread.GetId(), nil
		}
		if strings.HasPrefix(thread.GetId(), typed) || strings.HasPrefix(thread.GetHandle(), typed) {
			matches = append(matches, thread.GetId())
		}
	}

	switch len(matches) {
	case 0:
		return "", &workspace.NotFoundError{
			What: "thread", Name: typed,
			Have: identifiersOf(threads),
			Make: `start one with quay dispatch "..."`,
		}
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", &workspace.AmbiguousError{What: "threads", Name: typed, IDs: matches}
	}
}

// identifiersOf is every thread the crew holds, written as the listing writes it: the id, then the
// handle beside it. Both, because the operator was refused for typing one of them and has no way to
// tell from the refusal which one the command wanted.
func identifiersOf(threads []*quaycrewv1.Thread) []string {
	have := make([]string, 0, len(threads))
	for _, thread := range threads {
		have = append(have, fmt.Sprintf("%s (thread %s)",
			display.ShortID(thread.GetId()), display.ShortID(thread.GetHandle())))
	}
	sort.Strings(have)
	return have
}
