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
	// The header, the panel's frame and header, the footer, and at least a few rows to look at.
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
	add("Model", m.info.Model)
	add("Sandbox engine", m.info.Sandbox)
	add("Store engine", m.info.Store)
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
// It goes into two columns rather than being cut off when it does not fit, because a list of keys
// with the last few missing is exactly as useful as no list at all.
func (m Model) helpBody() []string {
	height := m.bodyHeight() + 1
	entries := m.helpLines()
	if len(entries) > height {
		entries = intoTwoColumns(entries, m.innerWidth()/2)
	}

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

// intoTwoColumns folds a list in half, side by side, reading down the left column and then down the
// right one.
func intoTwoColumns(entries []string, columnWidth int) []string {
	half := (len(entries) + 1) / 2
	folded := make([]string, 0, half)
	for index := 0; index < half; index++ {
		line := pad(entries[index], columnWidth)
		if right := index + half; right < len(entries) {
			line += entries[right]
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
		return truncate(prompt.Render(":")+m.input+prompt.Render("_"), m.width)
	case modeFilter:
		return truncate(prompt.Render("/")+m.input+prompt.Render("_"), m.width)
	case modeBrowse:
		return truncate(m.breadcrumb(), m.width)
	default:
		return ""
	}
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

	lines = append(lines, "", crumb.Render("  everywhere"))
	for _, pair := range [][2]string{
		{"↑↓ jk", "Move"},
		{"pgup pgdn", "Page"},
		{"enter", "Drill in"},
		{"esc", "Back, or clear the filter"},
		{":", "Switch resource"},
		{"/", "Filter these rows"},
		{"g", "Refresh now"},
		{"?", "This list"},
		{"q", "Quit"},
	} {
		lines = append(lines, "    "+hint(pair[0], pair[1]))
	}
	return append(lines, "", faint.Render("  any key closes this"))
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
