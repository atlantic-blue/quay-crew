package console

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
	// Ctrl+c quits, from every mode, before any of them see it. It used to be a second escape inside
	// the command bar, the filter and the wizard, which meant leaving a bar and leaving the console
	// were the same key: the first press dropped you back to browsing and only the second quit. The
	// command bar is the one way in, so that was most presses. Escape is the key that cancels a mode.
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	switch m.mode {
	case modeCommand:
		return m.updateCommandKey(msg)
	case modeFilter:
		return m.updateFilterKey(msg)
	case modeBrowse:
		return m.updateBrowseKey(msg)
	case modeConfirm:
		return m.updateConfirmKey(msg)
	case modeChoose:
		return m.updateChooseKey(msg)
	case modeType:
		return m.updateTypeKey(msg)
	case modeWizard:
		return m.updateWizardKey(msg)
	case modeReading:
		return m.updateReadingKey(msg)
	case modeHelp:
		// Moving scrolls it, because it is taller than a short window. Any other key closes it, and
		// nothing in here acts on anything, so there is nothing to get wrong.
		switch msg.String() {
		case "up", "k":
			m.helpTop--
		case "down", "j":
			m.helpTop++
		case "pgup", "ctrl+b":
			m.helpTop -= m.bodyHeight()
		case "pgdown", "ctrl+f":
			m.helpTop += m.bodyHeight()
		default:
			m.mode, m.helpTop = modeBrowse, 0
			return m, nil
		}
		if m.helpTop < 0 {
			m.helpTop = 0
		}
		return m, nil
	default:
		return m, nil
	}
}

// updateBrowseKey handles the default mode: move, drill, go back, act.
//
// The keys read as vim reads them, because the console is shaped like k9s and k9s is shaped like vim:
// the motion keys move and nothing else, an action never sits on one, and a key that moved says what
// to press now rather than doing nothing.
func (m Model) updateBrowseKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	key := msg.String()
	if m.pendingHalf != "" {
		return m.afterPending(msg)
	}
	if typing, isDigit := m.takeDigit(key); isDigit {
		return typing, nil
	}
	// Every key that is not a digit ends the count, and each one below reads how many it was.
	count := m.count()
	m.counted = ""

	switch key {
	case "q":
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
	case "g":
		// Half of gg, held until the other half arrives. It used to refresh, and holding it here is
		// what lets the first and last row have the keys they have everywhere else.
		m.pendingHalf, m.counted = "g", countText(count)
		return m, nil
	case "n", "N":
		// Next and previous match, the keys pressed straight after a search in vim. This console
		// filters rather than searching, so what they jump through is what the filter last matched,
		// which is worth having the moment the filter itself is cleared.
		return m.jumpToMatch(key, count)
	case "o":
		// Making something is not a thing any one view owns, so it is not an action on a row. Vim
		// opens a new line with o, which is the closest thing it has to making one.
		if m.client == nil {
			m.err = fmt.Errorf("this console cannot make anything: it was opened without a system")
			return m, nil
		}
		m.mode, m.making, m.err = modeWizard, wizard{}, nil
		return m, nil
	case "P":
		// A fresh conversation in place of the one beside the console. Opening the system comes back to
		// the one you were in, which is what you want almost always and not quite always. Beside `p`,
		// which shows and hides that same conversation, so the pair reads as one subject.
		return m.startFreshConversation()
	case "p":
		// Show the conversation beside the console, or hide the one already there. `krewe` opens the
		// console alone, so this key is the only way one arrives beside it.
		return m.toggleConversation()
	case "r":
		// Refreshing is the key reached for constantly, so it holds the short obvious letter.
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
	if moved, handled := m.move(key, count); handled {
		return moved, nil
	}
	return m.act(key)
}

// takeDigit collects a count typed in front of a move, so 5j moves five rows. A zero with nothing in
// front of it is not a count: it is the only digit that could begin one and mean nothing.
func (m Model) takeDigit(key string) (Model, bool) {
	if len(key) != 1 || key < "0" || key > "9" {
		return m, false
	}
	if key == "0" && m.counted == "" {
		return m, false
	}
	// A count long enough to overflow is a key held down rather than an intention.
	if len(m.counted) < 4 {
		m.counted += key
	}
	return m, true
}

// count is how many times the next key should happen, and zero when no count was typed. Zero rather
// than one, because a move that lands on a row by number has to tell "5G" from "G".
func (m Model) count() int {
	times := 0
	for _, digit := range m.counted {
		times = times*10 + int(digit-'0')
	}
	return times
}

// countText writes a count back out, for the half of a two key sequence that has to carry it.
func countText(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d", count)
}

// afterPending answers the second half of a two key sequence. The only sequence is gg, and anything
// else after a g is the operator reaching for the refresh that g used to be, so it says where refresh
// went and then does whatever the key they pressed does.
func (m Model) afterPending(msg tea.KeyMsg) (Model, tea.Cmd) {
	count := m.count()
	m.pendingHalf, m.counted = "", ""

	if msg.String() == "g" {
		m.selected = rowNumbered(count, 0)
		return m.clampSelection(), nil
	}
	m.err, m.held = fmt.Errorf("g does not refresh any more: r refreshes, and gg goes to the first row"), true
	return m.updateBrowseKey(msg)
}

// move handles navigation, returning whether the key was one. count is how many times, and zero is
// once: a move typed with no count in front of it.
func (m Model) move(key string, count int) (Model, bool) {
	body, times := m.bodyHeight(), count
	if times == 0 {
		times = 1
	}
	switch key {
	case "up", "k":
		m.selected -= times
	case "down", "j":
		m.selected += times
	case "pgup", "ctrl+b":
		m.selected -= times * body
	case "pgdown", "ctrl+f":
		m.selected += times * body
	case "ctrl+u":
		m.selected -= times * halfOf(body)
	case "ctrl+d":
		m.selected += times * halfOf(body)
	case "home":
		m.selected = 0
	case "end", "G":
		m.selected = rowNumbered(count, len(m.visibleRows())-1)
	default:
		return m, false
	}
	return m.clampSelection(), true
}

// rowNumbered is the row a counted jump lands on, counting from one the way an editor numbers lines,
// and fallback when no count was typed.
func rowNumbered(count, fallback int) int {
	if count == 0 {
		return fallback
	}
	return count - 1
}

// halfOf is half a page, and never nothing: a half page key on a window with two rows in it still
// has to move.
func halfOf(body int) int {
	if body < 2 {
		return 1
	}
	return body / 2
}

// jumpToMatch moves to the next row matching what the filter was last typed with, wrapping at the
// ends the way n does in vim.
//
// With nothing filtered for yet it says so, and names the key that used to be here: pressing one of
// these out of the blue is the operator reaching for what it used to do.
func (m Model) jumpToMatch(key string, count int) (Model, tea.Cmd) {
	forward := key == "n"
	if m.search == "" {
		m.err, m.held = movedToMatching(key), true
		return m, nil
	}
	visible := m.visibleRows()
	if len(visible) == 0 {
		return m, nil
	}
	step := 1
	if !forward {
		step = -1
	}
	times := count
	if times == 0 {
		times = 1
	}
	at := m.selected
	for range times {
		next, found := nextMatch(visible, at, step, strings.ToLower(m.search))
		if !found {
			m.err, m.held = fmt.Errorf("no other row matches %q", m.search), true
			return m, nil
		}
		at = next
	}
	m.selected = at
	return m.clampSelection(), nil
}

// movedToMatching says what these two keys do now, and where what they used to do has gone. n made a
// thing and N started a fresh conversation, and both are in somebody's fingers.
func movedToMatching(key string) error {
	if key == "n" {
		return fmt.Errorf(
			"nothing has been filtered for yet: n jumps to the next match of what / filtered for, and o makes something")
	}
	return fmt.Errorf(
		"nothing has been filtered for yet: N jumps to the previous match of what / filtered for, " +
			"and P starts a fresh conversation")
}

// nextMatch is the row the search lands on next, walking in one direction and wrapping. The row it
// started on is not an answer: a jump that lands where it already was reads as a key doing nothing.
func nextMatch(rows []Row, from, step int, needle string) (int, bool) {
	for away := 1; away < len(rows); away++ {
		at := ((from+away*step)%len(rows) + len(rows)) % len(rows)
		if rowMatches(rows[at], needle) {
			return at, true
		}
	}
	return 0, false
}

// act runs the action bound to key on the selected row, if there is one.
func (m Model) act(key string) (Model, tea.Cmd) {
	// A key this view used to answer to says what to press now, before anything else and whether or
	// not there is a row under the cursor. A key that quietly stopped working is the regression this
	// console has already had once.
	for _, action := range m.active.Actions {
		if !action.WasBound(key) {
			continue
		}
		m.err, m.held = fmt.Errorf("%s is not bound any more: %s is on %s",
			key, strings.ToLower(action.Label), action.Key), true
		return m, nil
	}
	// A key that acts on the view's scope is answered before the cursor is read, because the case it
	// exists for is a listing with nothing in it to put a cursor on.
	for _, action := range m.active.Actions {
		if action.OnScope && action.Bound(key) {
			return m.performOnScope(action)
		}
	}
	row, hasRow := m.selectedRowValue()
	if !hasRow {
		return m, nil
	}
	for _, action := range m.active.Actions {
		if !action.Bound(key) {
			continue
		}
		if action.RunTyped != nil {
			m.mode, m.typing, m.input = modeType, pending{action: action, row: row}, ""
			if action.Typed != nil {
				m.input = action.Typed(row)
			}
			return m, nil
		}
		if len(action.Offers) > 0 {
			m.mode, m.choosing = modeChoose, choice{action: action, row: row, at: 0}
			return m, nil
		}
		if action.Confirm && (action.Costs == nil || action.Costs(row)) {
			m.mode, m.waiting = modeConfirm, pending{action: action, row: row}
			return m, nil
		}
		return m.perform(action, row)
	}
	return m, nil
}

// perform runs an action, whether it was confirmed or never needed to be.
func (m Model) perform(action Action, row Row) (Model, tea.Cmd) {
	if action.Folds {
		// Nothing is asked for. The parts are rows of the listing already on screen, so opening one
		// is a redraw rather than a call.
		return m.fold(row), nil
	}
	if action.Descend != "" {
		return m.descendInto(action.Descend, row)
	}
	if action.Conversation {
		if next, cmd, opened := m.openConversationFor(row); opened {
			return next, cmd
		}
	}
	if action.Reads != nil {
		return m.showReading(m.active.One()+" "+row.Typed(), action.Reads(row)), nil
	}
	if action.Shell != nil {
		return m, m.shellCmd(action, row)
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
	if waiting.chosen != "" {
		return m, chosenCmd(waiting.action, waiting.row, waiting.chosen)
	}
	return m.perform(waiting.action, waiting.row)
}

// shellCmd suspends the console, hands the terminal to the command, and restores on exit.
func (m Model) shellCmd(action Action, row Row) tea.Cmd {
	command, err := action.Shell(row)
	if err != nil {
		return func() tea.Msg { return heldErrMsg{err: err} }
	}
	if command == nil {
		return func() tea.Msg {
			return heldErrMsg{err: fmt.Errorf("%s: nothing to run for %s", action.Label, row.ID)}
		}
	}
	return m.handOver(command, func(err error) tea.Msg {
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

// handOver gives the terminal to a command and reports what came back.
//
// A seam rather than a call to the terminal library, because the library's own version is answered by
// the program loop and never runs anything a test can see. Without it a scenario can only assert on
// the command the key produced, which is the half that always worked: the half that did not is what
// the operator is left looking at once the command has run.
func (m Model) handOver(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
	if m.terminal != nil {
		return m.terminal(command, done)
	}
	return tea.ExecProcess(command, done)
}

// WithTerminal says how the console hands the terminal to a command it starts.
func (m Model) WithTerminal(handOver Terminal) Model {
	m.terminal = handOver
	return m
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
	// What the child is scoped by, which is the row itself everywhere except jobs: a job descends
	// into its session's tasks, and a job with no session yet says so rather than opening an empty
	// listing under a heading that promises one.
	scope := row.ID
	if m.active.DrillBy != nil {
		narrowed, err := m.active.DrillBy(row)
		if err != nil {
			m.err = err
			return m, nil
		}
		scope = narrowed
	}
	m.stack = append(m.stack, crumbEntry{
		resource: m.active.Name, parent: m.parent, selected: m.selected,
		into: row.Name(), row: row.ID, typed: row.Typed(),
	})
	m.active, m.parent = child, scope
	m.rows, m.summary, m.selected, m.top, m.filter, m.err = nil, summary{}, 0, 0, "", nil
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
	m.rows, m.summary, m.top, m.filter, m.err = nil, summary{}, 0, "", nil
	return m, listCmd(m.active, m.parent)
}

// updateCommandKey handles the command bar: type a resource name or alias, enter to switch.
func (m Model) updateCommandKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.input = modeBrowse, ""
		return m, nil
	case "enter":
		return m.runTyped()
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
	m.rows, m.summary, m.selected, m.top, m.filter, m.err = nil, summary{}, 0, 0, "", nil
	return m, listCmd(m.active, m.parent)
}

// updateFilterKey handles the filter bar. Filtering is live: every keystroke narrows the rows.
//
// What was typed is remembered as the search, and escape does not take it with it. Escape puts every
// row back on screen, which is the moment the keys for the next and previous match are worth
// anything: until now the word was thrown away and there was nothing to jump through.
func (m Model) updateFilterKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.input, m.filter = modeBrowse, "", ""
		return m.clampSelection(), nil
	case "enter":
		m.mode, m.input = modeBrowse, ""
		return m, nil
	case "backspace":
		m.input = trimLastRune(m.input)
		m.filter, m.search = m.input, m.input
		return m.clampSelection(), nil
	}
	m.input += typedText(msg)
	m.filter, m.search = m.input, m.input
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

// updateChooseKey moves the cursor through what a key offers, and picks one.
//
// A pick that gives the row more than it has asks first, the way every widening key in the console
// does. A pick that takes something away does not: there is nothing to be careful about, and asking
// would only be in the way.
func (m Model) updateChooseKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	offers := m.choosing.action.Offers
	switch msg.String() {
	case "esc", "q":
		m.mode, m.choosing = modeBrowse, choice{}
		return m, nil
	case "up", "k", "shift+tab":
		m.choosing.at = (m.choosing.at - 1 + len(offers)) % len(offers)
		return m, nil
	case "down", "j", "tab":
		m.choosing.at = (m.choosing.at + 1) % len(offers)
		return m, nil
	case "enter":
		chosen := offers[m.choosing.at]
		action, row := m.choosing.action, m.choosing.row
		m.mode, m.choosing = modeBrowse, choice{}
		if action.Confirm && (action.Widens == nil || action.Widens(row, chosen)) {
			m.mode, m.waiting = modeConfirm, pending{action: action, row: row, chosen: chosen}
			return m, nil
		}
		return m, chosenCmd(action, row, chosen)
	}
	return m, nil
}

// chosenCmd runs what was picked, off the keyboard, so a slow system does not hold the console still.
// The same shape as runCmd, because a pick is a run that happens to carry an answer.
func chosenCmd(action Action, row Row, chosen string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := action.RunChosen(ctx, row, chosen); err != nil {
			return actionDoneMsg{err: fmt.Errorf("%s %s: %w", action.Label, row.ID, err)}
		}
		return actionDoneMsg{}
	}
}

// updateTypeKey collects a line of text for the selected row.
//
// Enter applies it, including when it is empty: an empty name is how a name is cleared, and a key
// that refused to clear would leave the operator with no way back to the identifier. Escape is how
// nothing happens, which is why clearing has to be enter rather than escape.
func (m Model) updateTypeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.typing, m.input = modeBrowse, pending{}, ""
		return m, nil
	case "enter":
		typed, action, row := m.input, m.typing.action, m.typing.row
		m.mode, m.typing, m.input = modeBrowse, pending{}, ""
		return m, typedCmd(action, row, typed)
	case "backspace":
		m.input = trimLastRune(m.input)
		return m, nil
	}
	m.input += typedText(msg)
	return m, nil
}

// typedCmd applies a typed line, off the keyboard, the same shape as every other action.
func typedCmd(action Action, row Row, typed string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := action.RunTyped(ctx, row, typed); err != nil {
			return actionDoneMsg{err: fmt.Errorf("%s %s: %w", action.Label, row.ID, err)}
		}
		return actionDoneMsg{}
	}
}

// performOnScope runs a key that acts on what the view is scoped to. The scope travels as a row so
// every kind of action reads it the same way, and a view opened with no scope at all says so rather
// than acting on nothing.
func (m Model) performOnScope(action Action) (Model, tea.Cmd) {
	if m.parent == "" {
		m.err, m.held = fmt.Errorf("%s has nothing to act on: this view was opened on its own rather than "+
			"from the row above it", strings.ToLower(action.Label)), true
		return m, nil
	}
	return m.perform(action, Row{ID: m.parent})
}
