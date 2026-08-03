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

	// The block is as tall as the taller of the two: neither the crew's description nor the keys
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
	return lines
}

// statusLines is the crew this console is pointed at: where it is, what a turn would run in, and
// whether any of it survives a restart. Anything the control plane did not say is left out rather
// than guessed at.
func (m Model) statusLines() []string {
	lines := make([]string, 0, 6)
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, faint.Render(pad(key, 8))+value)
		}
	}
	add("address", m.info.Address)
	add("model", m.info.Model)
	add("sandbox", m.info.Sandbox)
	add("store", m.info.Store)
	if m.info.Store != "" {
		add("state", statePhrase(m.info.StateKept))
	}
	add("scope", m.scopeName())
	if len(lines) == 0 {
		lines = append(lines, faint.Render("quay"))
	}
	return lines
}

// statePhrase says what happens to a conversation when its container is replaced, in words rather
// than a true or a false, because that is the thing the operator actually needs to know.
func statePhrase(kept bool) string {
	if kept {
		return "kept on the host"
	}
	return alert.Render("lost with the container")
}

// scopeName is what the current view is narrowed to, as a path, so it reads the way the operator
// would type it.
func (m Model) scopeName() string {
	trail := m.trail()
	if len(trail) == 0 {
		return ""
	}
	return strings.Join(trail, "/")
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

// hintLines is what the keys do here, wrapped into the same number of lines as the status block so
// the two sit side by side. The view's own actions come first: they are the reason the operator is
// looking at this view rather than another one.
func (m Model) hintLines() []string {
	perLine := 3
	hints := m.hintParts()
	lines := make([]string, 0, (len(hints)+perLine-1)/perLine)
	for start := 0; start < len(hints); start += perLine {
		end := start + perLine
		if end > len(hints) {
			end = len(hints)
		}
		lines = append(lines, strings.Join(hints[start:end], " "))
	}
	return lines
}

// panelTop is the framed panel's top edge, titled with the resource, its scope and its count, so
// both are visible without counting rows: sessions(house-bills)[3].
func (m Model) panelTop(count int) string {
	title := m.active.Name
	// The nearest thing drilled through, not the whole path: the path is already in the status
	// block, and the title is answering "these rows are which ones".
	if trail := m.trail(); len(trail) > 0 {
		title += "(" + trail[len(trail)-1] + ")"
	}
	title += fmt.Sprintf("[%d]", count)
	if m.filter != "" {
		title += " filter " + m.filter
	}

	edge := "╭─ " + title + " "
	if gap := m.width - lipgloss.Width(edge) - 1; gap > 0 {
		edge += strings.Repeat("─", gap)
	}
	return frame.Render(truncate(edge, m.width-1) + "╮")
}

func (m Model) panelBottom() string {
	width := m.width - 2
	if width < 0 {
		width = 0
	}
	return frame.Render("╰" + strings.Repeat("─", width) + "╯")
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
			out[i] += " ▲"
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

// breadcrumb is the drill path, so "me > house-bills > sessions" says what escape goes back to.
func (m Model) breadcrumb() string {
	trail := append(m.trail(), m.active.Name)
	line := " " + crumb.Render(strings.Join(trail, faint.Render(" > ")))
	if len(m.stack) > 0 {
		line += faint.Render("   esc to go back")
	}
	return line
}

// hintParts lists what the keys do, with the view's own actions first because they are the reason
// the operator is looking at this view rather than another one.
func (m Model) hintParts() []string {
	parts := make([]string, 0, len(m.active.Actions)+6)
	for _, action := range m.active.Actions {
		parts = append(parts, hint(action.Key, action.Label))
	}
	if m.active.DrillTo != "" {
		parts = append(parts, hint("⏎", m.active.DrillTo))
	}
	if len(m.stack) > 0 || m.filter != "" {
		parts = append(parts, hint("esc", "Back"))
	}
	parts = append(parts,
		hint("↑↓", "Nav"), hint(":", "Resource"), hint("/", "Filter"),
		hint("g", "Refresh"), hint("q", "Quit"),
	)
	return parts
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
