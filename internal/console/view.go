package console

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View draws the console: a breadcrumb, a column header, the rows, and a footer that says which keys
// do what here. Nothing else. The layout follows the operator's hub tools rather than boxing things.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	visible := m.visibleRows()
	lines := []string{m.breadcrumb(len(visible)), m.columnHeader()}
	lines = append(lines, m.bodyLines(visible)...)
	if m.err != nil {
		lines = append(lines, alert.Render(truncate(m.err.Error(), m.width)))
	}
	lines = append(lines, m.footer())
	return strings.Join(lines, "\n")
}

// breadcrumb names the view, its count, and what it is scoped to.
func (m Model) breadcrumb(count int) string {
	trail := make([]string, 0, len(m.stack)+1)
	for _, entry := range m.stack {
		trail = append(trail, entry.resource)
	}
	trail = append(trail, m.active.Name)

	line := crumb.Render("quay") + faint.Render(" · ") + crumb.Render(strings.Join(trail, " · "))
	line += faint.Render(fmt.Sprintf("  (%d)", count))
	if m.parent != "" {
		line += faint.Render("  scope " + m.parent)
	}
	if m.filter != "" {
		line += faint.Render("  filter " + m.filter)
	}
	return truncate(line, m.width)
}

// columnHeader is the black on green title bar, padded across the window.
func (m Model) columnHeader() string {
	return headerBar.Render(pad(m.renderCells(titles(m.active.Columns)), m.width))
}

func titles(columns []Column) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = strings.ToUpper(column.Title)
	}
	return out
}

// bodyLines renders the visible slice of rows, padding to a fixed height so the footer does not
// jump around as the list grows and shrinks.
func (m Model) bodyLines(visible []Row) []string {
	body := m.bodyHeight()
	lines := make([]string, 0, body)

	if len(visible) == 0 {
		lines = append(lines, faint.Render("  nothing here"))
	}
	for index := m.top; index < len(visible) && len(lines) < body; index++ {
		lines = append(lines, m.rowLine(visible[index], index == m.selected))
	}
	for len(lines) < body {
		lines = append(lines, "")
	}
	return lines
}

func (m Model) rowLine(row Row, isSelected bool) string {
	text := pad(m.renderCells(row.Cells), m.width)
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
	flex := m.width - fixed - len(columns)
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

// footer is the command bar, the filter bar, or the key hints for this view.
func (m Model) footer() string {
	switch m.mode {
	case modeCommand:
		return truncate(prompt.Render(":")+m.input+prompt.Render("_"), m.width)
	case modeFilter:
		return truncate(prompt.Render("/")+m.input+prompt.Render("_"), m.width)
	case modeBrowse:
		return truncate(m.hints(), m.width)
	default:
		return ""
	}
}

// hints lists what the keys do, with the view's own actions first because they are the reason the
// operator is looking at this view rather than another one.
func (m Model) hints() string {
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
	return strings.Join(parts, " ")
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
