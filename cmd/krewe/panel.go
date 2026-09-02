package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/console"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
)

// panelSession decides which conversation opens beside the console when the operator asks for one.
//
// It refuses rather than opening a pane with nothing in it. A pane that collapses the moment its
// command exits reads as the key being broken.
func panelSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string) (string, error) {
	if len(args) == 1 {
		return resolveSession(ctx, client, args[0])
	}

	project, err := driverProject(ctx, client)
	if err != nil {
		return "", err
	}

	// The driver, made the first time a conversation is asked for. Not whichever conversation happens
	// to be newest: that is somebody else.s job, and asking for one should not drop you into it.
	opened, err := client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: project})
	if err != nil {
		return "", err
	}
	return opened.GetSession().GetId(), nil
}

// conversationBeside is what the console runs when it is asked to put a conversation next to itself.
//
// The session the console hands over is the one that opens, because that is the row the operator has
// their cursor on. Handing over nothing asks for the driver, which is what pressing p on no session
// means and is deliberate.
func conversationBeside(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) func(string) ([]string, error) {
	return func(selected string) ([]string, error) {
		self, err := os.Executable()
		if err != nil {
			self = "krewe"
		}
		sessionID, err := panelSession(ctx, client, pointedAt(selected))
		if err != nil {
			return nil, err
		}
		return []string{self, "attach", sessionID}, nil
	}
}

// pointedAt is the session the console pointed at, in the form panelSession takes it. Nothing
// pointed at is no argument at all, which is the driver.
func pointedAt(selected string) []string {
	if selected == "" {
		return nil
	}
	return []string{selected}
}

// runBareConsole is `krewe console`, which is the same console `krewe` on its own opens. Two names for
// one screen, because the panes a conversation runs in still spell it out.
func runBareConsole(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, addr string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: krewe console, and krewe runs it for you")
	}
	current, err := currentPath()
	if err != nil {
		current = workspace.Path{}
	}
	return console.Run(ctx, client, console.Info{
		Version:   version,
		Address:   addr,
		Workspace: current.Workspace,
		Project:   current.Project,
	}, conversationBeside(ctx, client), endConversationBeside(ctx, client), remembering())
}

// driverProject is the project the driver belongs to: where the operator is standing, or the only
// project there is when they are standing nowhere yet.
func driverProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) (string, error) {
	current, err := currentPath()
	if err != nil {
		current = workspace.Path{}
	}
	if current.Project != "" {
		resolved, err := workspace.ResolveProject(ctx, client, current.Workspace, current.Project)
		if err == nil {
			return resolved, nil
		}
	}

	// Narrowed to the workspace you are standing in. `krewe use atlantic-blue` said something, and
	// counting projects across the whole system threw it away: a system with eight projects refused to
	// open even though the workspace it was standing in held one.
	request := &quaycrewv1.ListProjectsRequest{}
	if current.Workspace != "" {
		if id, err := workspace.Resolve(ctx, client, current.Workspace); err == nil {
			request.Workspace = id
		}
	}
	listed, err := client.ListProjects(ctx, request)
	if err != nil {
		return "", err
	}
	if len(listed.GetProjects()) == 1 {
		return listed.GetProjects()[0].GetId(), nil
	}

	// None to choose from, or too many to choose between. Both leave the key with no conversation to
	// open, and the reason names the one thing the operator can do about it. The console stays where it
	// is and shows this: asking for a conversation and getting nothing is a refusal, never a screen
	// that goes away.
	return "", fmt.Errorf("%s", whyNoConversation(current, len(listed.GetProjects())))
}

// whyNoConversation is the reason nothing can open beside the console, in the words of what to do
// about it. The operator asked for a conversation, so the answer is how to get one.
func whyNoConversation(current workspace.Path, projects int) string {
	if projects == 0 {
		if current.Workspace == "" {
			return "there is no project to open a conversation in, press o and choose project"
		}
		return "there is no project in " + current.Workspace + ", press o and choose project"
	}
	if current.Workspace == "" {
		return "there is more than one project, so say which: krewe use <workspace>/<project>"
	}
	return "there is more than one project in " + current.Workspace +
		", so say which: krewe use " + current.Workspace + "/<project>"
}

// endConversationBeside ends the conversation the driver is in, so the next open starts a fresh one.
//
// The conversation runs in a tmux session inside the sandbox, attached to rather than started when it
// is already there, which is what makes coming back to it work. Ending that session is what makes the
// next open a fresh start.
func endConversationBeside(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) func(string) error {
	return func(selected string) error {
		// The same session the fresh one opens in. Ending one conversation and opening another leaves
		// the operator looking at a conversation they did not end, and ends one they cannot see.
		sessionID, err := panelSession(ctx, client, pointedAt(selected))
		if err != nil {
			return err
		}
		spec, err := client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sessionID})
		if err != nil {
			return err
		}
		return endConversation(spec.GetSandbox(), inTheSandbox)
	}
}

// inTheSandbox runs a command inside a session's container and returns whatever it had to say.
func inTheSandbox(box string, argv ...string) ([]byte, error) {
	return exec.Command("docker", append([]string{"exec", box}, argv...)...).CombinedOutput()
}

// endConversation ends the conversation running in a sandbox, and says so when it could not.
//
// The failure to end it is the whole thing worth reporting, because the pane is reopened straight
// after: a conversation that is still running is attached to rather than started, so it comes back
// with its history and the key reads as doing nothing at all. That is what it did, quietly, whenever
// the container was not running, or the image was too old to have tmux in it.
//
// Whether it worked is answered by asking, not by the exit status. Ending a conversation that was
// never there fails too, and that is the state the next open wants anyway, so what matters is whether
// one is still running afterwards.
func endConversation(box string, run func(box string, argv ...string) ([]byte, error)) error {
	if box == "" {
		return fmt.Errorf("the system did not say which sandbox the conversation is in")
	}
	out, err := run(box, "tmux", "kill-session", "-t", sandbox.AttachedSessionName)
	if err == nil {
		return nil
	}
	if _, running := run(box, "tmux", "has-session", "-t", sandbox.AttachedSessionName); running != nil {
		// Nothing left to end, which is what the next open wants.
		return nil
	}
	return fmt.Errorf("the conversation in %s could not be ended, so opening it again comes back to "+
		"the one that is there: %s", box, strings.TrimSpace(string(out)))
}
