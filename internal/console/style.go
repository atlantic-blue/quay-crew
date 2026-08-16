package console

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

// The same colours again as escape sequences, for the per cell colouring in a listing. Lipgloss
// styles a whole string; a row is padded to its columns first and then coloured cell by cell, so
// these are written on directly and the widths cannot move.
const (
	dimCode        = "\x1b[2m"
	offCode        = "\x1b[0m"
	boldCode       = "\x1b[1m"
	ansiRedCode    = "\x1b[31m"
	ansiGreenCode  = "\x1b[32m"
	ansiYellowCode = "\x1b[33m"
	ansiCyanCode   = "\x1b[36m"
)

var (
	// headerBar is the column header: black on green, full width.
	headerBar = lipgloss.NewStyle().Foreground(ansiBlack).Background(ansiGreen)
	// selectedRow is the cursor line: black on cyan, full width.
	selectedRow = lipgloss.NewStyle().Foreground(ansiBlack).Background(ansiCyan)
	// statusKey labels a value in the status block: "Address:", "Model:".
	statusKey = lipgloss.NewStyle().Foreground(ansiCyan)
	// hotKey is a key in the hints, wrapped in angle brackets so it reads as a key rather than a
	// word: <a>, <ctrl-d>.
	hotKey = lipgloss.NewStyle().Foreground(ansiCyan).Bold(true)
	// hotKeyLabel is what that key does.
	hotKeyLabel = lipgloss.NewStyle().Faint(true)
	// mark is the wordmark in the top right.
	mark = lipgloss.NewStyle().Foreground(ansiGreen)
	// chip is the view you are in, at the bottom left, black on green the way the column header is.
	chip = lipgloss.NewStyle().Foreground(ansiBlack).Background(ansiGreen)
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

// hint renders one key hint, for example "<s> Shell". The angle brackets are k9s's, and they earn
// their place: they say the thing inside is a key rather than a word.
func hint(key, label string) string {
	return hotKey.Render("<"+key+">") + " " + hotKeyLabel.Render(label)
}

// namePalette is 256 colour codes for names. The first twelve are the sessions tool's set, so the
// listings look like each other, and twelve more follow them.
//
// Twelve was not enough, and this is measured rather than assumed: over the fourteen workspace and
// project names on this crew, twelve colours put "atlantic-blue" and "itv" on the same one, and both
// are workspaces. Ten names into twelve slots collides by arithmetic, so a different hash does not
// fix it: FNV-1a over the same names collided three times where this one collided once. Twenty four
// leaves every workspace distinct and collides only between two pairs of projects in different
// workspaces.
var namePalette = []int{
	75, 114, 213, 179, 150, 209, 141, 110, 168, 84, 222, 117,
	80, 176, 108, 215, 146, 204, 115, 183, 109, 173, 78, 139,
}

// colourOfName is a stable colour for a name: the same workspace is the same colour on every refresh
// and in every view, which is what lets the eye find its rows without reading them.
//
// A hash into twelve colours collides eventually, and that is accepted: two workspaces sharing a
// colour is a smaller problem than thirty rows sharing one.
func colourOfName(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	hash := uint32(0)
	for _, char := range name {
		hash = hash*31 + uint32(char)
	}
	return fmt.Sprintf("\x1b[38;5;%dm", namePalette[int(hash)%len(namePalette)])
}

// colourOfMode is what a thread may do, coloured by how much that is. This is the cell it costs most
// to misread, so it is the one cell coloured by meaning rather than by identity.
func colourOfMode(mode string) string {
	switch mode {
	case "plan":
		return ansiGreenCode
	case "dangerous":
		return ansiRedCode
	default:
		return ""
	}
}

// colourOfTokens dims a number nobody acts on and marks one they might. The threshold is the sessions
// tool's, which is the listing this is being made to match.
func colourOfTokens(cell string) string {
	if strings.HasSuffix(cell, "M") {
		return ansiYellowCode
	}
	return dimCode
}

// colourOfStatus puts a row's state in the one cell that names it, rather than over the whole line.
// A line drawn in a single colour cannot carry any of the others, so colouring every row by its state
// costs the workspace, the project and the mode their colours, and a listing of thirty rows comes out
// in two.
//
// The words are the control plane's, so this and stateFromStatus read the same set and a status
// neither of them knows stays uncoloured rather than being guessed at. The stale mark rides on the
// same cell, and it is the state before it that decides the colour.
func colourOfStatus(cell string) string {
	status := cell
	if before, _, marked := strings.Cut(cell, " "); marked {
		status = before
	}
	switch stateFromStatus(status) {
	case StateFailed:
		return ansiRedCode
	case StateBusy:
		return ansiYellowCode
	case StateReady:
		return ansiGreenCode
	case StateStopped:
		return dimCode
	default:
		return ""
	}
}

// colourOfAge is how long ago a thread was touched, coloured by how long that is: the sessions tool's
// three bands, so a fresh thread, a thread from this morning and a thread from last week are told
// apart without reading the number.
//
// It reads the suffix compactDuration writes rather than a duration, because a column holds the text
// that was already laid out and cutting it back into a time would be inventing one.
func colourOfAge(cell string) string {
	if len(cell) < 2 {
		return dimCode
	}
	number, unit := cell[:len(cell)-1], cell[len(cell)-1]
	switch unit {
	case 's':
		return ansiGreenCode
	case 'm':
		// Fifteen minutes is the sessions tool's line between working and left alone.
		if minutes, err := strconv.Atoi(number); err == nil && minutes < 15 {
			return ansiGreenCode
		}
		return ansiYellowCode
	case 'h':
		return ansiYellowCode
	case 'd':
		return ansiRedCode
	default:
		return dimCode
	}
}

// colourOfWritten says whether a level of context exists, which is the only question that view is
// asked. Green for a level that says something, dim for one that says nothing yet.
func colourOfWritten(cell string) string {
	if cell == "written" {
		return ansiGreenCode
	}
	return dimCode
}

// place is a path, a repository or an engine: somewhere rather than something. Cyan is where the
// sessions tool puts a repository, and this is the same cell in a different listing.
func place(string) string { return ansiCyanCode }

// heading is the cell a row is about, when the row's own name is what carries it: the feature a
// scenario proves, the key a binding is on. Bold rather than coloured, because it sits next to cells
// that are already carrying colour of their own.
func heading(cell string) string {
	if strings.TrimSpace(cell) == "" {
		return ""
	}
	return boldCode
}

// dim is anything secondary: an identifier, an age, a count that is only there for completeness.
func dim(string) string { return dimCode }
