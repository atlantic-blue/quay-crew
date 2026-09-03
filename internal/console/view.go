package console

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View draws the console: a framed panel of rows, and one footer row under it.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	visible := m.visibleRows()

	var lines []string
	if m.mode == modeReading {
		lines = append(lines, m.panelTop(len(m.readingLines())))
		for _, line := range m.readingBody() {
			lines = append(lines, m.framed(line))
		}
		lines = append(lines, m.panelBottom())
		if m.err != nil {
			lines = append(lines, alert.Render(truncate(m.err.Error(), m.width)))
		}
		return strings.Join(append(lines, m.rule(), m.footer()), "\n")
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
		return strings.Join(append(lines, m.rule(), m.footer()), "\n")
	}

	lines = append(lines, m.panelTop(len(visible)))
	if line := m.summaryLine(); line != "" {
		lines = append(lines, m.framed(styleFor(m.summary.state).Render(m.fit(line))))
	}
	lines = append(lines, m.framed(m.columnHeader()))
	for _, line := range m.bodyLines(visible) {
		lines = append(lines, m.framed(line))
	}
	lines = append(lines, m.panelBottom())
	if m.err != nil {
		lines = append(lines, alert.Render(truncate(m.err.Error(), m.width)))
	}
	lines = append(lines, m.rule(), m.footer())
	return strings.Join(lines, "\n")
}

// everywhereKeys are the keys that job in every view. One list, read by the overlay behind the
// question mark and by the keys view, because two lists of the same keys drift and the one nobody is
// looking at drifts first.
var everywhereKeys = [][2]string{
	{"↑↓ jk", "Move"},
	{"5j", "Any move, five times"},
	{"gg G", "First row, last row"},
	{"ctrl-f ctrl-b", "Page down, page up"},
	{"ctrl-d ctrl-u", "Half a page down, half up"},
	{"enter", "Drill in"},
	{"esc", "Back, or clear the filter"},
	{":", "Switch view, or run any krewe command"},
	{"/", "Filter these rows"},
	{"n N", "Next and previous match of what was filtered for"},
	{"r", "Refresh now"},
	{"o", "Make one thing"},
	{"p", "Show or hide the conversation beside this"},
	{"P", "Start a fresh conversation beside this"},
	// The one key here that is not the console's own. An open conversation runs inside tmux in its
	// sandbox, so this leaves it running and comes back; without it the only way out of a session is
	// ending it, which is what everybody does until somebody tells them otherwise.
	{"ctrl-q", "Leave a conversation running"},
	{"?", "This list"},
	{"q", "Quit"},
}

// sandboxImagePhrase names the build every session is running, and says in red when the system has
// moved on from it. Sessions run whatever that image holds, so an image left behind is a system whose
// conversations are on the build from before, with the krewe inside them older than the system or not
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

// panelTop is the framed panel.s top edge, titled with the resource, its scope and its count, so both
// are visible without counting rows: tasks(read the electricity bill)[3].
//
// The nearest thing drilled through, and read rather than typed: a job is addressed by its identifier
// and this line is answering "these rows are which ones". The typeable address is the footer.s, and
// the two are drawn separately so the console still names its scope while a bar covers that row.
func (m Model) panelTop(count int) string {
	title := m.active.Name
	if m.mode == modeHelp {
		title = "help(" + m.active.Name + ")"
		return m.titledEdge(title)
	}
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

// rule is the hairline above the footer, the one piece of drawing the footer costs. It separates the
// list from the row that says where you are, which is the job the header's three rows used to do with
// a wordmark and a status block.
func (m Model) rule() string {
	if m.width < 1 {
		return ""
	}
	return frame.Render(strings.Repeat("─", m.width))
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

// ViewName is the resource on screen, which the footer draws as a chip beside the address.
func (m Model) ViewName() string {
	return m.active.Name
}

// Position is where the operator is standing, written the way they would type it: the workspace, then
// the project, then the job. It is empty at the top, where they are above every workspace and there is
// nothing to address.
//
// A separate thing from the trail, because the trail is what to read and this is what to type. They
// are the same words for a workspace and a project, and different ones for a job: the trail carries
// its title and an address takes its identifier.
func (m Model) Position() string {
	typed := make([]string, 0, len(m.stack))
	for _, entry := range m.stack {
		if entry.typed != "" {
			typed = append(typed, entry.typed)
		}
	}
	return strings.Join(typed, "/")
}

// positionRow is the one row under the list: where the operator is standing and how to leave on the
// left, and what is true of the tool on the right.
//
// The left is drawn whole first and the right takes what is left of the row. A person who cannot see
// where they are has to guess; a person who cannot see which build this is reads it in the help panel,
// so when only one of the two fits it is never the position that goes.
func (m Model) positionRow() string {
	left := truncate(m.breadcrumb(), m.width)
	right := m.toolSide(m.width - lipgloss.Width(left) - 2)
	if right == "" {
		return left
	}
	return pad(left, m.width-lipgloss.Width(right)) + right
}

// toolSide is the right of the footer: which build this is, the key that opens everything else, and
// the product. It is what the header carried, on the row that replaced it.
//
// It drops from the end rather than wrapping or being cut, because a footer is one row: the name goes
// first, then the way to help, then the build. A width that holds none of them draws nothing, and the
// position keeps the whole row.
//
// A control plane older than the tool cannot say what it is running, so every other thing this row
// could carry is blank and the operator is looking at a console that quietly does less. That warning
// outranks all three and is the last thing to go.
func (m Model) toolSide(room int) string {
	if m.info.Behind {
		for _, said := range []string{
			"this control plane is older than the tool, run make upgrade",
			"older system, run make upgrade",
			"run make upgrade",
		} {
			if lipgloss.Width(said) <= room {
				return alert.Render(said)
			}
		}
		return ""
	}
	build := m.info.Version
	if build == "" {
		build = "unknown"
	}
	// Longest first, so the widest that fits is the one drawn.
	for _, parts := range [][]string{
		{"Version: " + build, "<?> Help", "Krewe"},
		{"Version: " + build, "<?> Help"},
		{"Version: " + build},
	} {
		line := strings.Join(parts, " | ")
		if lipgloss.Width(line) <= room {
			return tool.Render(line)
		}
	}
	return ""
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

	// The system block is drawn at full width, and only the keys are folded into columns. Folded in
	// together, one long line about where state is kept made every column that wide, which left room
	// for one column, which pushed the keys off the end of the panel. They were not shortened, they
	// were dropped, and a help panel missing half its keys looks exactly like a complete one.
	described := m.systemBlock()
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

// footer is the command bar, the filter bar, or the position row.
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
	case modeReading:
		return faint.Render("   any key closes, j and k scroll")
	case modeBrowse:
		return m.positionRow()
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
	// What an empty line does is the key's own, so a key that refuses one does not offer to clear
	// something. Two keys type into this line now, and they disagree about exactly that.
	hint := "  enter accepts, esc cancels"
	if means := m.typing.action.EmptyMeans; means != "" {
		hint = "  enter accepts, " + means + ", esc cancels"
	}
	return prompt.Render(" "+m.typing.action.Asks+" ") + m.typing.row.Name() + "  " +
		m.input + prompt.Render("_") + faint.Render(hint)
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
		// A word that used to open a view says what to type now, for the same reason: enter is
		// about to refuse it rather than run it.
		if instead, gone := moved(m.input); gone {
			return faint.Render("   " + instead)
		}
		return faint.Render("   enter runs this as a krewe command")
	}
	return faint.Render("   " + strings.Join(names, "  "))
}

// wizardPrompt shows what has been typed, asterisks for a secret. A step that chooses from what the
// system already has lists it: asking somebody to name a workspace they cannot see leaves them able
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

// breadcrumb is where you are with the view you are in as a chip, so "acme/house-bills <jobs>" says
// both where you are and what escape goes back to.
//
// The address a person could type rather than the titles they read, because this is the line somebody
// copies to reach the same place from a command line. A job is where the two differ: it reads as its
// title and it is addressed by the identifier the listing prints beside it.
func (m Model) breadcrumb() string {
	line := " "
	if where := m.Position(); where != "" {
		line += faint.Render(where) + " "
	}
	line += chip.Render("<" + m.active.Name + ">")
	if len(m.stack) > 0 {
		line += faint.Render("   esc to go back")
	}
	// What has been typed and not yet acted on: a count, or the first g of gg. The console holds
	// those keys waiting for the rest of the sequence, and holding a keypress while showing nothing
	// looks exactly like dropping it.
	if typed := m.counted + m.pendingHalf; typed != "" {
		line += prompt.Render("   " + typed)
	}
	return line
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

// systemLines describe the system this console is pointed at: where it is, where you are standing in it,
// and what it is running underneath. They were the header until the header had no room left for the
// wordmark, and they are also the stats view, which is the one to leave open beside what you are
// doing.
func (m Model) systemLines() []string {
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
		add("State", statePhrase(m.info.State))
	}
	if len(lines) == 0 {
		lines = append(lines, faint.Render("still asking what this control plane is running"))
	}
	return lines
}

// systemBlock is what the system is, drawn at full width above the keys. The header carries the wordmark
// and which build this is; everything it used to carry is here.
func (m Model) systemBlock() []string {
	lines := []string{crumb.Render("  this system")}
	for _, described := range m.systemLines() {
		lines = append(lines, "    "+described)
	}
	return append(lines, "")
}

// summaryLine is the line drawn above the columns, or empty where the view has none.
//
// Nothing is drawn over the help or over a command's output: both take the panel from the rows, and
// a total about a listing that is not on screen is a line about nothing.
func (m Model) summaryLine() string {
	if m.mode == modeHelp || m.mode == modeReading {
		return ""
	}
	return m.summary.line
}
