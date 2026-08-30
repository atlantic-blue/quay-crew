package console

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"sort"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
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
	// modeOutput is what a command printed, over the rows, scrolled and closed like the help
	// overlay because it is the same kind of thing: something to read.
	modeOutput
	// modeWizard is making something: a workspace, a project, and whatever else was offered along the
	// way. Nothing is made until the last step, so leaving it creates nothing.
	modeWizard
	// modeType is a key asking for a line of text about the selected row, drawn where the command bar
	// draws. Naming a session is the one so far.
	modeType
	// modeChoose is a key offering a few named things and waiting for one to be picked. It draws
	// where the confirmation draws, because it is the same kind of thing: a question with the answer
	// on screen. Leaving it does nothing, so it is also how somebody reads what the choices are.
	modeChoose
)

// pending is a destructive action waiting on an answer, and the row it would act on. The row is held
// rather than looked up again, so a refresh that reorders the list underneath cannot task a yes into
// a yes to something else.
type pending struct {
	action Action
	row    Row
	// chosen is what was picked, for a question that came from a picker rather than from a key that
	// acts on its own. Empty for every other key.
	chosen string
}

// choice is a key waiting on a pick: the action that opened it, the row it would act on, and where
// the cursor is. The row is held rather than looked up again, for the same reason a confirmation
// holds one: a refresh that reorders the list must not task a pick into a pick of something else.
type choice struct {
	action Action
	row    Row
	at     int
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
	Secrets   string
	State     string
	Events    string
	// Behind says the control plane is older than this tool: old enough that it cannot answer what it
	// is running. Everything else in here is then blank, and the console has to say why rather than
	// quietly showing less.
	Behind bool
	// Spent is what every conversation in the crew has cost so far. Zero is a crew nobody has used.
	Spent sandbox.Usage
	// SandboxBuild is the build the sandbox image was made from. Empty means the image does not say,
	// and nothing is then shown: a crew that cannot see which build its image came from should say
	// nothing rather than accuse a good image of being old.
	SandboxBuild string
	// Room is what the machine has left: one figure and one word. Empty means nobody asked, and the
	// header then says nothing rather than saying there is room it never measured. The header that
	// drew a healthy crew through eighteen kills is why this is here at all. See issue 405.
	Room string
	// RoomState is that word on its own, so the header can colour it: full has to be readable
	// without reading the figure beside it.
	RoomState string
}

// SandboxStale says every session is running an image from a build the crew has moved on from.
// Upgrading rebuilds the tool and the stack, and a sandbox image left behind means each conversation
// keeps running the build from before, with the quay inside it older than the crew or missing.
func (i Info) SandboxStale() bool {
	return i.Version != "" && i.SandboxBuild != "" && i.SandboxBuild != i.Version
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
	// errMsg is a listing that failed. The next listing to arrive clears it.
	errMsg struct{ err error }
	// heldErrMsg is something the operator asked for that failed. It stays on the screen until the
	// next key, because the refresh that follows an action would otherwise blank it unread.
	heldErrMsg struct{ err error }
	// tickMsg is the refresh clock.
	tickMsg struct{}
	// actionDoneMsg is an action that finished, successfully or not. kind and made say what the
	// wizard built, so the guided setup can carry a new workspace or project into its next stage.
	actionDoneMsg struct {
		err  error
		kind wizardKind
		made wizardChoice
	}

	// firstRunMsg answers whether the crew has any workspaces at all, which is what decides
	// whether opening the console offers the guided setup.
	firstRunMsg struct{ empty bool }
	// infoMsg carries what the control plane says it is running.
	infoMsg struct{ info Info }
	// behindMsg says the control plane is too old to answer at all.
	behindMsg struct{}
	// conversationMsg is a conversation opened beside the console, or closed. pane is the one that
	// was opened, and is empty when it was closed.
	conversationMsg struct {
		pane string
		err  error
	}
	// wizardChoicesMsg carries what a wizard step can be answered with. It names its step so a
	// listing that came back after the operator moved on is discarded rather than offered for the
	// wrong question.
	wizardChoicesMsg struct {
		step    wizardStep
		choices []wizardChoice
		err     error
	}
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
	choosing choice
	// typing is the row a typed line will be applied to, held for the same reason a confirmation
	// holds one: a refresh underneath must not move what the answer is about.
	typing pending
	input  string
	filter string
	// search is what the filter was last typed with, kept after the filter itself is cleared, so the
	// keys for the next and previous match have something to jump through. Escape out of the filter
	// puts every row back on screen, and that is exactly when jumping between the matches is worth
	// anything.
	search string
	// counted is the count typed in front of a move, as it is being typed, so "5j" moves five rows.
	// Empty is no count, which every move reads as once.
	counted string
	// pendingHalf is the first key of a two key sequence, and the only one is the g of gg. It is
	// drawn in the breadcrumb while it waits, because a console holding a keypress and showing
	// nothing looks like a console that dropped it.
	pendingHalf string
	err         error
	// held says the error came from something the operator did, so the next listing must leave it
	// alone. Enter on a session that cannot open used to set the error and ask for a refresh in the
	// same return, and the refresh blanked it before it was ever drawn: the key looked like it did
	// nothing at all. A held error is cleared by the next key, which is the operator saying they
	// have read it.
	held     bool
	stack    []crumbEntry
	quitting bool
	info     Info
	source   InfoSource
	waiting  pending
	// making is what the wizard has been told so far, and client is what it will ask.
	making wizard
	// runCommand runs a quay command typed into the bar, and the three below are what it said.
	// Nil in a console that cannot run one, which says so rather than doing nothing.
	runCommand    CommandRunner
	commandTyped  string
	commandOutput []string
	commandTop    int
	// offeredSetup says the guided setup has had its one chance this run, so an emptied listing
	// later cannot reopen it over whatever the operator is doing.
	offeredSetup bool
	client       quaycrewv1.ControlPlaneServiceClient
	// headless leaves the header to the pane above, which draws it across both halves. Drawing it
	// here as well would show it twice and cost the list the rows.
	headless bool
	// publish says which view the console moved to, so the header drawn in that pane names this
	// view's keys rather than the keys of whichever view it was started on.
	publish func(view string) error
	// helpTop is how far the help panel is scrolled. Everything the header used to carry is in there,
	// so on a short window it is taller than the room it has.
	helpTop int
	// beside opens a conversation next to the console, and is nil when nobody gave it one to open.
	// The console does not know which conversation: it hands over the row under the cursor, which may
	// be nothing, and gets back the command to run.
	beside func(selected string) ([]string, error)
	// freshen ends the conversation the driver is in, so the next open starts one rather than coming
	// back to it. Nil when nobody gave the console a way to.
	freshen func(selected string) error
	// conversation is the tmux pane the console opened, so the key closes the one it opened rather
	// than whichever pane happens to be beside it now. Empty means none is open.
	conversation string
	// terminal is how the console hands the screen to a command it starts. Nil means the terminal
	// library's own way, which is what runs in front of an operator.
	terminal Terminal
}

// Terminal hands the screen to a command and turns whatever came back into a message. It is what
// opening a conversation, shelling into a sandbox and editing a context all go through.
type Terminal func(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd

// Reported is what the console is telling the operator right now, and nil when it is telling them
// nothing. A scenario reads it to say whether a key that could not do its job said so.
func (m Model) Reported() error { return m.err }

// Selected is the row under the cursor, and whether there is one. A scenario reads it to say where a
// move landed: the cursor is drawn as a highlight, and a highlight is a colour, which is nothing at
// all on a screen rendered with no terminal attached.
func (m Model) Selected() (Row, bool) { return m.selectedRowValue() }

// Listed is the rows on screen, after the filter and in the order they are drawn, which is what a
// scenario counts and what it reads a position out of.
func (m Model) Listed() []Row { return m.visibleRows() }

// WithoutHeader leaves the header to the pane above.
func (m Model) WithoutHeader() Model {
	m.headless = true
	return m
}

// WithViewPublisher tells the console where to say which view it is on, so a header drawn in another
// pane can name this view's keys. Without it those hints would be the ones for whichever view the
// console opened on, and would go quietly wrong the moment anybody drilled in.
func (m Model) WithViewPublisher(publish func(view string) error) Model {
	m.publish = publish
	return m
}

// Freshen tells the console how to end the conversation beside it, which is what the key for a new
// one does before opening it again.
func (m Model) Freshen(end func(selected string) error) Model {
	m.freshen = end
	return m
}

// WithInfo puts what is already known about the crew on the screen straight away, rather than an
// empty status block that fills in a moment later when the control plane answers.
func (m Model) WithInfo(info Info) Model {
	m.info = info
	return m
}

// Beside tells the console how to put a conversation next to itself, which is what the key for it
// does. Without one the key says so rather than doing nothing.
func (m Model) Beside(open func(selected string) ([]string, error)) Model {
	m.beside = open
	return m
}

// WithClient gives the console the crew to ask when it makes something. Listing goes through each
// resource's own lister, so this is only for the wizard, which makes things no single view owns.
func (m Model) WithClient(client quaycrewv1.ControlPlaneServiceClient) Model {
	m.client = client
	return m
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
	return tea.Batch(listCmd(m.active, m.parent), infoCmd(m.source), m.publishCmd(), tickCmd())
}

// publishCmd says which view the console is on, and does nothing when nobody asked.
func (m Model) publishCmd() tea.Cmd {
	if m.publish == nil {
		return nil
	}
	publish, view := m.publish, m.active.Name
	return func() tea.Msg {
		// A header that cannot be told where we are is not a reason to stop showing the crew.
		_ = publish(view)
		return nil
	}
}

// Update advances the console. It performs no input or output of its own: anything that talks to the
// world is returned as a command for the runtime to run.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		// The description is refreshed with the rows now, not only at startup. It carries what the
		// crew has cost, which changes with every task, and a total from when the console opened is
		// worse than none: it looks live and is not.
		return m, tea.Batch(listCmd(m.active, m.parent), infoCmd(m.source), tickCmd())
	case rowsMsg:
		next := m.applyRows(msg)
		// A first listing with nothing in it is the one moment the guided setup can be worth
		// offering. Whether the crew is genuinely empty is the workspaces' answer, not this
		// view's: the console opens on sessions, and a crew can have workspaces and no sessions.
		if !next.offeredSetup && next.mode == modeBrowse && next.parent == "" &&
			len(msg.rows) == 0 && next.client != nil {
			next.offeredSetup = true
			return next, firstRunCmd(next.client)
		}
		return next, nil
	case firstRunMsg:
		if msg.empty && m.mode == modeBrowse {
			m.mode, m.making, m.err = modeWizard, guidedSetup(), nil
			return m, m.wizardChoicesCmd()
		}
		return m, nil
	case errMsg:
		// The previous rows stay on screen. A failed refresh should not blank the view the
		// operator is reading.
		m.err = msg.err
		return m, nil
	case heldErrMsg:
		m.err, m.held = msg.err, true
		return m, nil
	case actionDoneMsg:
		// The guided setup carries on: a made workspace or project is what the later stages act
		// on, and a refusal re-asks the stage's last question rather than abandoning the chain.
		if m.mode == modeWizard && m.making.guided {
			if msg.err != nil {
				m.err = msg.err
				m.making.at = len(m.making.kind.steps()) - 1
				return m, m.wizardChoicesCmd()
			}
			switch msg.kind {
			case kindWorkspace:
				m.making.workspace = msg.made
			case kindProject:
				m.making.project = msg.made
			}
			next, cmd := m.advanceGuided()
			return next, tea.Batch(cmd, listCmd(next.active, next.parent))
		}
		// Held, because the refresh on the next line is what used to blank it.
		m.err, m.held = msg.err, msg.err != nil
		// The wizard is finished the moment the crew answers, so it closes and the refreshed list
		// shows what it made. Left open it drew "making it" over a list it had already updated, which
		// reads as nothing having happened at all. A refusal comes back on the list rather than
		// trapping the operator on a question that is no longer being asked.
		if m.mode == modeWizard {
			m.mode, m.making = modeBrowse, wizard{}
		}
		return m, listCmd(m.active, m.parent)
	case infoMsg:
		m.info = msg.info
		return m, nil
	case behindMsg:
		m.info.Behind = true
		return m, nil
	case commandOutputMsg:
		return m.showCommandOutput(msg), nil
	case wizardChoicesMsg:
		applied := m.applyWizardChoices(msg)
		// A skill stage in the guided setup with nothing to offer passes itself over: a question
		// with no possible answer is not a question.
		if applied.mode == modeWizard && applied.making.guided &&
			applied.making.step() == stepPickSkill && applied.making.loaded &&
			len(applied.making.choices) == 0 {
			return applied.advanceGuided()
		}
		return applied, nil
	case conversationMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.conversation, m.err = msg.pane, nil
		return m, nil
	case tea.KeyMsg:
		// The view can change under a key, and whoever draws the header needs to hear about it.
		before := m.active.Name
		// A key is the operator saying they read the last refusal, so it stops being held here rather
		// than in each handler. The handlers that set an error set it after this line.
		m.held = false
		next, cmd := m.updateKey(msg)
		if next.active.Name != before {
			return next, tea.Batch(cmd, next.publishCmd())
		}
		return next, cmd
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
	if !m.held {
		m.err = nil
	}
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

// bodyHeight is the window less the header block, the frame, the column header and the footer.
//
// The whole block, not the status inside it: the key hints sit beside the status and either can be
// the taller, so measuring the status alone draws a view taller than the window and the top scrolls
// away.
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

// firstRunCmd asks whether the crew has any workspaces. An error is not an empty crew: offering a
// setup over a crew that could not answer would offer to remake what may exist.
func firstRunCmd(client quaycrewv1.ControlPlaneServiceClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		listed, err := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
		if err != nil {
			return firstRunMsg{}
		}
		return firstRunMsg{empty: len(listed.GetWorkspaces()) == 0}
	}
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

// infoCmd asks the crew what it is running. A control plane without the call at all is reported,
// because that is the common case after an upgrade and showing four fewer lines reads as the console
// being broken. Any other failure is swallowed: a status block is not worth an error screen.
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
