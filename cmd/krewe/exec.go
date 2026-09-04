package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
)

// flagDispatch is the one flag the word takes: send the exec and let go of it.
//
// A flag rather than a second word, because the two differ in one thing only, whether anybody waits,
// and a second top level word for that made a person choose between three commands before they could
// say anything at all.
const flagDispatch = "--dispatch"

// execUsage is both shapes of the word, because somebody who typed one of them wrongly is as likely
// to have wanted the other.
const execUsage = "usage: krewe exec [--dispatch] [<address>] <text>" +
	"\n   or: krewe exec list <session>"

// runExec is the one word for an exec: send one, or read back what a session was sent.
//
// An exec is an entity like a job and like a flow, so it reads like them: one noun, verbs under it.
// It used to be three top level commands, ask, dispatch and execs, and reading the command list gave
// no clue that the three were one thing.
func runExec(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	letGo, rest, err := lettingGo(args)
	if err != nil {
		return err
	}
	if len(rest) > 0 && rest[0] == "list" {
		if letGo {
			return fmt.Errorf("%s sends an exec, and says nothing about reading one back"+
				"\n\n  krewe exec list <session>", flagDispatch)
		}
		return runExecList(ctx, client, rest[1:], out)
	}
	return sendExec(ctx, client, rest, letGo, out)
}

// lettingGo reads the flag off the front and hands back what is left.
//
// It is taken only in first position. Anywhere else it is refused rather than read, because the
// alternative is reading it out of the middle of a sentence somebody meant to send: `krewe exec "say
// --dispatch to the operator"` is one word away from a flag, and a message with a word silently
// removed from it is the defect this whole shape exists to avoid.
func lettingGo(args []string) (letGo bool, rest []string, err error) {
	for at, arg := range args {
		if arg == flagDispatch && at != 0 {
			return false, nil, fmt.Errorf("%s comes first or it is part of the message"+
				"\n\n%s", flagDispatch, execUsage)
		}
	}
	if len(args) > 0 && args[0] == flagDispatch {
		return true, args[1:], nil
	}
	return false, args, nil
}

// sendExec starts an exec, and either waits for the answer here or lets go of it.
//
// Letting go is what real work wants, because an exec takes as long as the job takes, which is
// minutes and sometimes an hour, and holding it in the client makes the terminal the weakest part of
// the system: an exec killed at seventeen minutes recorded "failed: model: run exited: signal: killed",
// said nothing about why, and the job was gone. Waiting is what a short question wants, because the
// person typing it is looking at the terminal and the reply is the point.
func sendExec(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string,
	letGo bool, out io.Writer) error {
	if err := notASessionOnItsOwn(args); err != nil {
		return err
	}
	typed, words := workspace.SplitSession(args)
	text := strings.TrimSpace(strings.Join(words, " "))
	if text == "" {
		return fmt.Errorf("%s", execUsage)
	}

	project, handle := "", ""
	switch {
	case typed == "" || strings.Contains(typed, workspace.Separator):
		// No address at all is where the operator is standing, which may already name a session.
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		if !located.HasProject() {
			return needsAProject(ctx, client, located)
		}
		project, handle = located.ProjectID, located.SessionID
	default:
		// One bare identifier, read off the session column. The system's own call takes a project and a
		// handle, so both are read from the session the operator named rather than worked out again.
		session, err := workspace.Session(ctx, client, typed)
		if err != nil {
			return fmt.Errorf("%w\n\nto send %q as the message instead, quote the whole message", err, typed)
		}
		project, handle = session.GetProject(), session.GetHandle()
	}

	resp, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: handle, Text: text, Detach: letGo,
	})
	if err != nil {
		return err
	}
	// Said out loud, because an empty line where a reply used to be reads as an exec that answered
	// nothing. It names both ways back in: the history, and the conversation itself.
	if letGo {
		fmt.Fprintf(out, "started. the system has it, and nothing here is waiting for it.\n")
		fmt.Fprintf(out, "read it back with krewe exec list %s, or sit in it with krewe attach %s\n",
			display.ShortID(resp.GetHandle()), display.ShortID(resp.GetHandle()))
	} else {
		fmt.Fprintln(out, resp.GetReply())
	}
	fmt.Fprintf(out, "(session %s, handle %s)\n", resp.GetId(), resp.GetHandle())
	return nil
}

// notASessionOnItsOwn refuses one argument that names a session and nothing else.
//
// This is the way off `krewe execs <session>`, which the same fingers now type as `krewe exec
// <session>`. That used to print a history and would otherwise send the session's own identifier to
// the model as a message: the command succeeds, the operator reads a listing that does not have what
// they asked for, and nothing anywhere says the word changed. An identifier the system does not hold
// is refused a level down, so this covers the one that it does.
func notASessionOnItsOwn(args []string) error {
	if len(args) != 1 || !workspace.NamesASession(args[0]) {
		return nil
	}
	only := args[0]
	if strings.Contains(only, workspace.Separator) {
		return fmt.Errorf("%q says where, not what: an exec needs something to say after the address"+
			"\n\n  krewe exec %s \"...\"", only, only)
	}
	return fmt.Errorf("krewe exec %s printed a session's history, and this word sends an exec now"+
		"\n\n  read that session back:  krewe exec list %s"+
		"\n  send it as the message:  krewe exec <address> %s", only, only, only)
}

// needsAProject explains an address that stops at a workspace and lists what it holds, because the
// next thing the operator needs is the name of one of those projects.
func needsAProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, located workspace.Location) error {
	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: located.WorkspaceID})
	if err != nil {
		return fmt.Errorf("%s is a workspace: an exec runs in a project inside it", located.Path.Workspace)
	}
	if len(resp.GetProjects()) == 0 {
		return fmt.Errorf("%s holds no projects yet: create one with `krewe project create <name>`", located.Path.Workspace)
	}
	names := make([]string, 0, len(resp.GetProjects()))
	for _, project := range resp.GetProjects() {
		names = append(names, project.GetName())
	}
	return fmt.Errorf("%s is a workspace: an exec runs in a project inside it, one of %s",
		located.Path.Workspace, strings.Join(names, ", "))
}

// runExecList prints one session's history: what was asked, what came back, in the order it happened.
//
// This reads the history the sending path writes rather than the model's own conversation store, so
// it answers without starting a container, and it keeps answering for a session whose sandbox is long
// gone. What it does not have is the working inside an exec, the tool calls and the thinking: for
// that, `krewe attach` opens the conversation itself.
func runExecList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: krewe exec list <session>\n\na session is its id, its handle, or its address")
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	resp, err := client.ListExecs(ctx, &quaycrewv1.ListExecsRequest{Session: sessionID})
	if err != nil {
		return err
	}
	// The session is the scope here, and it is named for the same reason every other listing names
	// where it looked: a history read off the wrong session is unreadable as a mistake.
	where := narrowedTo("execs", "session "+display.ShortID(sessionID), "krewe sessions system lists every session")
	if len(resp.GetExecs()) == 0 {
		where.nothing(out)
		return nil
	}

	for _, exec := range resp.GetExecs() {
		when := exec.GetOccurredAt().AsTime().Local().Format("15:04:05")
		fmt.Fprintf(out, "%s  you  %s\n", when, oneLine(exec.GetPrompt()))
		switch exec.GetStatus() {
		case "failed":
			fmt.Fprintf(out, "          failed: %s\n", oneLine(exec.GetFailure()))
		// An exec is written when it starts, so the one at the end of this listing is often still
		// working. Saying so is the difference between an exec in flight and one that answered with
		// nothing.
		case "running":
			fmt.Fprintf(out, "          still running\n")
		default:
			fmt.Fprintf(out, "          %s\n", oneLine(exec.GetReply()))
		}
	}
	where.counted(out, len(resp.GetExecs()))
	return nil
}

// oneLine keeps a listing readable when a reply runs to paragraphs: a history is for finding the
// exec you want, and `krewe attach` is for reading it.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const most = 120
	if len(flat) <= most {
		return flat
	}
	return flat[:most] + "..."
}
