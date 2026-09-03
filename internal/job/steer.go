package job

import (
	"fmt"
	"time"
)

// A steer is one moment the operator had to say something the system should have known.
//
// It exists because the number that scores a job was counted by hand. Thirteen of them across two
// days of the acceptance job were written out afterwards by the person who watched it, numbered in a
// markdown file, and nothing in the system knew any of them happened. So every improvement after it
// was argued rather than measured: four issues came out of that job and nobody could say by how much
// the number moved.
//
// A steer looks exactly like an ordinary message, because that is what it is. Only the person typing
// it knows it is one, which is why the mark is theirs to make and why it has to be one word: a mark
// that takes a form to fill in does not get made in the moment.

// SteerLimit is how long the text of a steer is expected to be. It is the title's guide, because a
// report prints one line for each one and what belongs underneath a line belongs in the issue it
// opens.
//
// It refuses nothing. A steer is made in the moment, with a hand already on the keyboard, so a
// refusal for length is the system arguing with somebody who is telling it that it went wrong. The
// listing cuts what it draws and says so.
const SteerLimit = TitleLimit

// WhatASteerIs is the definition, and it ships with the tool because a count is worth nothing when
// the definition drifts. It is printed where a steer is recorded and at the head of the report, so
// the number means the same thing in December as it did in August.
const WhatASteerIs = "A steer is one moment the operator had to say something the system should have " +
	"known, asked for, or refused on its own. Setup before the job was declared is not a steer, and " +
	"an answer to a question the system asked is not a steer."

// Steer is one steer as the system keeps it: what was said, when, and which job it landed on.
//
// It belongs to that job and to nothing else. A job belongs to its project and nothing sits under
// it, so there is no tree to carry the count up, and reading one job's steers is one query on the
// job the operator was looking at when they made the mark.
type Steer struct {
	ID         string
	Job        string
	Workspace  string
	Project    string
	Text       string
	OccurredAt time.Time
}

// TidySteer is the text as it is stored: one line, however it arrived.
func TidySteer(text string) string { return TidySentence(text) }

// Steered refuses a steer nothing could be read back from.
//
// Held at the write, while the person who typed it is looking. A blank line in a report a month
// later has nobody to ask what it was.
func Steered(text string) error {
	if TidySteer(text) == "" {
		return fmt.Errorf("a steer carries what you had to say, so say what you had to say: " +
			"krewe steer \"the workspace has no secrets\"")
	}
	return nil
}

// SteerLine is one steer as a listing draws it, cut to the guide and marked where it was cut.
//
// The record keeps every word. This line is beside a time and an identifier in a terminal, so a
// steer that ran over several lines would push the ones under it off a short screen. The mark says
// the text goes on, because a line that stops without a mark reads as the whole thing.
func SteerLine(text string) string {
	if len(text) <= SteerLimit {
		return text
	}
	return holdTo(text, SteerLimit)
}

// Compared is how this job's count reads against the one before it, which is the question the count
// exists to answer. A reader given two numbers subtracts them; a reader given a sentence sees the
// direction.
//
// A count of -1 for the job before means there was none, which is the first job in a project.
func Compared(count, before int) string {
	switch {
	case before < 0:
		return "the first job here, so there is nothing to compare it with"
	case count == before:
		return "the same as the job before it"
	case count < before:
		return fmt.Sprintf("%d fewer than the job before it", before-count)
	default:
		return fmt.Sprintf("%d more than the job before it", count-before)
	}
}

// Steers is how a count reads in a line, so one job's score is written the same way everywhere.
func Steers(count int) string {
	if count == 1 {
		return "1 steer"
	}
	return fmt.Sprintf("%d steers", count)
}
