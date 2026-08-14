package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// headerRedraw is how often the header pane redraws. It carries where you are standing and which
// view the console is on, and both change while you work, so it cannot be drawn once.
const headerRedraw = time.Second

// runHeader draws the panel's header across the full width of its own pane, and keeps it true.
//
// It is a pane of its own because a tmux pane is a rectangle: a header reaching across both halves
// cannot belong to either of them. Being a separate process is why the console publishes which view
// it is on, because otherwise these key hints would be the ones for whichever view it opened on.
func runHeader(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer, addr string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: quay header, and the panel runs it for you")
	}

	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return err
	}

	// The alternate screen, so redrawing never touches the scrollback. Without it every redraw is
	// another header in the pane's history and the pane scrolls: seventeen headers stacked up the
	// screen, which is what it looked like.
	defer fmt.Fprint(out, leaveAlternateScreen)

	first := true
	for {
		if err := drawHeader(ctx, registry, client, out, addr, first); err != nil {
			return err
		}
		first = false
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(headerRedraw):
		}
	}
}

// drawHeader paints one frame: the whole pane, cleared and rewritten. The header is a handful of
// lines and it is redrawn once a second, so there is nothing here worth the complexity of working out
// which of them changed.
func drawHeader(ctx context.Context, registry *console.Registry, client quaycrewv1.ControlPlaneServiceClient, out io.Writer, addr string, first bool) error {
	width, height := headerSize()
	lines, err := console.HeaderOnly(registry, currentView(), headerInfo(ctx, client, addr), width, height)
	if err != nil {
		return err
	}
	// Home the cursor and clear, rather than scrolling: a pane that scrolls loses its own top line.
	return paintHeader(out, lines, width, first)
}

// headerSize is the pane the header has to fit. A pane with no terminal behind it is being captured
// rather than watched, so it gets a width worth reading.
func headerSize() (int, int) {
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil || width == 0 {
		return 120, 24
	}
	return width, height
}

// headerInfo describes the crew, the same way the console's own status block does. Where you are
// standing is read every time rather than once, because `quay use` in the other pane moves it.
func headerInfo(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, addr string) console.Info {
	info := console.Info{Version: version, Address: addr}
	if current, err := currentPath(); err == nil {
		info.Workspace, info.Project = current.Workspace, current.Project
	}

	described, err := client.GetInfo(ctx, &quaycrewv1.GetInfoRequest{})
	if err != nil {
		// Behind, or not answering. Either way the header says what it knows rather than nothing.
		return info
	}
	info.Model = described.GetModel()
	info.Sandbox = described.GetSandbox()
	info.Store = described.GetStore()
	info.Secrets = described.GetSecrets()
	info.State = described.GetState()
	info.Events = described.GetEvents()
	info.SandboxBuild = described.GetSandboxBuild()
	// What the crew has cost. Its own call, because it is a running total rather than configuration,
	// and the header is redrawn every second so it stays true.
	if spent, err := client.GetUsage(ctx, &quaycrewv1.GetUsageRequest{}); err == nil {
		info.Spent = sandbox.Usage{
			Input:        spent.GetTotal().GetInput(),
			Output:       spent.GetTotal().GetOutput(),
			CacheRead:    spent.GetTotal().GetCacheRead(),
			CacheWritten: spent.GetTotal().GetCacheWritten(),
		}
	}
	return info
}

// viewFile is where the console says which view it is on, so the header in another pane can name that
// view's keys. It is one small file rather than a channel because the two processes already share a
// data directory, and a header that is a second late is not a problem worth a socket.
func viewFile() (string, error) {
	home, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "panel-view"), nil
}

// currentView is the view the console says it is showing, falling back to the one the panel opens on.
// A header naming the wrong keys is worse than one naming the usual keys, so the fallback is the view
// the console starts in.
func currentView() string {
	path, err := viewFile()
	if err != nil {
		return console.Default
	}
	said, err := os.ReadFile(path) //nolint:gosec // a path this process built, under its own data directory
	if err != nil {
		return console.Default
	}
	view := strings.TrimSpace(string(said))
	if view == "" {
		return console.Default
	}
	return view
}

// publishView is how the console says where it moved to.
func publishView(view string) error {
	path, err := viewFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(view+"\n"), 0o644) //nolint:gosec // a view name, not a secret
}

// truncateWidth cuts a line to a printed width, counting what is displayed rather than what is
// stored: the header is styled, so most of its bytes are escape sequences that take up no room.
func truncateWidth(line string, width int) string {
	if width < 1 || lipgloss.Width(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "")
}

// paintHeader draws one frame: the whole pane, homed and cleared and rewritten. Printing after what is
// already there scrolls, and what scrolls in a pane this short is the header itself.
func paintHeader(out io.Writer, lines []string, width int, first bool) error {
	// The alternate screen on the first frame, so nothing this draws ever becomes scrollback. Without
	// it every redraw is another header in the pane's history and the pane scrolls.
	if first {
		fmt.Fprint(out, alternateScreen)
	}
	fmt.Fprint(out, "\033[H\033[2J")
	for index, line := range lines {
		if index > 0 {
			fmt.Fprint(out, "\r\n")
		}
		fmt.Fprint(out, truncateWidth(line, width-1))
	}
	return nil
}

// The alternate screen. Redrawing there never touches the scrollback, so a pane that redraws every
// second does not fill with old headers and start scrolling.
const (
	alternateScreen      = "\033[?1049h"
	leaveAlternateScreen = "\033[?1049l"
)
