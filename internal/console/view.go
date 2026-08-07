package console

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View draws the console: a status block saying which crew this is and what the keys do, a framed
// panel of rows titled with its scope and count, and a breadcrumb saying where escape goes back to.
//
// The order answers the two questions in the order they are asked: which crew am I about to act on,
// and what can I press.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	visible := m.visibleRows()

	lines := m.headerLines()
	if m.mode == modeHelp {
		lines = append(lines, m.panelTop(len(visible)))
		for _, line := range m.helpBody() {
			lines = append(lines, m.framed(line))
		}
		lines = append(lines, m.panelBottom())
		if m.err != nil {
			lines = append(lines, alert.Render(truncate(m.err.Error(), m.width)))
		}
		return strings.Join(append(lines, m.footer()), "\n")
	}

	lines = append(lines, m.panelTop(len(visible)), m.framed(m.columnHeader()))
	for _, line := range m.bodyLines(visible) {
		lines = append(lines, m.framed(line))
	}
	lines = append(lines, m.panelBottom())
	if m.err != nil {
		lines = append(lines, alert.Render(truncate(m.err.Error(), m.width)))
	}
	lines = append(lines, m.footer())
	return strings.Join(lines, "\n")
}

// headerLines is the status block with the key hints beside it: what this crew is on the left, what
// the keyboard does on the right.
func (m Model) headerLines() []string {
	if m.headless {
		return nil
	}
	status := m.statusLines()
	hints := m.hintLines()

	widest := 0
	for _, line := range status {
		if width := lipgloss.Width(line); width > widest {
			widest = width
		}
	}

	// The block is as tall as the tallest of them: neither the crew's description nor the keys
	// available may be cut off because the other one is shorter.
	height := len(status)
	if len(hints) > height {
		height = len(hints)
	}

	lines := make([]string, 0, height)
	for index := 0; index < height; index++ {
		left := ""
		if index < len(status) {
			left = status[index]
		}
		combined := " " + pad(left, widest+3)
		if index < len(hints) {
			combined += hints[index]
		}
		lines = append(lines, truncate(combined, m.width))
	}
	return m.withLogo(lines)
}

// logo is the wordmark, drawn on the right of the header the way k9s draws its own.
// everywhereKeys are the keys that work in every view. One list, read by the overlay behind the
// question mark and by the keys view, because two lists of the same keys drift and the one nobody is
// looking at drifts first.
var everywhereKeys = [][2]string{
	{"↑↓ jk", "Move"},
	{"pgup pgdn", "Page"},
	{"enter", "Drill in"},
	{"esc", "Back, or clear the filter"},
	{":", "Switch resource"},
	{"/", "Filter these rows"},
	{"r g", "Refresh now"},
	{"n", "Make one thing"},
	{"p", "Show or hide the conversation beside this"},
	// The one key here that is not the console's own. An open conversation runs inside tmux in its
	// sandbox, so this leaves it running and comes back; without it the only way out of a thread is
	// ending it, which is what everybody does until somebody tells them otherwise.
	{"ctrl-q", "Leave a conversation running"},
	{"?", "This list"},
	{"q", "Quit"},
}

var logo = []string{
	"  ██████  ██    ██  █████  ██    ██",
	" ██    ██ ██    ██ ██   ██  ██  ██ ",
	" ██    ██ ██    ██ ███████   ████  ",
	" ██ ▄▄ ██ ██    ██ ██   ██    ██   ",
	"  ██████   ██████  ██   ██    ██   ",
	"     ▀▀                            ",
}

// withLogo puts the wordmark against the right edge, growing the header to fit it when the header is
// shorter, which it is against a control plane too old to say what it is running.
//
// It gives way rather than pushing: on a window too narrow to hold it beside the status block, or too
// short to spare the rows, the rows matter more than the branding and the mark is simply not drawn.
func (m Model) withLogo(lines []string) []string {
	room := 0
	for _, line := range lines {
		if width := lipgloss.Width(line); width > room {
			room = width
		}
	}
	if m.width-room-2 < lipgloss.Width(logo[0]) {
		return lines
	}
	// The header, the panel's frame and header, the footer, and at least a few rows to look at. The
	if m.height-len(logo)-4 < 3 {
		return lines
	}

	for len(lines) < len(logo) {
		lines = append(lines, "")
	}
	for index := range logo {
		lines[index] = pad(lines[index], m.width-lipgloss.Width(logo[index])) + mark.Render(logo[index])
	}
	return lines
}

// statusLines is the crew this console is pointed at: where it is, what a turn would run in, and
// whether any of it survives a restart. Anything the control plane did not say is left out rather
// than guessed at.
func (m Model) statusLines() []string {
	lines := make([]string, 0, 6)
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, statusKey.Render(pad(key+":", 16))+value)
		}
	}
	add("Version", m.info.Version)
	add("Address", m.info.Address)
	add("Workspace", m.info.Workspace)
	add("Project", m.info.Project)
	// Also in the :stats view, which is where to read them when a conversation beside the console has
	// squeezed the header. They stay here because this is the header the operator asked to keep.
	add("Model", m.info.Model)
	add("Sandbox engine", m.info.Sandbox)
	add("Store engine", m.info.Store)
	add("Secrets", secretsPhrase(m.info.Secrets))
	if m.info.Store != "" {
		add("Events engine", eventsPhrase(m.info.Events))
		add("State", statePhrase(m.info.State))
	}
	if m.info.Behind {
		add("Quay", alert.Render("this control plane is older than the tool, run make upgrade"))
	}
	if len(lines) == 0 {
		lines = append(lines, statusKey.Render(pad("Quay:", 16))+faint.Render("asking what this control plane is running"))
	}
	return lines
}

// statePhrase says where a conversation is kept. Empty means nowhere: it lives in the container and
// dies with it, which is worth saying in red because it is the difference between a thread you can
// come back to and one you cannot.
func statePhrase(where string) string {
	if where == "" {
		return alert.Render("in the container, lost when it is replaced")
	}
	return where
}

// secretsPhrase says where a workspace's credentials are kept, and says the cost out loud when they
// are kept nowhere: the subscription token goes with every restart, and the turn that then fails says
// nothing about why.
func secretsPhrase(where string) string {
	if strings.HasPrefix(where, "memory") {
		return alert.Render(where)
	}
	return where
}

// eventsPhrase names the event log, and says so plainly when nothing is connected to it rather than
// letting an empty column read as "fine".
func eventsPhrase(engine string) string {
	if engine == "" {
		return faint.Render("none, nothing reads or writes the log yet")
	}
	return engine
}

// trail is what was drilled through to get here, named rather than identified.
func (m Model) trail() []string {
	names := make([]string, 0, len(m.stack))
	for _, entry := range m.stack {
		if entry.into != "" {
			names = append(names, entry.into)
		}
	}
	return names
}

// hintLines is what the keys do here, in aligned columns beside the status block: one hint per line,
// filling a column top to bottom before starting the next, the way k9s lays them out. Reading down a
// column is how you find a key; reading along a wrapped row is not.
//
// The view's own actions come first, because they are the reason the operator is looking at this
// view rather than another one.
func (m Model) hintLines() []string {
	hints := m.hintParts()
	if len(hints) == 0 {
		return nil
	}

	// Tall enough to read, matching the status block when that is taller so the two end together,
	// and never so tall that the rows have nowhere left to go.
	height := len(m.statusLines())
	if height < 4 {
		height = 4
	}
	if height > len(hints) {
		height = len(hints)
	}
	if limit := m.height / 3; limit > 0 && height > limit {
		height = limit
	}
	if height < 1 {
		height = 1
	}

	columns := make([][]string, 0, (len(hints)+height-1)/height)
	for start := 0; start < len(hints); start += height {
		end := start + height
		if end > len(hints) {
			end = len(hints)
		}
		columns = append(columns, hints[start:end])
	}

	lines := make([]string, height)
	for _, column := range columns {
		widest := 0
		for _, cell := range column {
			if width := lipgloss.Width(cell); width > widest {
				widest = width
			}
		}
		for row := 0; row < height; row++ {
			cell := ""
			if row < len(column) {
				cell = column[row]
			}
			lines[row] += pad(cell, widest+3)
		}
	}
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " ")
	}
	return lines
}

// panelTop is the framed panel's top edge, titled with the resource, its scope and its count, so
// both are visible without counting rows: sessions(house-bills)[3].
func (m Model) panelTop(count int) string {
	title := m.active.Name
	if m.mode == modeHelp {
		title = "help(" + m.active.Name + ")"
		return m.titledEdge(title)
	}
	// The nearest thing drilled through, not the whole path: the path is already in the status
	// block, and the title is answering "these rows are which ones".
	if trail := m.trail(); len(trail) > 0 {
		title += "(" + trail[len(trail)-1] + ")"
	}
	title += fmt.Sprintf("[%d]", count)
	if m.filter != "" {
		title += " filter " + m.filter
	}
	return m.titledEdge(title)
}

// titledEdge draws the panel's top edge with its title centred, which is where k9s puts it and where
// the eye goes first.
func (m Model) titledEdge(title string) string {
	labelled := " " + crumb.Render(title) + " "
	rule := m.width - 2 - lipgloss.Width(labelled)
	if rule < 0 {
		return frame.Render("╭" + strings.Repeat("─", max(m.width-2, 0)) + "╮")
	}
	left := rule / 2
	return frame.Render("╭"+strings.Repeat("─", left)) + labelled +
		frame.Render(strings.Repeat("─", rule-left)+"╮")
}

// max is the larger of two, which the standard library only grew for floats.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m Model) panelBottom() string {
	width := m.width - 2
	if width < 0 {
		width = 0
	}
	return frame.Render("╰" + strings.Repeat("─", width) + "╯")
}

// helpBody is the key list, padded to the same height the rows would have taken, so opening it does
// not resize the window's contents underneath.
//
// It goes into as many columns as it takes rather than being cut off, because a list of keys with the
// last few missing is exactly as useful as no list at all, and worse than that: it is a list that
// looks complete. Adding one binding used to push the last one off the bottom with nothing to say so.
func (m Model) helpBody() []string {
	height := m.bodyHeight() + 1
	entries := intoColumns(m.helpLines(), height, m.innerWidth())

	lines := make([]string, 0, height)
	for _, line := range entries {
		if len(lines) == height {
			break
		}
		lines = append(lines, m.fit(line))
	}
	for len(lines) < height {
		lines = append(lines, m.fit(""))
	}
	return lines
}

// intoColumns folds a list into however many columns fit in height lines, reading down each column
// before starting the next. A list that already fits is left alone.
//
// The columns are as wide as the widest entry rather than an even share of the room, because an even
// share cuts whatever is longer than it and a key list with its last few characters missing is a key
// list that lies. Fewer columns and everything readable beats more columns and an ellipsis.
func intoColumns(entries []string, height, width int) []string {
	if height < 1 || len(entries) <= height {
		return entries
	}
	widest := 0
	for _, entry := range entries {
		if w := lipgloss.Width(entry); w > widest {
			widest = w
		}
	}

	const gap = 2
	columns := 1
	if widest+gap > 0 {
		columns = width / (widest + gap)
	}
	if columns < 1 {
		columns = 1
	}
	if needed := (len(entries) + height - 1) / height; columns > needed {
		columns = needed
	}
	perColumn := (len(entries) + columns - 1) / columns

	folded := make([]string, 0, perColumn)
	for index := 0; index < perColumn; index++ {
		line := ""
		for column := 0; column < columns; column++ {
			at := column*perColumn + index
			if at >= len(entries) {
				break
			}
			if column == columns-1 {
				line += entries[at]
				break
			}
			line += pad(entries[at], widest+gap)
		}
		folded = append(folded, line)
	}
	return folded
}

// framed puts one line inside the panel's sides.
func (m Model) framed(line string) string {
	return frame.Render("│") + line + frame.Render("│")
}

// innerWidth is the room inside the panel's sides.
func (m Model) innerWidth() int {
	if m.width < 4 {
		return 1
	}
	return m.width - 2
}

// columnHeader is the black on green title bar, marked with an arrow on the column the rows are
// ordered by, because an order you cannot see is an order you cannot trust.
func (m Model) columnHeader() string {
	return headerBar.Render(m.fit(m.renderCells(titles(m.active.Columns, m.active.SortBy))))
}

// fit makes a line exactly the width inside the panel: padded when it is short, cut when it is long.
// Cutting matters on a narrow window, where the columns a resource declares add up to more than the
// room available and an overflowing line wraps, which costs a row and pushes the top of the view off
// the screen. It cuts before any styling is applied, because slicing a styled string cuts through
// the escape codes in it.
func (m Model) fit(text string) string {
	return pad(truncate(text, m.innerWidth()), m.innerWidth())
}

func titles(columns []Column, sortedBy int) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = strings.ToUpper(column.Title)
		if i == sortedBy {
			out[i] += "↑"
		}
	}
	return out
}

// bodyLines renders the visible slice of rows, padding to a fixed height so the footer does not
// jump around as the list grows and shrinks.
func (m Model) bodyLines(visible []Row) []string {
	body := m.bodyHeight()
	lines := make([]string, 0, body)

	if len(visible) == 0 {
		lines = append(lines, faint.Render(m.fit("  nothing here")))
	}
	for index := m.top; index < len(visible) && len(lines) < body; index++ {
		lines = append(lines, m.rowLine(visible[index], index == m.selected))
	}
	for len(lines) < body {
		lines = append(lines, strings.Repeat(" ", m.innerWidth()))
	}
	return lines
}

func (m Model) rowLine(row Row, isSelected bool) string {
	// Sized to the full width inside the panel, so the cursor is a bar across the row rather than a
	// highlight around the text that happens to be in it.
	text := m.fit(m.renderCells(row.Cells))
	if isSelected {
		return selectedRow.Render(text)
	}
	return styleFor(row.State).Render(text)
}

// renderCells lays cells out in the active resource's columns. A zero width column takes whatever
// space is left, and at most one should have it.
func (m Model) renderCells(cells []string) string {
	columns := m.active.Columns
	fixed := 0
	for _, column := range columns {
		fixed += column.Width
	}
	flex := m.innerWidth() - fixed - len(columns)
	if flex < 8 {
		flex = 8
	}

	parts := make([]string, 0, len(columns))
	for index, column := range columns {
		cell := ""
		if index < len(cells) {
			cell = cells[index]
		}
		width := column.Width
		if width == 0 {
			width = flex
		}
		parts = append(parts, pad(truncate(cell, width), width))
	}
	return strings.Join(parts, " ")
}

// footer is the command bar, the filter bar, or the breadcrumb.
func (m Model) footer() string {
	switch m.mode {
	case modeCommand:
		return truncate(prompt.Render(":")+m.input+prompt.Render("_")+m.offered(), m.width)
	case modeFilter:
		return truncate(prompt.Render("/")+m.input+prompt.Render("_"), m.width)
	case modeConfirm:
		return truncate(m.confirmPrompt(), m.width)
	case modeWizard:
		return truncate(m.wizardPrompt(), m.width)
	case modeBrowse:
		return truncate(m.breadcrumb(), m.width)
	default:
		return ""
	}
}

// confirmPrompt names the thing about to be acted on, so a yes is a yes to something in particular
// rather than to whatever the cursor happened to be over.
func (m Model) confirmPrompt() string {
	return prompt.Render(" ") + alert.Render(strings.ToLower(m.waiting.action.Label)+" "+
		m.active.One()+" "+m.waiting.row.Name()+"?") + faint.Render("  y to confirm, any other key cancels")
}

// offered is what the command bar could open, narrowed by what has been typed. Pressing colon used to
// ask a question with nothing to answer it from, so the only way to learn a view's name was to know
// it already.
func (m Model) offered() string {
	if m.registry == nil {
		return ""
	}
	names := m.registry.Offer(m.input)
	if len(names) == 0 {
		return faint.Render("   nothing called that")
	}
	return faint.Render("   " + strings.Join(names, "  "))
}

// wizardPrompt asks the step's question and shows what has been typed, which for a secret is asterisks
// and nothing else.
//
// A step that chooses from what the crew already has shows what there is, narrowed by what has been
// typed, the same way the command bar does. Asking somebody to name a workspace they cannot see is how
// the wizard ended up only able to make new ones.
func (m Model) wizardPrompt() string {
	if m.making.step() == stepWorking {
		return prompt.Render(" making ") + faint.Render(m.making.summary())
	}
	line := prompt.Render(" "+m.making.prompt()+": ") + m.making.shown() + prompt.Render("_")
	if offers := m.making.offers(); len(offers) > 0 {
		line += faint.Render("   " + strings.Join(offers, "  "))
	} else if m.making.picking() && m.making.loaded {
		line += faint.Render("   nothing here yet")
	}
	return line + faint.Render("   enter accepts, esc cancels and makes nothing")
}

// breadcrumb is the drill path with the view you are in as a chip, so "me > house-bills <sessions>"
// says both where you are and what escape goes back to.
func (m Model) breadcrumb() string {
	line := " "
	if trail := m.trail(); len(trail) > 0 {
		line += faint.Render(strings.Join(trail, " > ")) + " "
	}
	line += chip.Render("<" + m.active.Name + ">")
	if len(m.stack) > 0 {
		line += faint.Render("   esc to go back")
	}
	return line
}

// hintParts is what the header shows: this view's own commands, and the key that lists the rest.
//
// Only this view's commands, deliberately. k9s puts the view's verbs up here and everything else
// behind a question mark, and a header that lists every key teaches the operator to stop reading it.
func (m Model) hintParts() []string {
	parts := make([]string, 0, len(m.active.Actions)+3)
	for _, action := range m.active.Actions {
		parts = append(parts, hint(action.Key, action.Label))
	}
	if m.active.DrillTo != "" {
		parts = append(parts, hint("enter", m.active.DrillTo))
	}
	if len(m.stack) > 0 || m.filter != "" {
		parts = append(parts, hint("esc", "Back"))
	}
	return append(parts, hint("?", "Help"))
}

// helpLines is every key, this view's own first and then the ones that work everywhere. This is what
// the question mark opens, and it is the only place the full list lives.
func (m Model) helpLines() []string {
	lines := make([]string, 0, len(m.active.Actions)+12)
	lines = append(lines, crumb.Render("  "+m.active.Name))
	for _, action := range m.active.Actions {
		// Every spelling, not just the primary one, because this is where somebody looks for the key
		// they used to press.
		lines = append(lines, "    "+hint(strings.Join(action.Keys(), " "), action.Label))
	}
	if m.active.DrillTo != "" {
		lines = append(lines, "    "+hint("enter", "Drill into "+m.active.DrillTo))
	}

	// Every view, with what to type for it. The command bar cannot be used by somebody who does not
	// already know what is in there.
	lines = append(lines, "", crumb.Render("  views, with :"))
	for _, name := range m.registry.Names() {
		// The name and the shortest way to type it. Every spelling would make one entry wider than the
		// column and push what follows off the edge, and the command bar offers them all anyway.
		lines = append(lines, "    "+hint(strings.Join(shortestSpelling(m.registry.Spellings(name)), " "), name))
	}

	lines = append(lines, "", crumb.Render("  everywhere"))
	for _, pair := range everywhereKeys {
		lines = append(lines, "    "+hint(pair[0], pair[1]))
	}
	return append(lines, "", faint.Render("  any key closes this"))
}

// shortestSpelling is a resource's name and the briefest alias for it, which is what somebody wants
// from a list they are reading to find out what to type.
func shortestSpelling(spellings []string) []string {
	if len(spellings) < 2 {
		return spellings
	}
	shortest := spellings[1]
	for _, spelling := range spellings[1:] {
		if len(spelling) < len(shortest) {
			shortest = spelling
		}
	}
	return []string{spellings[0], shortest}
}

// pad right pads to a display width, leaving anything already wider alone.
func pad(text string, width int) string {
	gap := width - lipgloss.Width(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
}

// truncate cuts to a display width, marking the cut with a single character so a clipped identifier
// does not read as a complete one.
func truncate(text string, width int) string {
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
