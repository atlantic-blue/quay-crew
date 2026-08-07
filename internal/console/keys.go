package console

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// updateKey routes a keypress to whichever bar has the keyboard.
//
// One read can carry several runes at once. That is what pasting looks like, and what a terminal
// does with keys pressed faster than it drains its buffer, so ":p" can arrive as a single two rune
// message. Fold those through one rune at a time, otherwise they match no binding at all and the
// keystrokes are silently dropped.
func (m Model) updateKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) <= 1 {
		return m.routeKey(msg)
	}
	commands := make([]tea.Cmd, 0, len(msg.Runes))
	for _, r := range msg.Runes {
		next, cmd := m.routeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next
		if cmd != nil {
			commands = append(commands, cmd)
		}
	}
	return m, tea.Batch(commands...)
}

func (m Model) routeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.mode {
	case modeCommand:
		return m.updateCommandKey(msg)
	case modeFilter:
		return m.updateFilterKey(msg)
	case modeBrowse:
		return m.updateBrowseKey(msg)
	case modeConfirm:
		return m.updateConfirmKey(msg)
	case modeWizard:
		return m.updateWizardKey(msg)
	case modeHelp:
		// Any key closes it. Nothing in here acts on anything, so there is nothing to get wrong.
		m.mode = modeBrowse
		return m, nil
	default:
		return m, nil
	}
}

// updateBrowseKey handles the default mode: move, drill, go back, act.
func (m Model) updateBrowseKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case ":":
		m.mode, m.input = modeCommand, ""
		return m, nil
	case "/":
		m.mode, m.input = modeFilter, m.filter
		return m, nil
	case "?":
		m.mode = modeHelp
		return m, nil
	case "n":
		// Making something is not a thing any one view owns, so it is not an action on a row.
		if m.client == nil {
			m.err = fmt.Errorf("this console cannot make anything: it was opened without a crew")
			return m, nil
		}
		m.mode, m.making, m.err = modeWizard, wizard{}, nil
		return m, nil
	case "p":
		// Show the conversation beside the console, or hide the one already there. The panel builds
		// this in one go; this is the same thing without having to decide before opening the console.
		return m.toggleConversation()
	case "r", "g":
		// Refreshing is the key reached for constantly, so it holds the short obvious letter. `g` is
		// what the help has said since the console shipped, so it keeps working.
		return m, listCmd(m.active, m.parent)
	case "enter":
		// Enter descends where there is somewhere to descend to, and otherwise does whatever this
		// view has bound to it. On a list of conversations that is opening the one under the cursor,
		// which is the obvious meaning of the key and the reason it used to do nothing at all.
		if m.active.DrillTo != "" {
			return m.drill()
		}
		return m.act("enter")
	case "esc":
		return m.back()
	}
	if moved, handled := m.move(msg.String()); handled {
		return moved, nil
	}
	return m.act(msg.String())
}

// move handles navigation, returning whether the key was one.
func (m Model) move(key string) (Model, bool) {
	body := m.bodyHeight()
	switch key {
	case "up", "k":
		m.selected--
	case "down", "j":
		m.selected++
	case "pgup", "ctrl+b":
		m.selected -= body
	case "pgdown", "ctrl+f":
		m.selected += body
	case "home":
		m.selected = 0
	case "end":
		m.selected = len(m.visibleRows()) - 1
	default:
		return m, false
	}
	return m.clampSelection(), true
}

// act runs the action bound to key on the selected row, if there is one.
func (m Model) act(key string) (Model, tea.Cmd) {
	row, hasRow := m.selectedRowValue()
	if !hasRow {
		return m, nil
	}
	for _, action := range m.active.Actions {
		if !action.Bound(key) {
			continue
		}
		if action.Confirm {
			m.mode, m.waiting = modeConfirm, pending{action: action, row: row}
			return m, nil
		}
		return m.perform(action, row)
	}
	return m, nil
}

// perform runs an action, whether it was confirmed or never needed to be.
func (m Model) perform(action Action, row Row) (Model, tea.Cmd) {
	if action.Descend != "" {
		return m.descendInto(action.Descend, row)
	}
	if action.Shell != nil {
		return m, shellCmd(action, row)
	}
	if action.Run != nil {
		return m, runCmd(action, row)
	}
	return m, nil
}

// updateConfirmKey answers a destructive key. Cancelling is the default: yes is the only thing that
// acts, and every other key steps back out, because the cost of an accidental cancel is one more
// keypress and the cost of an accidental yes is a conversation.
func (m Model) updateConfirmKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	waiting := m.waiting
	m.mode, m.waiting = modeBrowse, pending{}

	if msg.String() != "y" {
		return m, nil
	}
	return m.perform(waiting.action, waiting.row)
}

// shellCmd suspends the console, hands the terminal to the command, and restores on exit.
func shellCmd(action Action, row Row) tea.Cmd {
	command, err := action.Shell(row)
	if err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	if command == nil {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("%s: nothing to run for %s", action.Label, row.ID)}
		}
	}
	return tea.ExecProcess(command, func(err error) tea.Msg {
		if err != nil || action.After == nil {
			return actionDoneMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := action.After(ctx, row); err != nil {
			return actionDoneMsg{err: fmt.Errorf("%s %s: %w", action.Label, row.Name(), err)}
		}
		return actionDoneMsg{}
	})
}

func runCmd(action Action, row Row) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := action.Run(ctx, row); err != nil {
			return actionDoneMsg{err: fmt.Errorf("%s %s: %w", action.Label, row.ID, err)}
		}
		return actionDoneMsg{}
	}
}

// drill descends into the selected row's child resource, remembering where to come back to.
func (m Model) drill() (Model, tea.Cmd) {
	if m.active.DrillTo == "" {
		return m, nil
	}
	row, hasRow := m.selectedRowValue()
	if !hasRow {
		return m, nil
	}
	return m.descendInto(m.active.DrillTo, row)
}

// descendInto opens a resource scoped to one row, remembering where to come back to. It is what both
// enter and a key bound to Descend do, so escape behaves the same however you got there.
func (m Model) descendInto(name string, row Row) (Model, tea.Cmd) {
	child, found := m.registry.Get(name)
	if !found {
		m.err = fmt.Errorf("console: %s descends into unknown resource %q", m.active.Name, name)
		return m, nil
	}
	m.stack = append(m.stack, crumbEntry{
		resource: m.active.Name, parent: m.parent, selected: m.selected, into: row.Name(),
	})
	m.active, m.parent = child, row.ID
	m.rows, m.selected, m.top, m.filter, m.err = nil, 0, 0, "", nil
	return m, listCmd(m.active, m.parent)
}

// back returns to the view that was drilled down from, with its selection intact. At the top of the
// stack escape clears the filter instead, and clears nothing when there is no filter.
func (m Model) back() (Model, tea.Cmd) {
	if len(m.stack) == 0 {
		if m.filter == "" {
			return m, nil
		}
		m.filter = ""
		return m.clampSelection(), nil
	}
	previous := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]

	resource, found := m.registry.Get(previous.resource)
	if !found {
		m.err = fmt.Errorf("console: cannot go back to unknown resource %q", previous.resource)
		return m, nil
	}
	m.active, m.parent, m.selected = resource, previous.parent, previous.selected
	m.rows, m.top, m.filter, m.err = nil, 0, "", nil
	return m, listCmd(m.active, m.parent)
}

// updateCommandKey handles the command bar: type a resource name or alias, enter to switch.
func (m Model) updateCommandKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode, m.input = modeBrowse, ""
		return m, nil
	case "enter":
		return m.openTyped()
	case "backspace":
		m.input = trimLastRune(m.input)
		return m, nil
	}
	m.input += typedText(msg)
	return m, nil
}

// openTyped switches to the resource named in the command bar.
func (m Model) openTyped() (Model, tea.Cmd) {
	typed := m.input
	m.mode, m.input = modeBrowse, ""

	resource, found := m.registry.Resolve(typed)
	if !found {
		m.err = fmt.Errorf("no resource %q, try one of %v", typed, m.registry.Tokens())
		return m, nil
	}
	if resource.Name == m.active.Name && m.parent == "" {
		return m, listCmd(m.active, m.parent)
	}
	// Switching resource by name is a jump, not a descent, so the breadcrumb stack resets.
	m.active, m.parent, m.stack = resource, "", nil
	m.rows, m.selected, m.top, m.filter, m.err = nil, 0, 0, "", nil
	return m, listCmd(m.active, m.parent)
}

// updateFilterKey handles the filter bar. Filtering is live: every keystroke narrows the rows.
func (m Model) updateFilterKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode, m.input, m.filter = modeBrowse, "", ""
		return m.clampSelection(), nil
	case "enter":
		m.mode, m.input = modeBrowse, ""
		return m, nil
	case "backspace":
		m.input = trimLastRune(m.input)
		m.filter = m.input
		return m.clampSelection(), nil
	}
	m.input += typedText(msg)
	m.filter = m.input
	return m.clampSelection(), nil
}

// typedText is the printable text a keypress contributes, and empty for anything else, so a stray
// function key does not end up inside a filter.
func typedText(msg tea.KeyMsg) string {
	if msg.Type == tea.KeySpace {
		return " "
	}
	if msg.Type != tea.KeyRunes {
		return ""
	}
	return string(msg.Runes)
}

func trimLastRune(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}
