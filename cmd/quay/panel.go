package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/panel"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
	"github.com/charmbracelet/x/term"
)

// errNothingBeside is the one reason opening the crew falls back to the console on its own: there is
// nothing to put beside it yet, which is what a crew nobody has used yet looks like. Every other
// failure is the operator's to see.
var errNothingBeside = errors.New("nothing to put beside the console yet")

// runPanel puts the console and a conversation side by side, each on half the screen.
//
// Named a session opens that one. Named nothing it opens the newest conversation where you are
// standing, which is the one you were last talking to.
// runPanel is what `quay` does: it opens the crew. The header across the top, the console under it on
// the left, and a conversation on the right.
//
// There is no separate command for it: a second command to open the thing the first command is for
// reads as a second product.
func runPanel(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer, addr string) error {
	sessionID, err := panelSession(ctx, client, args)
	if err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		// Falling back to the name means it is resolved from PATH, which is the copy the shell runs
		// anyway. Refusing to open the panel over this would be a strange place to stop.
		self = "quay"
	}
	width, height := terminalSize()
	rows, err := headerRows(ctx, client, addr, width, height)
	if err != nil {
		return err
	}
	layout := panel.Layout{
		Version:    version,
		Header:     []string{self, "header"},
		HeaderRows: rows,
		Width:      width,
		Height:     height,
		// The header is drawn in the pane above, across both halves, so this one draws none.
		Left:  []string{self, "console"},
		Right: []string{self, "attach", sessionID},
	}

	fmt.Fprintf(out, "opening on %s\n", display.ShortID(sessionID))
	return openPanel(layout, out)
}

// openPanel runs the tmux invocations the layout asks for. A panel already open is reattached to
// rather than split again.
func openPanel(layout panel.Layout, out io.Writer) error {
	asked := layout.HasSession()
	open := exec.Command(asked[0], asked[1:]...).Run() == nil
	term := panel.Terminal{
		AlreadyOpen: open,
		// An open panel built by a different build is torn down and made again. Its panes are running
		// the old binary, so reattaching to it shows yesterday's tool however many times you upgrade.
		Stale: open && builtBy(layout) != version,
		// tmux sets this for everything it runs, so it is how a program knows it is already inside
		// one. From in there the panel is switched to rather than attached, because tmux refuses a
		// second client on a terminal that already has one.
		InsideTmux: os.Getenv("TMUX") != "",
	}

	commands, err := layout.Commands(term)
	if err != nil {
		return err
	}
	for _, argv := range commands {
		command := exec.Command(argv[0], argv[1:]...)
		// Only the last one takes the terminal, and every one of them can fail with something worth
		// reading, so they all get somewhere to say it.
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, out, os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("panel: %s: %w", argv[1], err)
		}
	}
	return nil
}

// panelSession decides which conversation the right half opens.
//
// It refuses rather than opening half a panel. A layout with an empty pane, or one that collapses
// back to a single pane the moment its command exits, reads as the panel being broken.
func panelSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string) (string, error) {
	if len(args) == 1 {
		return resolveSession(ctx, client, args[0])
	}

	project, err := driverProject(ctx, client)
	if err != nil {
		return "", err
	}

	// The driver, made the first time the crew is opened. Not whichever conversation happens to be
	// newest: that is somebody else's job, and opening the crew should not drop you into it.
	opened, err := client.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: project})
	if err != nil {
		return "", err
	}
	return opened.GetSession().GetId(), nil
}

// newestSession is the conversation most recently spoken to, which is the one the operator was last
// working in. A session with no conversation behind it cannot be opened at all, so it is not offered.
func newestSession(sessions []*quaycrewv1.Session) (string, bool) {
	live := make([]*quaycrewv1.Session, 0, len(sessions))
	for _, session := range sessions {
		if session.GetModelSessionId() != "" && session.GetArchivedAt() == nil {
			live = append(live, session)
		}
	}
	if len(live) == 0 {
		return "", false
	}
	sort.Slice(live, func(i, j int) bool {
		return live[i].GetUpdatedAt().AsTime().After(live[j].GetUpdatedAt().AsTime())
	})
	return live[0].GetId(), true
}

func where(current workspace.Path) string {
	if current.Workspace == "" {
		return ""
	}
	if current.Project == "" {
		return " in " + current.Workspace
	}
	return " in " + current.Workspace + "/" + current.Project
}

// terminalSize is the terminal the panel is being opened from, so tmux builds the window at that size
// rather than at its own default and then scaling everything when a client attaches.
func terminalSize() (int, int) {
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil || width == 0 {
		return 120, 40
	}
	return width, height
}

// conversationBeside is what the console runs when it is asked to put a conversation next to itself.
//
// Always the driver, whatever the cursor is on. Following the cursor reads well until the key for a
// fresh conversation is pressed: that would end whichever session the cursor happened to be over,
// while the pane beside the console carried on showing the driver.
func conversationBeside(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) func(string) ([]string, error) {
	return func(string) ([]string, error) {
		self, err := os.Executable()
		if err != nil {
			self = "quay"
		}
		sessionID, err := panelSession(ctx, client, nil)
		if err != nil {
			return nil, err
		}
		return []string{self, "attach", sessionID}, nil
	}
}

// headerRows is how tall the header pane has to be, measured from the header itself rather than
// guessed. A guess one line short cuts the last line off, and one line long leaves a gap.
func headerRows(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, addr string, width, height int) (int, error) {
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return 0, err
	}
	rows := console.HeaderHeight(registry, console.Default, headerInfo(ctx, client, addr), width, height)
	if rows < 1 {
		return 0, fmt.Errorf("the header would have no rows to draw in")
	}
	return rows, nil
}

// runBareConsole is the panel's lower left: the console with no header of its own, saying which view
// it moves to so the header in the pane above names that view's keys.
func runBareConsole(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, addr string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: quay console, and quay runs it for you")
	}
	current, err := currentPath()
	if err != nil {
		current = workspace.Path{}
	}
	return console.RunBare(ctx, client, console.Info{
		Version:   version,
		Address:   addr,
		Workspace: current.Workspace,
		Project:   current.Project,
	}, conversationBeside(ctx, client), endConversationBeside(ctx, client), publishView)
}

// builtBy is the build that made the panel that is already open, and empty when it did not say, which
// is every panel made before this was recorded.
func builtBy(layout panel.Layout) string {
	argv := panel.Built(layout.Session)
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

	// Narrowed to the workspace you are standing in. `quay use atlantic-blue` said something, and
	// counting projects across the whole crew threw it away: a crew with eight projects refused to
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

	// None to choose from, or too many to choose between. Both mean the same thing to the operator:
	// there is nothing to put beside the console, so the console opens on its own and they pick from
	// it. `quay` opens the crew. It does not refuse to open the crew and tell them to type something
	// first, which is what it did with more than one project in reach.
	return "", fmt.Errorf("%w: %s", errNothingBeside, whyNoConversation(current, len(listed.GetProjects())))
}

// whyNoConversation is the reason nothing opened beside the console, for a caller that reports it.
// Opening the crew swallows this by design, because the console is the answer rather than a message.
func whyNoConversation(current workspace.Path, projects int) string {
	if projects == 0 {
		if current.Workspace == "" {
			return "there is no project for the crew to open in, press n and choose project"
		}
		return "there is no project in " + current.Workspace + ", press n and choose project"
	}
	if current.Workspace == "" {
		return "there is more than one project, so say which: quay use <workspace>/<project>"
	}
	return "there is more than one project in " + current.Workspace +
		", so say which: quay use " + current.Workspace + "/<project>"
}

// endConversationBeside ends the conversation the driver is in, so the next open starts one.
//
// The conversation runs in a tmux session inside the sandbox, attached to rather than started when it
// is already there, which is what makes coming back to it job. Ending that session is what makes the
// next open a fresh start.
func endConversationBeside(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) func(string) error {
	return func(string) error {
		sessionID, err := panelSession(ctx, client, nil)
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
		return fmt.Errorf("the crew did not say which sandbox the conversation is in")
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
