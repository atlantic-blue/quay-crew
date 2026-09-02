package job

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// A plan is read by several roles with different jobs, and only what none of them could settle is
// put to a person.
//
// One reading finds what that reading looks for. A design named the address shape of a page and
// nobody asked what a person types into it, because the role that read the plan was not the role
// that needs an input and an output before it can write an example. The discipline this borrows from
// is example mapping: several people with different jobs write concrete examples for one rule, and
// the questions nobody can answer become explicit.
//
// So a reading writes rows. A later reader is handed the rows that are still open, reads the same
// plan through its own lens, and settles what it can. What every lens left open is what reaches a
// person, and nothing else does: a gate that puts every question every reader raised is a gate a
// person stops reading.
//
// A question is not a claim. A claim is something a plan takes to be true and it carries a check. A
// question is a hole where nobody holds a claim at all, so the two stay apart rather than sharing
// one word for two vocabularies.

const (
	// QuestionOpen is a row nobody settled. It is the only status that reaches a person.
	QuestionOpen = "open"
	// QuestionSettled is a row a later reader answered from its own lens.
	QuestionSettled = "settled"
)

const (
	// QuestionRowLimit is how long one question row may be. It is the title's ceiling, because both
	// are one line a person reads, and a question that needs a paragraph is a plan nobody can read.
	QuestionRowLimit = TitleLimit
	// QuestionRowCount is how many questions one reading may write.
	//
	// Three is chosen and not measured. The measurement that replaces it is the count of rows per
	// reader over the first month of real readings. What it protects is the person at the end: every
	// open row goes in front of them, and a reader that can write twenty makes a gate nobody reads.
	QuestionRowCount = 3
)

// Question is one thing a reading of a plan could not settle.
type Question struct {
	// Job is the job the row sits on.
	Job string
	// Seq is where this question sits in the order they were asked, counting from one. It is the
	// number a later reader settles by.
	Seq int
	// Text is the question, in one line.
	Text string
	// AskedBy is the role that wrote it, and empty where no role ran.
	AskedBy string
	// AskedIn is the job whose session wrote it, which is not the job the row sits on once the row
	// has been handed to a later reader. The ceiling counts by this, so a reader may write three of
	// its own however many it was handed.
	AskedIn string
	// Status is QuestionOpen or QuestionSettled.
	Status string
	// Answer is what settled it, and empty while it is open.
	Answer string
	// SettledBy is the role that settled it, and empty while it is open.
	SettledBy string
	AskedAt   time.Time
	SettledAt time.Time
}

// Open says whether this row still has to reach somebody.
func (q Question) Open() bool { return q.Status != QuestionSettled }

// TidyQuestionRow is a question as the system keeps it, and the refusal where it could not be kept.
func TidyQuestionRow(text string) (string, error) {
	tidy := TidySentence(text)
	switch {
	case tidy == "":
		return "", fmt.Errorf("a question says what this reading could not settle: write it in one line, " +
			"so the next reader and the person at the end can both read it")
	case len(tidy) > QuestionRowLimit:
		return "", fmt.Errorf("this question is %d bytes and a question may be %d: it is one line beside the "+
			"others, so ask the one thing and leave the working out of it", len(tidy), QuestionRowLimit)
	}
	return tidy, nil
}

// TidyRowAnswer is what settles a row, and the refusal where it could not be kept.
func TidyRowAnswer(answer string) (string, error) {
	tidy := TidySentence(answer)
	switch {
	case tidy == "":
		return "", fmt.Errorf("settling a row says what the answer is: a row settled with nothing is a row " +
			"that still has to reach a person, so leave it open instead")
	case len(tidy) > QuestionRowLimit:
		return "", fmt.Errorf("this answer is %d bytes and an answer may be %d: say what settles the row and "+
			"leave the working out of it", len(tidy), QuestionRowLimit)
	}
	return tidy, nil
}

// AlreadyAsked is the row this question repeats, and false where it asks something new.
//
// The measure is the words that carry the content, the same measure a brief is held to, so one hole
// named twice in the same words leaves one row however it was punctuated. It does not catch a
// rename: two readers naming one hole in different words write two rows, and a person reads both.
func AlreadyAsked(questions []Question, text string) (Question, bool) {
	asked := sortedWords(text)
	if len(asked) == 0 {
		return Question{}, false
	}
	for _, one := range questions {
		if sameWords(sortedWords(one.Text), asked) {
			return one, true
		}
	}
	return Question{}, false
}

// RoomForAQuestion is the refusal where this reading has written as many questions as it may.
//
// It counts what this reading wrote rather than what the row list holds, because a later reader is
// handed every open row and would otherwise be refused its first question for somebody else's work.
func RoomForAQuestion(questions []Question, askedIn string) error {
	written := 0
	for _, one := range questions {
		if one.AskedIn == askedIn {
			written++
		}
	}
	if written < QuestionRowCount {
		return nil
	}
	return fmt.Errorf("this reading has written %d questions and one reading may write %d: every open row "+
		"goes in front of the next reader and in front of the person at the end, so ask the %d that decide "+
		"the plan and settle the rest yourself", written, QuestionRowCount, QuestionRowCount)
}

// TheQuestion is the row with this number, and false where there is no such row.
func TheQuestion(questions []Question, seq int) (Question, bool) {
	for _, one := range questions {
		if one.Seq == seq {
			return one, true
		}
	}
	return Question{}, false
}

// OpenQuestions are the rows nobody settled, in the order they were asked.
func OpenQuestions(questions []Question) []Question {
	open := make([]Question, 0, len(questions))
	for _, one := range questions {
		if one.Open() {
			open = append(open, one)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Seq < open[j].Seq })
	return open
}

// RenderQuestions is the open rows as the next reader and the person at the end are handed them: one
// line each, numbered by the number a reader settles by.
//
// Empty where every row is settled, which is what a graph reads to decide whether anybody has to be
// asked at all. A reading that settled everything asks nothing.
func RenderQuestions(questions []Question) string {
	open := OpenQuestions(questions)
	if len(open) == 0 {
		return ""
	}
	lines := make([]string, 0, len(open))
	for _, one := range open {
		asked := ""
		if one.AskedBy != "" {
			asked = fmt.Sprintf(" (asked by %s)", one.AskedBy)
		}
		lines = append(lines, fmt.Sprintf("%d. %s%s", one.Seq, one.Text, asked))
	}
	return strings.Join(lines, "\n")
}

// CarriedQuestions are the rows a later reader is handed: the open ones, and never the earlier
// reader's answer.
//
// A reader that cannot see the earlier reading cannot be led by it, and it also cannot use it. That
// is the trade example mapping already makes: the questions are public and the reasoning behind them
// is not. The row keeps the reading it was written in, so the ceiling still counts by reader.
func CarriedQuestions(questions []Question, onto string) []Question {
	open := OpenQuestions(questions)
	carried := make([]Question, 0, len(open))
	for _, one := range open {
		one.Job, one.Answer, one.SettledBy, one.SettledAt = onto, "", "", time.Time{}
		carried = append(carried, one)
	}
	return carried
}

// sortedWords are the content words of a question, in one order, so two questions can be compared
// however they were written.
func sortedWords(text string) []string {
	words := contentWords(text)
	sort.Strings(words)
	return words
}

// sameWords says whether two questions carry the same content words.
func sameWords(one, other []string) bool {
	if len(one) != len(other) || len(one) == 0 {
		return false
	}
	for at := range one {
		if one[at] != other[at] {
			return false
		}
	}
	return true
}

// ErrNoSuchQuestion is a settle against a row nobody wrote. It is a refusal rather than a silence,
// because a reader settling a number it was never handed has read the wrong list.
var ErrNoSuchQuestion = errors.New("job: there is no question with that number on this job")

// ErrQuestionSettled is a settle against a row an earlier reader already settled. A row settled
// twice would take one reader's answer over another's for no reason a person could read.
var ErrQuestionSettled = errors.New("job: that question is settled already")
