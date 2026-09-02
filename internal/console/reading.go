package console

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The panel of text over the rows, and the keys that scroll it and close it.
//
// Two things arrive here: what a command printed, and what a row is about when its cells can only
// hold a fragment of it. Both are the same kind of thing, something to read, so both are drawn the
// same way and closed by the same key.

// showCommandOutput puts what a command said on the screen.
//
// A command that failed shows its output too, and the error under it: the words are the useful part,
// and "exit status 1" on its own tells nobody anything. A command that said nothing at all says so,
// because a blank panel reads as the console having broken rather than as an empty listing.
func (m Model) showCommandOutput(msg commandOutputMsg) Model {
	said := msg.output
	if strings.TrimSpace(said) == "" {
		said = "(it said nothing)"
	}
	m = m.showReading(":"+msg.typed, said)
	m.err = msg.err
	return m
}

// showReading puts text over the rows, under a title saying what is being read.
func (m Model) showReading(title, text string) Model {
	m.mode, m.readingTop = modeReading, 0
	m.readingTitle = title
	m.reading = strings.Split(strings.TrimRight(text, "\n"), "\n")
	return m
}

// updateReadingKey scrolls what is being read, and closes it on anything else. The same shape the
// help overlay has, because it is the same kind of thing: something to read, over the rows.
func (m Model) updateReadingKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.readingTop--
	case "down", "j":
		m.readingTop++
	case "pgup", "ctrl+b":
		m.readingTop -= m.bodyHeight()
	case "pgdown", "ctrl+f":
		m.readingTop += m.bodyHeight()
	default:
		m.mode, m.readingTop, m.err = modeBrowse, 0, nil
		m.reading, m.readingTitle = nil, ""
		return m, nil
	}
	if m.readingTop < 0 {
		m.readingTop = 0
	}
	if most := len(m.reading) - 1; m.readingTop > most {
		m.readingTop = most
	}
	return m, nil
}

// readingBody is the window of the text that fits, with the title at the top so a screen of words
// says what it is answering.
func (m Model) readingBody() []string {
	height := m.bodyHeight()
	if height < 1 {
		height = 1
	}
	// Cut and padded to exactly the panel's width: the frame puts a border either side of whatever
	// it is given, so a short line leaves the right border next to the text and a long one spills
	// past it and wraps the terminal. Either way the panel comes out ragged.
	lines := []string{m.fit(prompt.Render(m.readingTitle))}
	for _, line := range m.reading {
		lines = append(lines, m.fit(line))
	}
	top := m.readingTop
	if top > len(lines)-1 {
		top = len(lines) - 1
	}
	if top < 0 {
		top = 0
	}
	end := top + height
	if end > len(lines) {
		end = len(lines)
	}
	return lines[top:end]
}
