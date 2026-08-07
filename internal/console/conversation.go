package console

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/panel"
	tea "github.com/charmbracelet/bubbletea"
)

// The conversation beside the console, and the key that shows and hides it.
//
// `quay panel` builds this in one go. This is the same thing around a console already running, so the
// operator does not have to decide before opening it whether they wanted a conversation next to it.

// toggleConversation shows the conversation beside the console, or hides the one already there.
func (m Model) toggleConversation() (Model, tea.Cmd) {
	if m.beside == nil {
		m.err = fmt.Errorf("this console cannot open a conversation: it was opened without a crew")
		return m, nil
	}
	here := os.Getenv("TMUX_PANE")
	if here == "" {
		// tmux is what splits the screen. Outside it there is nothing to split, and saying so beats a
		// key that looks broken.
		m.err = fmt.Errorf("a conversation opens beside the console inside tmux: run `quay panel`, " +
			"or start tmux and press p again")
		return m, nil
	}

	if m.conversation != "" {
		return m, closeConversationCmd(m.conversation)
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
	return m, openConversationCmd(here, right)
}

// openConversationCmd splits the console's pane and reports which pane the conversation landed in.
func openConversationCmd(here string, right []string) tea.Cmd {
	return func() tea.Msg {
		before, err := panesBeside(here)
		if err != nil {
			return conversationMsg{err: err}
		}
		commands, err := panel.Beside(here, right)
		if err != nil {
			return conversationMsg{err: err}
		}
		for _, argv := range commands {
			if output, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
				return conversationMsg{err: fmt.Errorf("open the conversation: %s: %w",
					strings.TrimSpace(string(output)), err)}
			}
		}

		after, err := panesBeside(here)
		if err != nil {
			return conversationMsg{err: err}
		}
		// Whichever pane is there now and was not before. Asked of tmux rather than assumed, because
		// the identifier it hands out is not something to guess at.
		for pane := range after {
			if !before[pane] {
				return conversationMsg{pane: pane}
			}
		}
		return conversationMsg{err: fmt.Errorf("the conversation opened and tmux does not say where")}
	}
}

// closeConversationCmd removes the pane the console opened and gives it back its own header.
func closeConversationCmd(pane string) tea.Cmd {
	return func() tea.Msg {
		commands, err := panel.Away(pane)
		if err != nil {
			return conversationMsg{err: err}
		}
		for _, argv := range commands {
			// A pane the operator already closed themselves is not an error worth reporting: the
			// conversation is gone either way, which is what was asked for.
			_ = exec.Command(argv[0], argv[1:]...).Run()
		}
		return conversationMsg{}
	}
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
