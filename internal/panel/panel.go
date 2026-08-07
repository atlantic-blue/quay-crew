// Package panel puts the crew and a conversation on the screen at once: the console on the left, the
// thread driving it on the right, each on half the width.
//
// tmux does the splitting. It is already how an open conversation is kept alive, which is what makes
// ctrl-q able to leave one running, and it already survives a connection dropping. The alternative is
// rendering other programs inside the console, which means writing a terminal emulator inside a
// terminal: resize, escape sequences, mouse, the alternate screen. That is a large piece of work
// which buys nothing until the two halves need to talk to each other.
package panel

import (
	"fmt"
	"strconv"
	"strings"
)

// SessionName is the tmux session the panel lives in. One name, so opening the panel twice comes back
// to the one already open rather than stacking a second layout on top of it.
const SessionName = "quay-panel"

// WindowName names the window inside it, so panes can be addressed without counting.
const WindowName = "panel"

// Layout is what the panel is made of: the command in each region, and what to call the whole thing.
type Layout struct {
	// Left is the console.
	Left []string
	// Right is the conversation driving it.
	Right []string
	// Session is the tmux session to live in.
	Session string
	// Width and Height are the terminal the panel is being opened from. The session is built at that
	// size rather than at tmux's default, because tmux scales the panes it already has when a client
	// of a different size attaches: a header given ten rows in an eighty by twenty four window comes
	// back as twenty one rows in a taller one, and the exact height is the point of giving it one.
	Width, Height int
}

// Terminal is what the panel is being opened from. These cannot be worked out from here without
// running something, so they are handed in.
type Terminal struct {
	// AlreadyOpen says the panel's tmux session exists. It is then moved to rather than built again:
	// splitting an open panel every time it is opened is how you end up with eleven panes.
	AlreadyOpen bool
	// InsideTmux says the operator is already inside tmux. It changes how the panel is shown and not
	// what it is: tmux refuses to attach a client that is already attached, so from in here the
	// client is switched to the panel instead. Without this the panel was built correctly, left
	// running, and never appeared.
	InsideTmux bool
}

// Commands is every tmux invocation needed to put the panel on the screen, in order.
func (l Layout) Commands(term Terminal) ([][]string, error) {
	if err := l.check(); err != nil {
		return nil, err
	}
	session := l.Session
	if session == "" {
		session = SessionName
	}
	if term.AlreadyOpen {
		return [][]string{show(term, session)}, nil
	}

	target := session + ":" + WindowName
	built := [][]string{
		// Detached, so the layout is finished before anybody looks at it. Attaching first would show
		// one pane growing into three.
		//
		append(l.newSession(session), l.Left...),
		// -h divides the window left and right: the console keeps the left, the conversation takes
		// the right. Two panes, because the header is not one.
		append([]string{"tmux", "split-window", "-h", "-l", "50%", "-t", target + ".0"}, l.Right...),
		// The console has the keyboard when the panel opens, because that is what the operator came
		// to use. The conversation is one pane away.
		{"tmux", "select-pane", "-t", target + ".0"},
		show(term, session),
	}
	return built, nil
}

// newSession builds the panel's window, at the size of the terminal opening it where that is known.
func (l Layout) newSession(session string) []string {
	argv := []string{"tmux", "new-session", "-d", "-s", session, "-n", WindowName}
	if l.Width > 0 && l.Height > 0 {
		argv = append(argv, "-x", strconv.Itoa(l.Width), "-y", strconv.Itoa(l.Height))
	}
	return argv
}

// show is how the operator ends up looking at the panel. From a plain terminal that is attaching a
// client to it. From inside tmux there is already a client, and tmux will not attach a second one, so
// the one that is there is moved instead.
func show(term Terminal, session string) []string {
	if term.InsideTmux {
		return []string{"tmux", "switch-client", "-t", session}
	}
	return []string{"tmux", "attach-session", "-t", session}
}

// HasSession is the tmux invocation that answers whether the panel is already open. It is here rather
// than written out at the call site so the session name is decided in one place.
func (l Layout) HasSession() []string {
	session := l.Session
	if session == "" {
		session = SessionName
	}
	return []string{"tmux", "has-session", "-t", session}
}

func (l Layout) check() error {
	if len(l.Left) == 0 {
		return fmt.Errorf("panel: nothing to put in the left half")
	}
	if len(l.Right) == 0 {
		return fmt.Errorf("panel: nothing to put in the right half")
	}
	return nil
}

// Beside puts a conversation next to a pane that is already on the screen, which is what the console
// does when the operator presses the key for it. The panel is the same thing built in one go; this is
// the same thing built around a console that is already running.
//
// target is the pane to split, as tmux's own pane identifier. Nothing else changes: the console keeps
// drawing its own header, which is the header the operator asked to keep.
func Beside(target string, right []string) ([][]string, error) {
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("panel: no pane to open a conversation beside")
	}
	if len(right) == 0 {
		return nil, fmt.Errorf("panel: nothing to put in the conversation")
	}
	// -d leaves the keyboard where it is. The operator asked to see a conversation, not to start
	// typing into it, and having the cursor jump out of the list is the thing that would annoy.
	return [][]string{
		append([]string{"tmux", "split-window", "-h", "-d", "-l", "50%", "-t", target}, right...),
	}, nil
}

// Away closes the conversation again and gives the console its own header back.
func Away(pane string) ([][]string, error) {
	if strings.TrimSpace(pane) == "" {
		return nil, fmt.Errorf("panel: no conversation to close")
	}
	return [][]string{{"tmux", "kill-pane", "-t", pane}}, nil
}

// Opened is the tmux invocation that says which pane the conversation ended up in, so the console can
// close the one it opened rather than whichever pane happens to be beside it now.
func Opened(target string) []string {
	return []string{"tmux", "list-panes", "-F", "#{pane_id}", "-t", target}
}
