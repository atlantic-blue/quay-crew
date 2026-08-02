package console

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshEvery is how often the active view reloads on its own. The operator can always force one.
const refreshEvery = 3 * time.Second

// mode is what the keyboard is doing right now.
type mode int

const (
	// modeBrowse is the default: keys navigate and act on the selected row.
	modeBrowse mode = iota
	// modeCommand is the command bar, opened with a colon, for switching resource.
	modeCommand
	// modeFilter is the filter bar, opened with a slash, narrowing the rows on screen.
	modeFilter
)

// crumbEntry remembers a view that was drilled down from, so escape restores it exactly.
type crumbEntry struct {
	resource string
	parent   string
	selected int
}

// Messages. Everything that touches the network or the clock arrives as one of these, which is what
// keeps Update a pure function over (model, message).
type (
	// rowsMsg carries a completed listing. It names its resource and parent so a listing that
	// finished after the operator moved on is discarded rather than shown in the wrong view.
	rowsMsg struct {
		resource string
		parent   string
		rows     []Row
	}
	// errMsg is a listing or an action that failed.
	errMsg struct{ err error }
	// tickMsg is the refresh clock.
	tickMsg struct{}
	// actionDoneMsg is an action that finished, successfully or not.
	actionDoneMsg struct{ err error }
)

// Model is the console. It is a pure function over messages: Update never performs input or output,
// so every behaviour here is table tested with no terminal and no control plane.
type Model struct {
	registry *Registry
	active   Resource
	parent   string
	rows     []Row
	selected int
	top      int
	width    int
	height   int
	mode     mode
	input    string
	filter   string
	err      error
	stack    []crumbEntry
	quitting bool
}

// New opens the console on the named resource.
func New(registry *Registry, start string) (Model, error) {
	if registry == nil {
		return Model{}, fmt.Errorf("console: nil registry")
	}
	resource, found := registry.Get(start)
	if !found {
		return Model{}, fmt.Errorf("console: no resource named %q", start)
	}
	return Model{registry: registry, active: resource, width: 100, height: 24}, nil
}

// Init loads the opening view and starts the refresh clock.
func (m Model) Init() tea.Cmd {
	return tea.Batch(listCmd(m.active, m.parent), tickCmd())
}

// Update advances the console. It performs no input or output of its own: anything that talks to the
// world is returned as a command for the runtime to run.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(listCmd(m.active, m.parent), tickCmd())
	case rowsMsg:
		return m.applyRows(msg), nil
	case errMsg:
		// The previous rows stay on screen. A failed refresh should not blank the view the
		// operator is reading.
		m.err = msg.err
		return m, nil
	case actionDoneMsg:
		m.err = msg.err
		return m, listCmd(m.active, m.parent)
	case tea.KeyMsg:
		return m.updateKey(msg)
	default:
		return m, nil
	}
}

// applyRows installs a completed listing, ignoring one that belongs to a view already left.
func (m Model) applyRows(msg rowsMsg) Model {
	if msg.resource != m.active.Name || msg.parent != m.parent {
		return m
	}
	m.rows = msg.rows
	m.err = nil
	return m.clampSelection()
}

// clampSelection keeps the cursor and the scroll offset inside the rows that are actually visible,
// which matters after a refresh shortens the list or a filter narrows it.
func (m Model) clampSelection() Model {
	visible := m.visibleRows()
	if len(visible) == 0 {
		m.selected, m.top = 0, 0
		return m
	}
	if m.selected >= len(visible) {
		m.selected = len(visible) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	body := m.bodyHeight()
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+body {
		m.top = m.selected - body + 1
	}
	if m.top < 0 {
		m.top = 0
	}
	return m
}

// visibleRows is the rows after the filter. An empty filter shows everything.
func (m Model) visibleRows() []Row {
	if m.filter == "" {
		return m.rows
	}
	needle := strings.ToLower(m.filter)
	matched := make([]Row, 0, len(m.rows))
	for _, row := range m.rows {
		if rowMatches(row, needle) {
			matched = append(matched, row)
		}
	}
	return matched
}

func rowMatches(row Row, needle string) bool {
	if strings.Contains(strings.ToLower(row.ID), needle) {
		return true
	}
	for _, cell := range row.Cells {
		if strings.Contains(strings.ToLower(cell), needle) {
			return true
		}
	}
	return false
}

// selectedRowValue returns the row under the cursor, and whether there is one.
func (m Model) selectedRowValue() (Row, bool) {
	visible := m.visibleRows()
	if m.selected < 0 || m.selected >= len(visible) {
		return Row{}, false
	}
	return visible[m.selected], true
}

// bodyHeight is how many rows fit: the window less the breadcrumb, the column header and the footer.
func (m Model) bodyHeight() int {
	body := m.height - 3
	if m.err != nil {
		body--
	}
	if body < 1 {
		return 1
	}
	return body
}

// listCmd loads a resource's rows. It is the only place the console reads from the world.
func listCmd(resource Resource, parent string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rows, err := resource.List(ctx, parent)
		if err != nil {
			return errMsg{err: fmt.Errorf("list %s: %w", resource.Name, err)}
		}
		return rowsMsg{resource: resource.Name, parent: parent, rows: rows}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}
