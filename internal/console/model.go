package console

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	// modeHelp lists every key, opened with a question mark. The header carries only this view's own
	// commands, the way k9s does, so the rest have to be somewhere.
	modeHelp
	// modeConfirm is a destructive key waiting for a yes. It is a mode the keyboard is in, drawn
	// where the command bar draws, rather than a floating window: the console has no overlay
	// machinery and this does not need any.
	modeConfirm
)

// pending is a destructive action waiting on an answer, and the row it would act on. The row is held
// rather than looked up again, so a refresh that reorders the list underneath cannot turn a yes into
// a yes to something else.
type pending struct {
	action Action
	row    Row
}

// crumbEntry remembers a view that was drilled down from, so escape restores it exactly. It also
// remembers what the operator drilled into, which is what the breadcrumb reads out: "me" rather than
// "workspaces".
type crumbEntry struct {
	resource string
	parent   string
	selected int
	into     string
}

// Info is what the crew on the other end of the connection is running. The console shows it so the
// operator can see which one they are about to act on, the way a cluster name does. It is fetched
// once: it is configuration, and configuration does not change under a running process.
type Info struct {
	// Version is the build of the tool itself, stamped in at compile time.
	Version string
	// Address is the control plane this console is pointed at.
	Address string
	// Workspace and Project are where the operator is standing, from their current context.
	Workspace string
	Project   string
	Model     string
	Sandbox   string
	Store     string
	State     string
	Events    string
	// Behind says the control plane is older than this tool: old enough that it cannot answer what it
	// is running. Everything else in here is then blank, and the console has to say why rather than
	// quietly showing less.
	Behind bool
}

// InfoSource fetches that description. It is a function rather than a client so the console stays
// testable with no control plane at all.
type InfoSource func(ctx context.Context) (Info, error)

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
	// infoMsg carries what the control plane says it is running.
	infoMsg struct{ info Info }
	// behindMsg says the control plane is too old to answer at all.
	behindMsg struct{}
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
	info     Info
	source   InfoSource
	waiting  pending
}

// New opens the console on the named resource. source describes the crew it is connected to, and may
// be nil, in which case the status block says nothing rather than guessing.
func New(registry *Registry, start string, source InfoSource) (Model, error) {
	if registry == nil {
		return Model{}, fmt.Errorf("console: nil registry")
	}
	resource, found := registry.Get(start)
	if !found {
		return Model{}, fmt.Errorf("console: no resource named %q", start)
	}
	return Model{registry: registry, active: resource, source: source, width: 100, height: 24}, nil
}

// Init loads the opening view, asks what it is connected to, and starts the refresh clock.
func (m Model) Init() tea.Cmd {
	return tea.Batch(listCmd(m.active, m.parent), infoCmd(m.source), tickCmd())
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
	case infoMsg:
		m.info = msg.info
		return m, nil
	case behindMsg:
		m.info.Behind = true
		return m, nil
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

// visibleRows is the rows after the filter, in the order the active resource sorts by. An empty
// filter shows everything.
func (m Model) visibleRows() []Row {
	matched := m.rows
	if m.filter != "" {
		needle := strings.ToLower(m.filter)
		matched = make([]Row, 0, len(m.rows))
		for _, row := range m.rows {
			if rowMatches(row, needle) {
				matched = append(matched, row)
			}
		}
	}
	return sortRows(matched, m.active.SortBy)
}

// sortRows orders rows by one column, stably, so rows that tie keep the order the control plane
// returned them in. It copies rather than sorting in place, because the unsorted listing is what a
// later refresh compares against.
func sortRows(rows []Row, column int) []Row {
	if len(rows) < 2 {
		return rows
	}
	sorted := make([]Row, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return cellAt(sorted[i], column) < cellAt(sorted[j], column)
	})
	return sorted
}

func cellAt(row Row, column int) string {
	if column < 0 || column >= len(row.Cells) {
		return ""
	}
	return row.Cells[column]
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

// bodyHeight is how many rows fit: the window less the header block, the panel's own frame and
// column header, and the footer line.
//
// It measures the header block rather than the status block inside it. The key hints sit beside the
// status, and either can be the taller of the two, so sizing the body off the status alone draws a
// view taller than the window. The terminal then scrolls, and what scrolls away is the top: the
// status block and the hints. Seen against a control plane too old to say what it was running, where
// the status was one line and the hints were three.
func (m Model) bodyHeight() int {
	body := m.height - len(m.headerLines()) - 4
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

// infoCmd asks the crew what it is running.
//
// A control plane that does not have the call at all is a different thing from one that failed to
// answer, and it is the common case after an upgrade: the tool moves ahead of the stack, because
// installing the tool does not rebuild the stack. That gets reported rather than swallowed, because
// silently showing four fewer lines reads as the console being broken.
//
// Any other failure is still swallowed. The operator came here to look at threads, and a status block
// that could not be filled in is not a reason to show them an error instead.
func infoCmd(source InfoSource) tea.Cmd {
	if source == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		info, err := source(ctx)
		if err == nil {
			return infoMsg{info: info}
		}
		if status.Code(err) == codes.Unimplemented {
			return behindMsg{}
		}
		return nil
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}
