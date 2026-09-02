package console

import (
	"context"
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/telling"
	tea "github.com/charmbracelet/bubbletea"
)

// The console is the surface that can tell a person without being navigated to.
//
// It reloads every three seconds already. When the count of jobs waiting for a person goes up it
// rings the terminal bell once and draws one line above the panel, whichever view is open. The bell
// is the part that reaches a tab that is not in front, which is where the person actually was on 1
// September 2026 while four jobs waited.
//
// One ring for each rise, never one for each poll: a bell every three seconds is a bell somebody
// turns off, and a console that has been silenced tells nobody anything.

// surfaceConsole is what the console calls itself on the record when it carries the telling.
const surfaceConsole = "console"

// waitingMsg is what waits for a person, as the system last answered it.
type waitingMsg struct{ waiting []*quaycrewv1.Waiting }

// Bell rings the terminal bell. The console is given one rather than writing to a file descriptor
// itself, so a scenario can count the rings and Update stays a pure function of its messages.
type Bell func()

// WithBell says how the console rings. Without one it does not ring, and the line is still drawn: a
// console with no bell is quieter, never blind.
func (m Model) WithBell(ring Bell) Model {
	m.bell = ring
	return m
}

// Waiting is what the console currently says waits for a person, which a scenario reads to say
// whether the telling arrived without anybody typing a command.
func (m Model) Waiting() []*quaycrewv1.Waiting { return m.waits }

// waitingCmd asks the system what waits for a person.
//
// It says which surface is asking, so the first one to name a waiting job records that the telling
// went out. Failure is silent: a console that could not reach the system already says so in its
// stats view, and an error screen over a listing because a count could not be read would be worse
// than the silence this exists to end.
func waitingCmd(client quaycrewv1.ControlPlaneServiceClient) tea.Cmd {
	if client == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		answer, err := client.GetWaiting(ctx, &quaycrewv1.GetWaitingRequest{Surface: surfaceConsole})
		if err != nil {
			return nil
		}
		return waitingMsg{waiting: answer.GetWaiting()}
	}
}

// ringCmd rings the bell once, off the update loop, so Update itself writes nothing.
func ringCmd(ring Bell) tea.Cmd {
	if ring == nil {
		return nil
	}
	return func() tea.Msg {
		ring()
		return nil
	}
}

// applyWaiting takes the answer and says whether the console should ring.
//
// It rings on a rise and on nothing else. A count that stays the same is the same wait still
// waiting; a count that falls is somebody answering, which is the good news and needs no bell.
func (m Model) applyWaiting(msg waitingMsg) (Model, tea.Cmd) {
	rose := len(msg.waiting) > len(m.waits)
	m.waits = msg.waiting
	if !rose {
		return m, nil
	}
	return m, ringCmd(m.bell)
}

// waitingLine is the line the console draws above the panel when something waits for a person, and
// empty when nothing does.
//
// One line whatever the count. The console is a full screen of rows already, and a telling that grew
// with the number of waiting jobs would push the rows it is drawn over off the screen. The longest
// wait is the one named, because it is the one that has cost the most time.
func (m Model) waitingLine() string {
	if len(m.waits) == 0 {
		return ""
	}
	line := telling.Count(m.waits)
	if len(m.waits) == 1 {
		return fmt.Sprintf("%s: %s", line, telling.Line(m.waits[0]))
	}
	return fmt.Sprintf("%s, longest first: %s", line, telling.Line(m.waits[0]))
}
