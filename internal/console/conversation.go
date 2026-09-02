package console

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/panel"
	tea "github.com/charmbracelet/bubbletea"
)

// The conversation beside the console, and the key that shows and hides it.
//
// `krewe` opens the console and nothing else, so this is the only way a conversation reaches the
// screen beside one. It is asked for, never opened for you.

// Panes is how the console opens and closes the conversation beside it. tmux is the one that runs in
// front of an operator, and a scenario gives it one whose panes it can read: without a seam here a
// test can only assert on the command a key produced, which is the half that already worked.
type Panes interface {
	// Beside is the pane the conversation sits in, and whether there is one at all.
	Beside(here string) (string, bool)
	// Open puts a command in a pane beside here, and says which pane it landed in.
	Open(here string, argv []string) (string, error)
	// Close takes a pane away.
	Close(pane string) error
}

// panesOrTmux is what this console opens panes with, which is tmux unless it was given something else.
func (m Model) panesOrTmux() Panes {
	if m.panes != nil {
		return m.panes
	}
	return tmuxPanes{}
}

// openConversationFor puts one session's conversation in the pane beside the console, and says
// whether it could. It could not when there is no pane beside it to open into, and the caller then
// hands over the console's own screen, which is all a console on its own can do.
//
// This is what enter on a session row does in a panel. The session travels to the command line, which
// decides what to run for it: handing over nothing is how the driver is asked for, and that is what
// opening the system with no argument means.
func (m Model) openConversationFor(row Row) (Model, tea.Cmd, bool) {
	if m.beside == nil || row.ID == "" {
		return m, nil, false
	}
	here := os.Getenv("TMUX_PANE")
	if here == "" {
		return m, nil, false
	}
	panes := m.panesOrTmux()
	beside, found := panes.Beside(here)
	if !found {
		return m, nil, false
	}
	right, err := m.beside(row.ID)
	if err != nil {
		// A session that cannot be opened is refused by name. The pane beside keeps the conversation
		// it had: replacing it with somebody else's is the fault this key is for, and it would be
		// worse arriving through the refusal.
		m.err, m.held = err, true
		return m, nil, true
	}
	return m, func() tea.Msg {
		if err := panes.Close(beside); err != nil {
			return conversationMsg{err: err}
		}
		pane, err := panes.Open(here, right)
		if err != nil {
			return conversationMsg{err: err}
		}
		return conversationMsg{pane: pane, session: row.ID}
	}, true
}

// toggleConversation shows the conversation beside the console, or hides the one already there.
func (m Model) toggleConversation() (Model, tea.Cmd) {
	if m.beside == nil {
		m.err = fmt.Errorf("this console cannot open a conversation: it was opened without a system")
		return m, nil
	}
	here := os.Getenv("TMUX_PANE")
	if here == "" {
		// tmux is what splits the screen. Outside it there is nothing to split, and saying so beats a
		// key that looks broken.
		m.err = fmt.Errorf("a conversation opens beside the console inside tmux: start tmux, run " +
			"`krewe` in it, and press p again")
		return m, nil
	}

	// Asked of tmux rather than remembered, because the operator can split their own window and the
	// console did not put that pane there. A console that only knew about the ones it opened would
	// answer the first p by opening a second.
	panes := m.panesOrTmux()
	if beside, found := panes.Beside(here); found {
		return m, closeConversationCmd(panes, beside)
	}
	selected := ""
	if row, found := m.selectedRowValue(); found {
		selected = row.ID
	}
	right, err := m.beside(selected)
	if err != nil {
		m.err = err
		return m, nil
	}
	return m, openConversationCmd(panes, here, selected, right)
}

// openConversationCmd puts a conversation beside the console and reports which pane it landed in.
func openConversationCmd(panes Panes, here, session string, right []string) tea.Cmd {
	return func() tea.Msg {
		pane, err := panes.Open(here, right)
		if err != nil {
			return conversationMsg{err: err}
		}
		return conversationMsg{pane: pane, session: session}
	}
}

// closeConversationCmd removes the pane the conversation is in and gives the console its width back.
func closeConversationCmd(panes Panes, pane string) tea.Cmd {
	return func() tea.Msg {
		if err := panes.Close(pane); err != nil {
			return conversationMsg{err: err}
		}
		return conversationMsg{}
	}
}

// tmuxPanes is the real one: the panes are tmux's, and every answer is asked of it rather than
// remembered, because the operator can split their own window and the console did not put that pane
// there.
type tmuxPanes struct{}

// Beside is the pane immediately to the right of this one, which is where a conversation beside the
// console sits.
func (tmuxPanes) Beside(here string) (string, bool) {
	argv := panel.Rightward(here)
	output, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		return "", false
	}
	return rightOf(string(output), here)
}

// Open splits the console's pane and answers which pane the conversation landed in.
func (tmuxPanes) Open(here string, right []string) (string, error) {
	before, err := panesBeside(here)
	if err != nil {
		return "", err
	}
	commands, err := panel.Beside(here, right)
	if err != nil {
		return "", err
	}
	for _, argv := range commands {
		if output, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("open the conversation: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}

	after, err := panesBeside(here)
	if err != nil {
		return "", err
	}
	// Whichever pane is there now and was not before. Asked of tmux rather than assumed, because the
	// identifier it hands out is not something to guess at.
	for pane := range after {
		if !before[pane] {
			return pane, nil
		}
	}
	// It did not open. The pane was made and its command was gone before this could list the panes,
	// which is what a conversation that could not be opened looks like from out here: the reason was
	// printed into a pane that no longer exists. Say that, rather than reporting an open conversation
	// nobody can find.
	return "", fmt.Errorf("the conversation closed as soon as it opened, so the reason went with it. " +
		"Run krewe attach on this session in a terminal to read it")
}

// Close removes a pane, and a pane the operator already closed themselves is not a failure: the
// conversation is gone either way, which is what was asked for.
func (tmuxPanes) Close(pane string) error {
	commands, err := panel.Away(pane)
	if err != nil {
		return err
	}
	for _, argv := range commands {
		_ = exec.Command(argv[0], argv[1:]...).Run()
	}
	return nil
}

// panesBeside is every pane in the window the console is in, by tmux's own identifier.
func panesBeside(here string) (map[string]bool, error) {
	argv := panel.Opened(here)
	output, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("ask tmux which panes are open: %w", err)
	}
	panes := make(map[string]bool, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if pane := strings.TrimSpace(line); pane != "" {
			panes[pane] = true
		}
	}
	return panes, nil
}

// rightOf is the pane immediately to the right of here, from what tmux listed. Same top, further left:
// a pane above or below is the header, not a conversation.
func rightOf(listing, here string) (string, bool) {
	mine, found := paneAt(listing, here)
	if !found {
		return "", false
	}
	beside, bestLeft := "", 0
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		id, left, top, ok := parsePane(line)
		if !ok || id == here || top != mine.top || left <= mine.left {
			continue
		}
		// The nearest one, so a window split three ways closes the neighbour rather than the far end.
		if beside == "" || left < bestLeft {
			beside, bestLeft = id, left
		}
	}
	return beside, beside != ""
}

type paneBox struct{ left, top int }

func paneAt(listing, want string) (paneBox, bool) {
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		if id, left, top, ok := parsePane(line); ok && id == want {
			return paneBox{left: left, top: top}, true
		}
	}
	return paneBox{}, false
}

// parsePane reads one line of what tmux was asked for: the pane, then where it starts.
func parsePane(line string) (id string, left, top int, ok bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) != 3 {
		return "", 0, 0, false
	}
	left, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, 0, false
	}
	top, err = strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, 0, false
	}
	return parts[0], left, top, true
}

// startFreshConversation ends the one the driver is in and opens a new one in its place.
//
// Coming back to the conversation you were in is what opening the system does, and what ctrl-q is for.
// This is the other thing somebody wants sometimes: a clean start, without losing the old one by
// accident. Ending it here is deliberate and asked for.
func (m Model) startFreshConversation() (Model, tea.Cmd) {
	if m.beside == nil || m.freshen == nil {
		m.err = fmt.Errorf("this console cannot open a conversation: it was opened without a system")
		return m, nil
	}
	here := os.Getenv("TMUX_PANE")
	if here == "" {
		m.err = fmt.Errorf("a conversation opens beside the console inside tmux: start tmux, run " +
			"`krewe` in it, and press this key again")
		return m, nil
	}

	selected := ""
	if row, found := m.selectedRowValue(); found {
		selected = row.ID
	}
	right, err := m.beside(selected)
	if err != nil {
		m.err = err
		return m, nil
	}
	// The pane beside the console, asked of tmux rather than remembered: the operator can split their
	// own window, so the console does not know that pane.s identifier. Remembering was how this opened
	// a fourth pane instead of replacing the third.
	panes := m.panesOrTmux()
	beside, _ := panes.Beside(here)
	freshen := m.freshen
	return m, func() tea.Msg {
		// The old one goes first, or reopening comes straight back to it: the conversation runs in a
		// tmux session inside the sandbox that is attached to rather than started when it is there.
		if err := freshen(selected); err != nil {
			return conversationMsg{err: err}
		}
		if beside != "" {
			closeConversationCmd(panes, beside)()
		}
		return openConversationCmd(panes, here, selected, right)()
	}
}
