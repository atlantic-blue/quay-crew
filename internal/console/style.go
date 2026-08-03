package console

import "github.com/charmbracelet/lipgloss"

// The palette is lifted from the operator's own hub tools (sessions, pulls, todo) so the console
// looks like it belongs next to them: black on green for the column header, black on cyan for the
// selected row and the footer key labels, bold key letters against those labels, and dim for
// anything secondary.
//
// These are the basic eight ANSI colours on purpose, not hex. They resolve through whatever theme
// the terminal already has, so the console matches the surrounding shell instead of fighting it.
const (
	ansiBlack  = lipgloss.Color("0")
	ansiRed    = lipgloss.Color("1")
	ansiGreen  = lipgloss.Color("2")
	ansiYellow = lipgloss.Color("3")
	ansiCyan   = lipgloss.Color("6")
)

var (
	// headerBar is the column header: black on green, full width.
	headerBar = lipgloss.NewStyle().Foreground(ansiBlack).Background(ansiGreen)
	// selectedRow is the cursor line: black on cyan, full width.
	selectedRow = lipgloss.NewStyle().Foreground(ansiBlack).Background(ansiCyan)
	// keyLabel is a footer hint's words, black on cyan, sitting right after its key letter.
	keyLabel = lipgloss.NewStyle().Foreground(ansiBlack).Background(ansiCyan)
	// keyLetter is the key itself, bold on the default background.
	keyLetter = lipgloss.NewStyle().Bold(true)
	// crumb is the breadcrumb line above the table.
	crumb = lipgloss.NewStyle().Bold(true)
	// faint is anything secondary: counts, timestamps, the scope note.
	faint = lipgloss.NewStyle().Faint(true)
	// alert is an error or a failed row.
	alert = lipgloss.NewStyle().Foreground(ansiRed)
	// busy is a row doing something right now.
	busy = lipgloss.NewStyle().Foreground(ansiYellow)
	// ready is a healthy, idle row.
	ready = lipgloss.NewStyle().Foreground(ansiGreen)
	// frame is the panel's border, dim so it separates without competing with the rows inside it.
	frame = lipgloss.NewStyle().Faint(true)
	// prompt is the command bar and the filter bar.
	prompt = lipgloss.NewStyle().Foreground(ansiCyan).Bold(true)
)

// styleFor returns the colour a row is drawn in when it is not the selected one. The selected row
// always wins, because a cursor that changes colour with its content is a cursor you lose.
func styleFor(state State) lipgloss.Style {
	switch state {
	case StateFailed:
		return alert
	case StateBusy:
		return busy
	case StateReady:
		return ready
	case StateStopped:
		return faint
	case StateUnknown:
		return lipgloss.NewStyle()
	default:
		return lipgloss.NewStyle()
	}
}

// hint renders one footer key hint, for example a bold "s" followed by "Shell" on cyan.
func hint(key, label string) string {
	return keyLetter.Render(key) + keyLabel.Render(label)
}
