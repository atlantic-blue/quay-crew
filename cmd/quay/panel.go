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
	"github.com/atlantic-blue/quay-crew/internal/panel"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
	"github.com/charmbracelet/x/term"
)

// runPanel puts the console and a conversation side by side, each on half the screen.
//
// Named a session opens that one. Named nothing it opens the newest conversation where you are
// standing, which is the one you were last talking to.
func runPanel(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer, addr string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay panel [<session id>]")
	}

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
	layout := panel.Layout{
		Width:  width,
		Height: height,
		Left:   []string{self},
		Right:  []string{self, "attach", sessionID},
	}

	fmt.Fprintf(out, "opening the panel on %s\n", display.ShortID(sessionID))
	return openPanel(layout, out)
}

// openPanel runs the tmux invocations the layout asks for. A panel already open is reattached to
// rather than split again.
func openPanel(layout panel.Layout, out io.Writer) error {
	asked := layout.HasSession()
	term := panel.Terminal{
		AlreadyOpen: exec.Command(asked[0], asked[1:]...).Run() == nil,
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

	current, err := currentPath()
	if err != nil {
		current = workspace.Path{}
	}
	listed, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return "", err
	}

	newest, found := newestSession(listed.GetSessions())
	if !found {
		return "", fmt.Errorf("there is no conversation to put beside the console yet%s: "+
			"start one with `quay dispatch \"hello\"`, then open the panel again", where(current))
	}
	return newest, nil
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
// The row under the cursor wins, because that is the conversation being looked at; with nothing
// selected it falls back to the one last spoken to, the same rule `quay panel` uses.
func conversationBeside(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) func(string) ([]string, error) {
	return func(selected string) ([]string, error) {
		self, err := os.Executable()
		if err != nil {
			self = "quay"
		}
		sessionID, err := panelSession(ctx, client, argsFor(selected))
		if err != nil {
			return nil, err
		}
		return []string{self, "attach", sessionID}, nil
	}
}

// argsFor turns the row under the cursor into the argument panelSession takes, which is none when
// there is no row or the row is not a session.
func argsFor(selected string) []string {
	if strings.TrimSpace(selected) == "" {
		return nil
	}
	return []string{selected}
}
