// Package panel puts the crew and a conversation on the screen at once: the console on the left, the
// thread driving it on the right, each on half the width.
//
// tmux does the splitting. It is already how an open conversation is kept alive, which is what makes
// ctrl-q able to leave one running, and it already survives a connection dropping. The alternative is
// rendering other programs inside the console, which means writing a terminal emulator inside a
// terminal: resize, escape sequences, mouse, the alternate screen. That is a large piece of work
// which buys nothing until the two halves need to talk to each other.
package panel

import "fmt"

// SessionName is the tmux session the panel lives in. One name, so opening the panel twice comes back
// to the one already open rather than stacking a second layout on top of it.
const SessionName = "quay-panel"

// WindowName names the window inside it, so panes can be addressed without counting.
const WindowName = "panel"

// Layout is what the panel is made of: the command in each half, and what to call the whole thing.
type Layout struct {
	// Left is the console.
	Left []string
	// Right is the conversation driving it.
	Right []string
	// Session is the tmux session to live in.
	Session string
}

// Commands is every tmux invocation needed to put the panel on the screen, in order.
//
// Two shapes, because they are genuinely different situations. A panel that is already open is
// reattached to, and must not be split again: splitting an open panel every time it is opened is how
// you end up with eleven panes. A panel that is not open yet is built and then attached to.
//
// running says whether the tmux session already exists, which is the one thing this cannot work out
// for itself without running something.
func (l Layout) Commands(running bool) ([][]string, error) {
	if err := l.check(); err != nil {
		return nil, err
	}
	session := l.Session
	if session == "" {
		session = SessionName
	}
	if running {
		return [][]string{{"tmux", "attach-session", "-t", session}}, nil
	}

	target := session + ":" + WindowName
	built := [][]string{
		// Detached, so the layout is finished before anybody looks at it. Attaching first would show
		// the console alone for as long as the split takes.
		append([]string{"tmux", "new-session", "-d", "-s", session, "-n", WindowName}, l.Left...),
		// -h splits left and right, and 50% is the half the layout is for.
		append([]string{"tmux", "split-window", "-h", "-l", "50%", "-t", target}, l.Right...),
		// The console has the keyboard when the panel opens, because that is what the operator came
		// to use. The conversation is one pane away.
		{"tmux", "select-pane", "-t", target + ".0"},
		{"tmux", "attach-session", "-t", session},
	}
	return built, nil
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
