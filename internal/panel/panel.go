// Package panel puts a conversation on the screen beside a console that is already running.
//
// tmux does the splitting. It already keeps an open conversation alive and survives a connection
// dropping; the alternative is a terminal emulator inside a terminal.
//
// Nothing here opens on its own. `krewe` opens the console and nothing else, so every command in this
// package answers a key the operator pressed.
package panel

import (
	"fmt"
	"strings"
)

// Beside splits target, tmux.s own pane identifier, to put a conversation next to a console that is
// already running. This is what p in the console does, and the only way a conversation reaches the
// screen beside one.
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

// Away closes the conversation again and gives the console the whole width back.
func Away(pane string) ([][]string, error) {
	if strings.TrimSpace(pane) == "" {
		return nil, fmt.Errorf("panel: no conversation to close")
	}
	return [][]string{{"tmux", "kill-pane", "-t", pane}}, nil
}

// Opened is the tmux invocation that says which panes are in the window, so the console can tell which
// one appeared.
func Opened(target string) []string {
	return []string{"tmux", "list-panes", "-F", "#{pane_id}", "-t", target}
}

// Rightward asks tmux where every pane in the window starts, so the console can find the one beside
// it: same top, further right. A pane above or below is the header, not a conversation.
func Rightward(target string) []string {
	return []string{"tmux", "list-panes", "-F", "#{pane_id} #{pane_left} #{pane_top}", "-t", target}
}
