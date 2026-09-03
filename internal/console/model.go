package console

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"sort"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
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
	// modeReading is text over the rows, scrolled and closed like the help overlay because it is the
	// same kind of thing: something to read. What a command printed arrives here, and so does the
	// whole of a row whose cells hold a fragment of it.
	modeReading
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
	// modeScreen is one row read on its own, in place of the listing it was opened from. A job is the
	// one so far: it states a sentence, it stands in one of four stages and sessions work on it, and
	// none of that fits in a row.
	modeScreen
)

// summary is a view's line above the columns: the text, and the state it is drawn in. The state is
// the whole line's rather than one word's, so it is applied after the line has been cut and padded, the
// way a cell's colour is: slicing a styled string cuts through the escape codes in it.
type summary struct {
	line  string
	state State
}

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
	// row is the identifier of what was drilled into, so a place written down can be walked back and
	// the level it names checked for still being there.
	row string
	// typed is what a person would type for that row, which is what the position line is built from.
	// It is the name for a workspace and a project, and the shortened identifier for a job.
	typed string
}

// Info is what the system on the other end of the connection is running. The console shows it so the
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
	// Spent is what every conversation in the system has cost so far. Zero is a system nobody has used.
	Spent sandbox.Usage
	// SandboxBuild is the build the sandbox image was made from. Empty means the image does not say,
	// and nothing is then shown: a system that cannot see which build its image came from should say
	// nothing rather than accuse a good image of being old.
	SandboxBuild string
	// Room is what the machine has left: one figure and one word. Empty means nobody asked, and the
	// header then says nothing rather than saying there is room it never measured. The header that
	// drew a healthy system through eighteen kills is why this is here at all. See issue 405.
	Room string
	// RoomState is that word on its own, so the header can colour it: full has to be readable
	// without reading the figure beside it.
	RoomState string
}

// SandboxStale says every session is running an image from a build the system has moved on from.
// Upgrading rebuilds the tool and the stack, and a sandbox image left behind means each conversation
// keeps running the build from before, with the krewe inside it older than the system or missing.
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
		// summary is the line above the columns, from the same load as the rows. It travels with them
		// so a view never draws a total from one moment over a listing from another.
		summary summary
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

	// firstRunMsg answers whether the system has any workspaces at all, which is what decides
	// whether opening the console offers the guided setup.
	firstRunMsg struct{ empty bool }
	// infoMsg carries what the control plane says it is running.
	infoMsg struct{ info Info }
	// behindMsg says the control plane is too old to answer at all.
	behindMsg struct{}
	// conversationMsg is a conversation opened beside the console, or closed. pane is the one that
	// was opened, and is empty when it was closed. session is whose conversation went in it, so the
	// console can say which one the operator is beside rather than only that one is open.
	conversationMsg struct {
		pane    string
		session string
		err     error
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
	// opened are the rows whose parts are drawn under them, by identifier. A row is closed until
	// somebody opens it, so a listing is the work a person declared rather than that work and every
	// part of it. It is kept across a drill and the way back, because a listing returned to should be
	// the listing that was left, and an identifier belongs to one row in one view anyway.
	opened map[string]bool
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
	held  bool
	stack []crumbEntry
	// summary is the active view's line above the columns, empty in a view that has none. It is
	// dropped along with the rows when a view is left, so one machine's totals are never drawn over
	// another view's listing while the next one loads.
	summary  summary
	quitting bool
	info     Info
	source   InfoSource
	waiting  pending
	// making is what the wizard has been told so far, and client is what it will ask.
	making wizard
	// runCommand runs a krewe command typed into the bar. Nil in a console that cannot run one,
	// which says so rather than doing nothing.
	runCommand CommandRunner
	// The three below are what is being read over the rows: the title of it, its lines, and how far
	// down them the panel is scrolled. What a command printed is one of these, and what a row is
	// about is the other.
	readingTitle string
	reading      []string
	readingTop   int
	// offeredSetup says the guided setup has had its one chance this run, so an emptied listing
	// later cannot reopen it over whatever the operator is doing.
	offeredSetup bool
	client       quaycrewv1.ControlPlaneServiceClient
	// places is where the console keeps where it was standing, so the next run opens there. Both
	// halves may be nil, and it then opens at the top every time.
	places PlaceStore
	// resuming is a remembered place waiting to be walked back down on the way up. Empty is a console
	// that was given nothing to resume to.
	resuming Place
	// screen is the row being read on its own, and screenTop is how far its prose is scrolled. The
	// block above it does not scroll, so the two are held apart: what a person is watching stays
	// where it is while they read.
	screen    Screen
	screenTop int
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
	// conversationOf is whose conversation is in that pane, so the console can say which one the
	// operator is sitting beside. Empty means it does not know, which is a pane the operator split for
	// themselves rather than one this console opened.
	conversationOf string
	// panes is how the console opens and closes the conversation beside it. Nil is tmux itself, which
	// is what runs in front of an operator.
	panes Panes
	// terminal is how the console hands the screen to a command it starts. Nil means the terminal
	// library's own way, which is what runs in front of an operator.
	terminal Terminal
	// waits is what the system last said waits for a person, and bell is how the console rings when
	// that count goes up. A nil bell is a console that draws the line and makes no sound.
	waits []*quaycrewv1.Waiting
	bell  Bell
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

// Freshen tells the console how to end the conversation beside it, which is what the key for a new
// one does before opening it again.
func (m Model) Freshen(end func(selected string) error) Model {
	m.freshen = end
	return m
}

// WithInfo puts what is already known about the system on the screen straight away, rather than an
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

// WithPanes says how the console opens and closes the conversation beside it. Without one it is tmux,
// which is what runs in front of an operator.
func (m Model) WithPanes(panes Panes) Model {
	m.panes = panes
	return m
}

// OpenBeside is the session whose conversation sits beside the console, and whether it knows. It does
// not know about the one `krewe` opened with the panel, because the console did not put it there.
func (m Model) OpenBeside() (string, bool) {
	return m.conversationOf, m.conversationOf != ""
}

// WithClient gives the console the system to ask when it makes something. Listing goes through each
// resource's own lister, so this is only for the wizard, which makes things no single view owns.
func (m Model) WithClient(client quaycrewv1.ControlPlaneServiceClient) Model {
	m.client = client
	return m
}

// New opens the console on the named resource. source describes the system it is connected to, and may
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
	return tea.Batch(m.Opening(), tickCmd())
}

// Opening is everything the console does on the way up except start the refresh clock: load the view,
// ask what it is connected to, and walk back down to wherever it was left.
//
// It is separate from Init because the clock is the one part nothing else wants. A test or a scenario
// that ran Init would wait for a tick to prove something about the first frame.
//
// A console with somewhere to resume to loads the top anyway, so the first frame is a listing rather
// than an empty panel while the levels are walked back down.
func (m Model) Opening() tea.Cmd {
	opening := tea.Batch(listCmd(m.active, m.parent), infoCmd(m.source), waitingCmd(m.client))
	if m.resuming.Empty() {
		return opening
	}
	return tea.Batch(opening, resumeCmd(m.registry, m.resuming))
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
		// system has cost, which changes with every task, and a total from when the console opened is
		// worse than none: it looks live and is not.
		// And what waits for a person, on the same clock. It is asked for whichever view is open,
		// because a job waiting on somebody is not a property of the rows they happen to be reading.
		return m, tea.Batch(listCmd(m.active, m.parent), infoCmd(m.source), waitingCmd(m.client), tickCmd())
	case rowsMsg:
		next := m.applyRows(msg)
		// A first listing with nothing in it is the one moment the guided setup can be worth
		// offering. Whether the system is genuinely empty is the workspaces' answer, not this
		// view's: the console opens on sessions, and a system can have workspaces and no sessions.
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
		// The wizard is finished the moment the system answers, so it closes and the refreshed list
		// shows what it made. Left open it drew "making it" over a list it had already updated, which
		// reads as nothing having happened at all. A refusal comes back on the list rather than
		// trapping the operator on a question that is no longer being asked.
		if m.mode == modeWizard {
			m.mode, m.making = modeBrowse, wizard{}
		}
		return m, listCmd(m.active, m.parent)
	case resumedMsg:
		return m.applyResumed(msg)
	case waitingMsg:
		return m.applyWaiting(msg)
	case screenMsg:
		// Held, because reading one row is something the operator asked for: the refresh on the next
		// tick would otherwise blank the reason before it was ever read.
		if msg.err != nil {
			m.err, m.held = msg.err, true
			return m, nil
		}
		m.mode, m.screen, m.screenTop, m.err = modeScreen, msg.screen, 0, nil
		return m, nil
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
		m.conversation, m.conversationOf, m.err = msg.pane, msg.session, nil
		return m, nil
	case tea.KeyMsg:
		// Where the operator is standing can change without the view changing, which is drilling from
		// one workspace into another.s projects, so the whole place is compared rather than the view.
		standing := m.place()
		// A key is the operator saying they read the last refusal, so it stops being held here rather
		// than in each handler. The handlers that set an error set it after this line.
		m.held = false
		next, cmd := m.updateKey(msg)
		if !next.place().Same(standing) {
			return next, tea.Batch(cmd, next.rememberCmd())
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
	m.rows, m.summary = msg.rows, msg.summary
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
//
// A filtered listing is flat. A row that is part of another is otherwise hidden by whether its
// parent is open, so filtering for a part would find nothing, and the filter is the second way onto
// a part: typing what it is about reaches it without opening the job above it first.
func (m Model) visibleRows() []Row {
	if m.filter != "" {
		needle := strings.ToLower(m.filter)
		matched := make([]Row, 0, len(m.rows))
		for _, row := range m.rows {
			if rowMatches(row, needle) {
				matched = append(matched, row)
			}
		}
		return sortRows(matched, m.active.SortBy)
	}
	return m.arranged(sortRows(m.rows, m.active.SortBy))
}

// arranged draws the listing in the shape its rows describe. A row that says it is part of another
// is drawn under that one and only while it is open, so the listing is the work a person declared
// until they ask for the rest of it.
//
// A part whose parent is not in this listing stays where it is. A row that quietly disappears is
// worse than a row in the wrong place, and a listing narrowed by a phase is exactly where a part
// arrives without the job above it.
func (m Model) arranged(rows []Row) []Row {
	parts := map[string][]Row{}
	listed := make(map[string]bool, len(rows))
	for _, row := range rows {
		listed[row.ID] = true
	}
	for _, row := range rows {
		if row.Under != "" && listed[row.Under] {
			parts[row.Under] = append(parts[row.Under], row)
		}
	}
	if len(parts) == 0 {
		return rows
	}
	arranged := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Under != "" && listed[row.Under] {
			continue
		}
		arranged = m.put(arranged, row, parts, 0)
	}
	return arranged
}

// put lays one row down, marked with what is under it, and then whatever it is open over.
func (m Model) put(into []Row, row Row, parts map[string][]Row, depth int) []Row {
	into = append(into, m.marked(row, len(parts[row.ID]), depth))
	if !m.opened[row.ID] {
		return into
	}
	for _, part := range parts[row.ID] {
		into = m.put(into, part, parts, depth+1)
	}
	return into
}

// marked writes the tree into the one cell a person reads the row by: how many parts are under this
// job and whether they are on screen, and an indent on each part so it reads as belonging to the row
// above it.
//
// The cells are copied rather than written in place. They belong to the listing, which is drawn
// again on the next refresh, and a marker written onto one would be marked a second time.
func (m Model) marked(row Row, parts, depth int) Row {
	if parts == 0 && depth == 0 {
		return row
	}
	at := m.markerColumn()
	if at >= len(row.Cells) {
		return row
	}
	cells := make([]string, len(row.Cells))
	copy(cells, row.Cells)
	cells[at] = strings.Repeat("  ", depth) + partsMarker(parts, m.opened[row.ID]) + cells[at]
	row.Cells = cells
	return row
}

// partsMarker says how many rows are under this one and which way the key would move them. Nothing
// at all on a row with none, so the mark is only ever on the rows it is about.
func partsMarker(parts int, open bool) string {
	if parts == 0 {
		return ""
	}
	if open {
		return fmt.Sprintf("▾%d ", parts)
	}
	return fmt.Sprintf("▸%d ", parts)
}

// markerColumn is where the tree is drawn, which is the column that flexes: it is the line a person
// reads the row by, and it is the only one with room to give. A view with no flexible column marks
// its first.
func (m Model) markerColumn() int {
	for at, column := range m.active.Columns {
		if column.Width == 0 {
			return at
		}
	}
	return 0
}

// fold opens the rows under this one, or closes them. The map is rebuilt rather than written to,
// because a model is a value every scenario holds copies of, and a map written in place would open a
// row in a copy nobody pressed a key on.
func (m Model) fold(row Row) Model {
	opened := make(map[string]bool, len(m.opened)+1)
	for id, open := range m.opened {
		opened[id] = open
	}
	if opened[row.ID] {
		delete(opened, row.ID)
	} else {
		opened[row.ID] = true
	}
	m.opened = opened
	return m
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

// bodyHeight is the window less the five rows that are not rows of the listing: the panel.s top edge,
// the column header, its bottom edge, the hairline and the footer under it.
func (m Model) bodyHeight() int {
	body := m.height - 5
	if m.err != nil {
		body--
	}
	// The line above the columns takes a row from the listing. A row it does not pay for is a panel
	// one line taller than the window, which pushes the footer off the bottom of the screen.
	if m.summaryLine() != "" {
		body--
	}
	if body < 1 {
		return 1
	}
	return body
}

// firstRunCmd asks whether the system has any workspaces. An error is not an empty system: offering a
// setup over a system that could not answer would offer to remake what may exist.
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
		loaded := rowsMsg{resource: resource.Name, parent: parent, rows: rows}
		if resource.Summary != nil {
			line, state := resource.Summary(ctx, parent)
			loaded.summary = summary{line: line, state: state}
		}
		return loaded
	}
}

// infoCmd asks the system what it is running. A control plane without the call at all is reported,
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
