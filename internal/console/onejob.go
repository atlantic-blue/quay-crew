package console

import (
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
)

// One row read on its own, and the job that is the first of them.
//
// A listing answers which ones there are. It cannot answer what one of them is: a job states a
// sentence a person gets back, it stands in one of four stages, and one or more sessions work on it.
// Enter on a job used to open the conversation of the session under it, which is one level past the
// thing a person pointed at, so the job itself had no screen at all.

// Screen is one row read on its own. Stay holds its place however far the reading is scrolled, and
// Prose is what scrolls under it.
//
// The split is the whole point on a short window. A laptop pane is about twenty four rows, and the
// lines that say which sessions are working are the first thing a body of prose pushes off the
// bottom: a person watching two sessions build must not lose them by reading.
type Screen struct {
	// Title is what the panel is headed with, inside the name of the view it was opened from.
	Title string
	// Stay are the lines that hold their place: what this is, where it stands, and what works on it.
	Stay []string
	// Prose is what is read, wrapped to the panel and scrolled a line at a time.
	Prose []string
}

// showScreen puts one row on the screen in place of the listing it was opened from.
//
// The address is the row's, because a person reading a job stands at that job: the footer says
// acme/house-bills/33333333 while they read it, and escape takes them back up to the listing. A
// screen a person cannot address is a screen they cannot tell somebody else how to reach.
func (m Model) showScreen(screen Screen, row Row) Model {
	m.mode, m.screen, m.screenTop, m.screenAt, m.err = modeScreen, screen, 0, row.Typed(), nil
	return m
}

// updateScreenKey scrolls the prose and closes on anything else, which is how the panel over the
// rows already behaves. The keys that scroll move the prose alone: the block above it is what a
// person came to watch.
func (m Model) updateScreenKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.screenTop--
	case "down", "j":
		m.screenTop++
	case "pgup", "ctrl+b":
		m.screenTop -= m.proseHeight()
	case "pgdown", "ctrl+f":
		m.screenTop += m.proseHeight()
	default:
		m.mode, m.screenTop, m.screen, m.screenAt, m.err = modeBrowse, 0, Screen{}, "", nil
		return m, nil
	}
	if most := len(m.proseLines()) - m.proseHeight(); m.screenTop > most {
		m.screenTop = most
	}
	if m.screenTop < 0 {
		m.screenTop = 0
	}
	return m, nil
}

// screenBody is the block that stays, and then as much of the prose as is left over. The prose is
// what gives way on a short window, because the lines above it are the ones a person is watching.
func (m Model) screenBody() []string {
	height := m.bodyHeight() + 1
	stay := m.stayLines()
	// One row of prose at the least. A window too short for both is a window where what is left is
	// the block that stays, cut from the bottom.
	if len(stay) > height-1 {
		stay = stay[:max(height-1, 0)]
	}
	lines := make([]string, 0, height)
	for _, line := range stay {
		lines = append(lines, m.fit(line))
	}
	prose := m.proseLines()
	top := m.screenTop
	if top > len(prose) {
		top = len(prose)
	}
	for _, line := range prose[top:] {
		if len(lines) == height {
			break
		}
		lines = append(lines, m.fit(line))
	}
	for len(lines) < height {
		lines = append(lines, m.fit(""))
	}
	return lines
}

// proseLines is the reading broken to the width of the panel. A sentence is prose and a terminal is
// not that wide, so a line left whole is a line cut at the border, which takes the end of it away.
func (m Model) proseLines() []string { return m.wrapped(m.screen.Prose) }

// stayLines is the block that holds its place, broken to the same width.
func (m Model) stayLines() []string { return m.wrapped(m.screen.Stay) }

// wrapped breaks lines to the room inside the panel.
func (m Model) wrapped(lines []string) []string {
	broken := make([]string, 0, len(lines))
	for _, line := range lines {
		broken = append(broken, wrapTo(line, m.innerWidth())...)
	}
	return broken
}

// proseHeight is how many rows the prose has once the block that stays has taken what it needs.
func (m Model) proseHeight() int {
	height := m.bodyHeight() + 1 - len(m.stayLines())
	if height < 1 {
		return 1
	}
	return height
}

// oneJob is a job read on its own: what it is, where it stands, what works on it, and the sentence
// it serves.
//
// The sentence is the first thing in the prose because it is what the rest of the job is read
// against: everything else the job says about itself is evidence for it.
func oneJob(one *quaycrewv1.Job, runs []*quaycrewv1.Execution) Screen {
	stage := job.StageOfWire(one)
	stay := []string{fmt.Sprintf("%s  %s", display.ShortID(one.GetId()), one.GetTitle()), whereItStands(one, stage)}
	return Screen{
		Title: display.ShortID(one.GetId()),
		Stay:  append(stay, sessionsWorkingOn(one, runs)...),
		Prose: whatTheJobSays(one, stage),
	}
}

// whereItStands is the pair a reader needs: the phase says the system is waiting, and the stage says
// what it is waiting for. A job can be in the ideation stage and the asking phase at the same time.
func whereItStands(one *quaycrewv1.Job, stage job.Stage) string {
	if stage.Outside != "" {
		return fmt.Sprintf("no stage, phase %s: %s", one.GetPhase(), stage.Outside)
	}
	return fmt.Sprintf("%s, phase %s", stage.Where(), one.GetPhase())
}

// sessionsWorkingOn names every session working on this job. A job runs in one session of its own
// until it fans out, and then one session for each vertical runs under it, so both are read: a
// person watching a build of three verticals is watching three conversations at once.
func sessionsWorkingOn(one *quaycrewv1.Job, runs []*quaycrewv1.Execution) []string {
	type working struct{ id, doing string }
	found := make([]working, 0, len(runs)+1)
	seen := make(map[string]bool, len(runs)+1)
	if id := one.GetSession(); id != "" {
		found, seen[id] = append(found, working{id: id, doing: one.GetPhase()}), true
	}
	for _, run := range runs {
		id := run.GetSession()
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		found = append(found, working{
			id:    id,
			doing: fmt.Sprintf("%s, %s", job.RunCalled(run.GetStage(), int(run.GetNumber())), run.GetPhase()),
		})
	}
	if len(found) == 0 {
		return []string{"no session yet: a job reaches a session when a controller starts it"}
	}
	lines := make([]string, 0, len(found)+1)
	lines = append(lines, saidTheSessions(len(found)))
	for _, one := range found {
		lines = append(lines, fmt.Sprintf("  %s  %s", display.ShortID(one.id), one.doing))
	}
	return lines
}

// saidTheSessions reads for one and for several, because a line that says "1 sessions" is a line
// that says nobody read it.
func saidTheSessions(count int) string {
	if count == 1 {
		return "one session is working on it:"
	}
	return fmt.Sprintf("%d sessions are working on it:", count)
}

// whatTheJobSays is the reading itself: the sentence a person gets back, what the stage before this
// one closed on, what opens the next, where it stands inside the one it is in, and what a person
// said about what it understood.
func whatTheJobSays(one *quaycrewv1.Job, stage job.Stage) []string {
	lines := []string{"for a person: " + one.GetProduct()}
	if one.GetProduct() == "" {
		lines = []string{"this job states no sentence, so it is an errand and runs no stages"}
	}
	for _, line := range []string{stage.Closed, stage.Opens, stage.Doing} {
		if line != "" {
			lines = append(lines, "  "+line)
		}
	}
	if answered := one.GetIdeationAnswer(); answered != "" {
		lines = append(lines, "", "a person answered what it understood: "+answered)
	}
	if outcome := one.GetOutcome(); outcome != "" {
		lines = append(lines, "", fmt.Sprintf("outcome: %s, %s", outcome, job.OutcomeMeans(outcome)))
	}
	if reason := one.GetReason(); reason != "" {
		lines = append(lines, "", reason)
	}
	return lines
}
