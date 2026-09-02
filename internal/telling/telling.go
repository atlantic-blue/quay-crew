// Package telling says a waiting job out loud, in one place, so every surface that carries the
// telling describes the same wait in the same words.
package telling

import (
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// Line is the one line a surface prints for one waiting job: what it is, what it wants, and how
// long it has been waiting where the wait has passed the limit.
//
// The age is named only past the limit. A job that stopped a minute ago and one that stopped an hour
// ago are different things, and an age on every line is an age nobody reads.
func Line(one *quaycrewv1.Waiting) string {
	line := fmt.Sprintf("%s %s: %s", display.ShortID(one.GetJob()), wants(one.GetWhy()), oneLine(one.GetWant()))
	if one.GetOverLimit() {
		line += fmt.Sprintf(" (waited %s)", job.Waited(waitedFor(one)))
	}
	return line
}

// Count is how a surface with room for one line says how many jobs wait, and empty where none do.
// Empty rather than "nothing waits", because a line that prints on every command forever is a line
// that stops being read.
func Count(waiting []*quaycrewv1.Waiting) string {
	switch len(waiting) {
	case 0:
		return ""
	case 1:
		return "1 job waits for you"
	default:
		return fmt.Sprintf("%d jobs wait for you", len(waiting))
	}
}

// wants says what kind of answer this wait needs, in a person's words rather than in the phase.
func wants(why string) string {
	switch why {
	case job.WaitingAsking:
		return "asks"
	case job.WaitingChecks:
		return "is red"
	default:
		return "stopped"
	}
}

// waitedFor is how long the wait has lasted, as the system measured it at the moment of the read. It
// is taken from the count of seconds rather than from the clock here, so every surface drawing the
// same answer says the same number.
func waitedFor(one *quaycrewv1.Waiting) time.Duration {
	return time.Duration(one.GetWaitedSeconds()) * time.Second
}

// oneLine holds what a job wants to a single line, since these are printed above a command's output
// and drawn inside a bordered panel. A question that wrapped over four lines would push the output
// off a short screen.
func oneLine(said string) string {
	said = strings.Join(strings.Fields(said), " ")
	if said == "" {
		return "it says nothing about what it wants"
	}
	if len(said) <= tellingWidth {
		return said
	}
	return said[:tellingWidth-1] + "…"
}

// tellingWidth is how much of what a job wants one line carries. Eighty is the narrowest terminal
// anybody still uses, and the identifier and the verb in front of it take about twenty.
const tellingWidth = 60
