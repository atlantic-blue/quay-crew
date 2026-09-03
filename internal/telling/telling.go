// Package telling says a waiting job out loud, in one place, so every surface that carries the
// telling describes the same wait in the same words.
package telling

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// Line is the one line a surface prints for one waiting job: what it is, what it wants, and how
// long it has been waiting where the wait has passed the limit.
//
// The record holds a question of any length, and this line is one line wide, so a long question is
// cut here and the line says which command prints all of it. A cut that says nothing reads as the
// whole question, and a person answers half a question.
//
// The age is named only past the limit. A job that stopped a minute ago and one that stopped an hour
// ago are different things, and an age on every line is an age nobody reads.
func Line(one *quaycrewv1.Waiting) string {
	id := display.ShortID(one.GetJob())
	head := fmt.Sprintf("%s %s: ", id, wants(one.GetWhy()))
	age := ""
	if one.GetOverLimit() {
		age = fmt.Sprintf(" (waited %s)", job.Waited(waitedFor(one)))
	}
	said := strings.Join(strings.Fields(one.GetWant()), " ")
	if said == "" {
		return head + "it says nothing about what it wants" + age
	}
	if len(head)+len(said)+len(age) <= lineWidth {
		return head + said + age
	}
	where := fmt.Sprintf(" (%s %s)", theWholeQuestion, id)
	return head + cutTo(said, lineWidth-len(head)-len(age)-len(where)-len(ellipsis)) + ellipsis + where + age
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

// cutTo is the start of what a job wants, in at most room bytes, ending on a whole word where one
// ends near enough to the cut. It never splits a character, because half a character is a question
// mark on the screen of anybody whose question was not written in English.
func cutTo(said string, room int) string {
	if room < 1 {
		return ""
	}
	if len(said) <= room {
		return said
	}
	kept := said[:room]
	for len(kept) > 0 && !utf8.RuneStart(kept[len(kept)-1]) {
		kept = kept[:len(kept)-1]
	}
	if kept != "" && !utf8.ValidString(kept) {
		kept = kept[:len(kept)-1]
	}
	if word := strings.LastIndexByte(kept, ' '); word >= room/2 {
		kept = kept[:word]
	}
	return strings.TrimRight(kept, " ")
}

const (
	// lineWidth is the whole width of this line. Eighty is the narrowest terminal anybody still uses,
	// and the line prints above whatever command the person actually typed.
	lineWidth = 80
	// ellipsis marks where the cut fell.
	ellipsis = "…"
	// theWholeQuestion is the command that prints the question the line could only start. A mark on
	// its own says the text stops and says nothing about where the rest is, and the rest is the reason
	// a person wrote a question that long.
	theWholeQuestion = "krewe job show"
)
