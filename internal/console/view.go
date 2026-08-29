package console

import (
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/charmbracelet/lipgloss"
)

// View draws the console: the status block, a framed panel of rows, and a breadcrumb.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	visible := m.visibleRows()

	lines := m.headerLines()
	if m.mode == modeOutput {
		lines = append(lines, m.panelTop(len(m.commandOutput)))
		for _, line := range m.commandBody() {
			lines = append(lines, m.framed(line))
		}
		lines = append(lines, m.panelBottom())
		if m.err != nil {
			lines = append(lines, alert.Render(truncate(m.err.Error(), m.width)))
		}
		return strings.Join(append(lines, m.footer()), "\n")
	}
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
	hints := m.hintLines()
	// Built without the total first, and again with it only if the whole block still leaves room for
	// the wordmark. The block is what decides, not the line the total sits on: the key hints beside
	// the status are usually the widest thing in it, so measuring the status line alone let the
	// wordmark go at a wider window than the total did, which is the wrong way round.
	status := m.statusLines("")
	// What joins the first line, in the order it earns its place: the machine first, because a full
	// machine stops the crew and what it cost does not. Each is added only while the whole block
	// still leaves room for the wordmark, so the header stays one row and the wordmark stays on it.
	trailer := ""
	for _, extra := range []string{m.roomPhrase(), m.spent()} {
		if extra == "" {
			continue
		}
		if m.roomFor(status, hints, trailer+extra) {
			trailer += extra
		}
	}
	if trailer != "" {
		status = m.statusLines(trailer)
	}

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
// everywhereKeys are the keys that job in every view. One list, read by the overlay behind the
// question mark and by the keys view, because two lists of the same keys drift and the one nobody is
// looking at drifts first.
var everywhereKeys = [][2]string{
	{"↑↓ jk", "Move"},
	{"pgup pgdn", "Page"},
	{"enter", "Drill in"},
	{"esc", "Back, or clear the filter"},
	{":", "Switch view, or run any quay command"},
	{"/", "Filter these rows"},
	{"r g", "Refresh now"},
	{"n", "Make one thing"},
	{"p", "Show or hide the conversation beside this"},
	{"N", "Start a fresh conversation beside this"},
	// The one key here that is not the console's own. An open conversation runs inside tmux in its
	// sandbox, so this leaves it running and comes back; without it the only way out of a session is
	// ending it, which is what everybody does until somebody tells them otherwise.
	{"ctrl-q", "Leave a conversation running"},
	{"?", "This list"},
	{"q", "Quit"},
}

// logo is the wordmark: the same block letters as always, at half the height. Every row carries two
// rows of the original through the half block characters, so this is the logo rather than the name
// written out in text, and it costs three rows instead of six.
//
// Three rows is what the header costs, because the logo is the tallest thing in it.
var logo = []string{
	" ▄█▀▀▀▀█▄ ██    ██ ▄█▀▀▀█▄ ▀█▄  ▄█▀ ",
	" ██ ▄▄ ██ ██    ██ ██▀▀▀██   ▀██▀   ",
	"  ▀▀▀██▀   ▀▀▀▀▀▀  ▀▀   ▀▀    ▀▀    ",
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
	// Width is the only thing that can stop it. The wordmark is one line, drawn at the right of a line
	// that is there anyway, so it costs no rows: a height check here dropped it from the panel's
	// header pane, which is one row tall by design and has nothing underneath it to starve.
	if m.width-room-2 < lipgloss.Width(logo[0]) {
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

// statusLines is the crew this console is pointed at: where it is, what a task would run in, and
// whether any of it survives a restart. Anything the control plane did not say is left out rather
// than guessed at.
// roomFor says whether the header block would still leave the wordmark somewhere to go once the
// total is added to its first line.
func (m Model) roomFor(status, hints []string, spent string) bool {
	widest := lipgloss.Width(spent)
	for index, line := range status {
		width := lipgloss.Width(line)
		if index == 0 {
			width += lipgloss.Width(spent)
		}
		if width > widest {
			widest = width
		}
	}
	longest := 0
	for index := range status {
		line := " " + pad(status[index], widest+3)
		if index < len(hints) {
			line += hints[index]
		}
		if width := lipgloss.Width(line); width > longest {
			longest = width
		}
	}
	for index := len(status); index < len(hints); index++ {
		if width := lipgloss.Width(" " + pad("", widest+3) + hints[index]); width > longest {
			longest = width
		}
	}
	return m.width-longest-2 >= lipgloss.Width(logo[0])
}

func (m Model) statusLines(spent string) []string {
	// Only which build this is. Everything else moved into the help panel, because it crowded the
	// wordmark off the screen: the header is as wide as the console, and the console is half the
	// window once a conversation is beside it.
	//
	//
	build := m.info.Version
	if build == "" {
		build = faint.Render("unknown")
	}
	lines := []string{statusKey.Render(pad("Version:", 16)) + build + spent}
	if m.info.Behind {
		lines = append(lines, statusKey.Render(pad("Quay:", 16))+
			alert.Render("this control plane is older than the tool, run make upgrade"))
	}
	if m.info.SandboxStale() {
		lines = append(lines, statusKey.Render(pad("Sandboxes:", 16))+
			alert.Render("running the image from "+m.info.SandboxBuild+
				", older than this build, run make sandbox-image"))
	}
	return lines
}

// roomPhrase is what the machine has left: one figure and one word, beside the build.
//
// One row rather than one line of its own, because the header is one row and the whole point of it
// is what can be read at a glance. The word is coloured rather than only spelled, because a full
// machine has to be readable without reading the number beside it. A crew that measured nothing says
// unknown, and unknown is never green: a header that claimed room it had not measured is what let
// eighteen sandboxes be killed with nothing said about it.
func (m Model) roomPhrase() string {
	if m.info.RoomState == "" {
		return ""
	}
	if m.info.RoomState == headroom.StateUnknown {
		return "   " + statusKey.Render("Memory ") + faint.Render(headroom.StateUnknown)
	}
	return "   " + statusKey.Render("Memory ") + m.info.Room + " " + roomWord(m.info.RoomState)
}

// roomWord colours the one word the header carries about the machine.
func roomWord(state string) string {
	switch state {
	case headroom.StateFull:
		return alert.Render(strings.ToUpper(state))
	case headroom.StateTight:
		return busy.Render(state)
	case headroom.StateRoom:
		return ready.Render(state)
	default:
		return faint.Render(headroom.StateUnknown)
	}
}

// sandboxImagePhrase names the build every session is running, and says in red when the crew has
// moved on from it. Sessions run whatever that image holds, so an image left behind is a crew whose
// conversations are on the build from before, with the quay inside them older than the crew or not
// there at all. An image that does not say which build it is is left out rather than guessed at.
func sandboxImagePhrase(info Info) string {
	if info.SandboxBuild == "" {
		return ""
	}
	if info.SandboxStale() {
		return alert.Render(info.SandboxBuild + ", older than this build, run make sandbox-image")
	}
	return info.SandboxBuild
}

// spent is what the crew has cost, drawn beside the build when there is room for it and the wordmark
// both. It gives way first: the number is in the listing too.
func (m Model) spent() string {
	if m.info.Spent.Empty() {
		return ""
	}
	return "   " + statusKey.Render("↑") + display.Tokens(m.info.Spent.Output) +
		statusKey.Render(" ↓") + display.Tokens(m.info.Spent.Input) +
		statusKey.Render(" ⟳") + display.Tokens(m.info.Spent.CacheRead)
}

// statePhrase says where a conversation is kept. Empty means nowhere: it lives in the container and
// dies with it, which is worth saying in red because it is the difference between a session you can
// come back to and one you cannot.
func statePhrase(where string) string {
	if where == "" {
		return alert.Render("in the container, lost when it is replaced")
	}
	return where
}

// secretsPhrase says where a workspace's credentials are kept, and says the cost out loud when they
// are kept nowhere: the subscription token goes with every restart, and the task that then fails says
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

// hintLines fills each column top to bottom before starting the next, the way k9s does, because
// reading down a column is how you find a key. The view's own actions come first.
func (m Model) hintLines() []string {
	hints := m.hintParts()
	if len(hints) == 0 {
		return nil
	}

	// Tall enough to read, matching the status block when that is taller so the two end together,
	// and never so tall that the rows have nowhere left to go.
	// The total sits on a line that is there either way, so it does not change how tall this is.
	height := len(m.statusLines(""))
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

// helpBody is padded to the height the rows would have taken, so opening it does not resize what is
// underneath, and it takes as many columns as it needs: a key list missing its last few entries looks
// complete and is not.
func (m Model) helpBody() []string {
	height := m.bodyHeight() + 1

	// The crew block is drawn at full width, and only the keys are folded into columns. Folded in
	// together, one long line about where state is kept made every column that wide, which left room
	// for one column, which pushed the keys off the end of the panel. They were not shortened, they
	// were dropped, and a help panel missing half its keys looks exactly like a complete one.
	described := m.crewBlock()
	entries := described
	if room := height - len(described); room > 0 {
		entries = append(described, intoColumns(m.helpLines(), room, m.innerWidth())...)
	}

	// Scrolled rather than cut. Everything the header used to carry lives here now, so on a short
	// window there is more of this than there is room, and dropping the end of it silently is how a
	// help panel missing half its keys looks exactly like a complete one.
	top := m.helpTop
	if top > len(entries)-height {
		top = len(entries) - height
	}
	if top < 0 {
		top = 0
	}

	lines := make([]string, 0, height)
	for _, line := range entries[top:] {
		if len(lines) == height {
			break
		}
		lines = append(lines, m.fit(line))
	}
	for len(lines) < height {
		lines = append(lines, m.fit(""))
	}
	if hidden := len(entries) - top - height; hidden > 0 {
		lines[len(lines)-1] = m.fit(faint.Render(
			fmt.Sprintf("    %d more, ↑↓ to scroll", hidden)))
	}
	return lines
}

// intoColumns folds a list into however many columns fit, reading down each one. Columns are as wide
// as their widest entry rather than an even share, because an even share truncates.
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
	columns := m.columns()
	return headerBar.Render(m.fit(m.renderCellsIn(columns, titles(columns, m.active.SortBy))))
}

// fit makes a line exactly the width inside the panel: padded when it is short, cut when it is long.
// Cutting matters on a narrow window, where the columns a resource declares add up to more than the
// room available and an overflowing line wraps, which costs a row and pushes the top of the view off
// the screen. It cuts before any styling is applied, because slicing a styled string cuts through
// the escape codes in it.
func (m Model) fit(text string) string {
	return pad(truncate(text, m.innerWidth()), m.innerWidth())
}

func titles(columns []visible, sortedBy int) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = strings.ToUpper(column.Title)
		if column.at == sortedBy {
			out[i] += "↑"
		}
	}
	return out
}

// visible is a column that is being drawn, and where its cell sits in a row. The two come apart as
// soon as a column is dropped: the fourth column drawn may be the fifth cell in the row.
type visible struct {
	Column
	at int
}

// columns is the active resource's columns, less any that had to give way to the width available.
//
// A line too long for the panel is cut, and what is cut is whatever happens to be at the end rather
// than whatever matters least. So a resource says which of its columns may give way and in what
// order, and the narrower the window the fewer are drawn.
func (m Model) columns() []visible {
	drawn := make([]visible, 0, len(m.active.Columns))
	for at, column := range m.active.Columns {
		drawn = append(drawn, visible{Column: column, at: at})
	}
	for m.tooWide(drawn) {
		next := -1
		for index, column := range drawn {
			if column.Give == 0 {
				continue
			}
			if next < 0 || column.Give < drawn[next].Give {
				next = index
			}
		}
		if next < 0 {
			// Nothing left that is allowed to go. The line is cut, which is the old behaviour and
			// the right one: a window this narrow cannot show the columns that matter either.
			return drawn
		}
		drawn = append(drawn[:next], drawn[next+1:]...)
	}
	return drawn
}

// tooWide says whether these columns need more room than there is. The flexible column is counted at
// its smallest, because a column squeezed to nothing is not a column anybody can read.
func (m Model) tooWide(columns []visible) bool {
	need := len(columns)
	for _, column := range columns {
		if column.Width == 0 {
			need += minimumFlex
			continue
		}
		need += column.Width
	}
	return need > m.innerWidth()
}

// minimumFlex is the least room the flexible column is given before other columns start giving way.
const minimumFlex = 8

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
	// The cursor is a bar across the whole row, so a cell keeping its own colour inside it would be
	// coloured text on a coloured background. Colour comes off for the selected row, exactly as the
	// sessions tool does it.
	if isSelected {
		return selectedRow.Render(m.fit(m.renderCells(row.Cells)))
	}
	// A failed row is the one state loud enough to take the whole line: it wants attention, and it has
	// to read as wanting it before it reads as anything else.
	//
	// Every other state goes in the status cell instead. Drawing a whole row in its state was costing
	// the row every other colour it had, because a line rendered in one colour cannot carry any of the
	// others: nine of the ten views set a state on every row, so nine listings came out flat and the
	// tenth came out in two colours. The states that were doing that job are green, yellow and dim on
	// one cell now, which is where the sessions tool puts them.
	if row.State == StateFailed {
		return styleFor(row.State).Render(m.fit(m.renderCells(row.Cells)))
	}
	return m.fit(m.renderColouredCells(row.Cells))
}

// renderCells lays cells out in the active resource's columns. A zero width column takes whatever
// space is left, and at most one should have it.
func (m Model) renderCells(cells []string) string {
	return m.renderCellsIn(m.columns(), cells)
}

// renderColouredCells is the same layout, with each cell written in its column's colour. The colour
// goes on after a cell has been cut and padded to its column, so it cannot change where anything
// sits: an escape sequence is zero columns wide on screen and several characters long in the string.
func (m Model) renderColouredCells(cells []string) string {
	columns := m.columns()
	laid := m.laidOut(columns, cells)
	for at, column := range columns {
		if column.Colour == nil || at >= len(laid) {
			continue
		}
		if colour := column.Colour(strings.TrimSpace(laid[at])); colour != "" {
			laid[at] = colour + laid[at] + offCode
		}
	}
	return strings.Join(laid, " ")
}

// renderCellsIn lays cells out in the columns given, which are the ones that fit.
func (m Model) renderCellsIn(columns []visible, cells []string) string {
	return strings.Join(m.laidOut(columns, cells), " ")
}

// laidOut is each cell cut and padded to its column, before anything is joined or coloured. It is
// separate so the coloured and uncoloured rows lay out through the same code: two copies of this
// would drift, and the drift would be columns that line up in one row and not in the next.
func (m Model) laidOut(columns []visible, cells []string) []string {
	fixed := 0
	for _, column := range columns {
		fixed += column.Width
	}
	flex := m.innerWidth() - fixed - len(columns)
	if flex < minimumFlex {
		flex = minimumFlex
	}

	parts := make([]string, 0, len(columns))
	for index, column := range columns {
		// Where the cell sits in the row, which is not where the column sits on the screen once one
		// has been dropped. Titles arrive already in the order they are drawn.
		from := column.at
		if len(cells) == len(columns) {
			from = index
		}
		cell := ""
		if from < len(cells) {
			cell = cells[from]
		}
		width := column.Width
		if width == 0 {
			width = flex
		}
		parts = append(parts, pad(truncate(cell, width), width))
	}
	return parts
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
	case modeChoose:
		return truncate(m.choosePrompt(), m.width)
	case modeType:
		return truncate(m.typePrompt(), m.width)
	case modeWizard:
		return truncate(m.wizardPrompt(), m.width)
	case modeOutput:
		return faint.Render("   any key closes, j and k scroll")
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

// typePrompt draws the line being typed, naming what it is about so a name is typed onto a session
// rather than into the console.
func (m Model) typePrompt() string {
	return prompt.Render(" "+m.typing.action.Asks+" ") + m.typing.row.Name() + "  " +
		m.input + prompt.Render("_") + faint.Render("  enter names it, empty clears it, esc cancels")
}

// choosePrompt draws what a key offers, with the one under the cursor marked, on the line the command
// bar and the confirmation draw on. Every choice is on screen at once because there are a handful of
// them and reading them is half the reason somebody opens this.
func (m Model) choosePrompt() string {
	offers := m.choosing.action.Offers
	shown := make([]string, 0, len(offers))
	for at, offer := range offers {
		if at == m.choosing.at {
			shown = append(shown, selectedRow.Render(" "+offer+" "))
			continue
		}
		shown = append(shown, faint.Render(" "+offer+" "))
	}
	return prompt.Render(" "+strings.ToLower(m.choosing.action.Label)+" ") +
		m.choosing.row.Name() + "  " + strings.Join(shown, "") +
		faint.Render("  j and k move, enter picks, esc cancels")
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
		// Not a view, so enter runs it as a command. Saying "nothing called that" here would be a
		// lie about the very next keystroke: the bar is about to do something with these words.
		if strings.TrimSpace(m.input) == "" {
			return ""
		}
		return faint.Render("   enter runs this as a quay command")
	}
	return faint.Render("   " + strings.Join(names, "  "))
}

// wizardPrompt shows what has been typed, asterisks for a secret. A step that chooses from what the
// crew already has lists it: asking somebody to name a workspace they cannot see leaves them able
// only to make new ones.
func (m Model) wizardPrompt() string {
	if m.making.step() == stepWorking {
		return prompt.Render(" making ") + faint.Render(m.making.summary())
	}
	line := prompt.Render(" "+m.making.prompt()+": ") + m.making.shown() + prompt.Render("_")
	offers := m.making.currentOffers()
	if len(offers) > 0 {
		line += "   " + m.wizardOffers(offers)
	} else if m.making.picking() && m.making.loaded {
		line += faint.Render("   nothing here yet")
	}

	hint := "enter accepts, esc cancels and makes nothing"
	if m.making.guided {
		// Honest about both differences: an empty answer moves on, and escape cannot unmake the
		// stages already made.
		hint = "enter accepts, empty skips, esc leaves the setup"
	}
	if len(offers) > 0 {
		hint += ", tab cycles the options"
	}
	return line + faint.Render("   "+hint)
}

// wizardOffers draws what a step offers, with the one tab has landed on marked the way the command
// bar's own choice picker marks its cursor. Nothing is marked until tab has been pressed once, so a
// step nobody has cycled through still reads as a plain list of what is possible.
func (m Model) wizardOffers(offers []string) string {
	if !m.making.cycling {
		return faint.Render(strings.Join(offers, "  "))
	}
	shown := make([]string, 0, len(offers))
	for at, offer := range offers {
		if at == m.making.cycleAt {
			shown = append(shown, selectedRow.Render(" "+offer+" "))
			continue
		}
		shown = append(shown, faint.Render(" "+offer+" "))
	}
	return strings.Join(shown, "")
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
	// One hint, and it is the one that leads to all the others. This view's keys are in the help
	// panel with everything else; listing them here is what crowded out the wordmark.
	return []string{hint("?", "Help")}
}

// helpLines is every key, this view's own first and then the ones that job everywhere. This is what
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

// crewLines describe the crew this console is pointed at: where it is, where you are standing in it,
// and what it is running underneath. They were the header until the header had no room left for the
// wordmark, and they are also the stats view, which is the one to leave open beside what you are
// doing.
func (m Model) crewLines() []string {
	lines := make([]string, 0, 9)
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, hint(pad(key, 16), value))
		}
	}
	add("Address", m.info.Address)
	add("Workspace", m.info.Workspace)
	add("Project", m.info.Project)
	add("Model", m.info.Model)
	add("Sandbox engine", m.info.Sandbox)
	add("Sandbox image", sandboxImagePhrase(m.info))
	add("Store engine", m.info.Store)
	add("Secrets", secretsPhrase(m.info.Secrets))
	if m.info.Store != "" {
		add("Events engine", eventsPhrase(m.info.Events))
		add("State", statePhrase(m.info.State))
	}
	if len(lines) == 0 {
		lines = append(lines, faint.Render("still asking what this control plane is running"))
	}
	return lines
}

// crewBlock is what the crew is, drawn at full width above the keys. The header carries the wordmark
// and which build this is; everything it used to carry is here.
func (m Model) crewBlock() []string {
	lines := []string{crumb.Render("  this crew")}
	for _, described := range m.crewLines() {
		lines = append(lines, "    "+described)
	}
	return append(lines, "")
}
