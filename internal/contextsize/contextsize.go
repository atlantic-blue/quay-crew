// Package contextsize says how big a level of context is, and when it is big enough to be a problem
// on its own.
//
// Context has levels, and the outer ones reach furthest. The crew's level is read by every session
// in every workspace, before that session has read a line of the repository it was made to work on.
// Nothing anywhere said how big any level was, so the crew's grew to 100,179 characters without
// anybody deciding to make it that big. A rule added to a level that size is a rule nobody finds,
// and on 29 August 2026 a cost rule went onto a workspace instead, because the crew's level would
// have buried it. That is the crew level's whole purpose undone.
//
// So a level states its size wherever it is listed or written, and states what to do about it once
// it is over the mark. It is the shape the sandbox memory reading already has: the number is always
// there, and the paragraph about what to do arrives only when it is needed.
package contextsize

import (
	"fmt"
	"strconv"
	"strings"
)

// Mark is the number of characters at which a level stops being a page somebody reads and becomes
// somewhere a rule can hide. Twenty thousand is about seven pages of prose, and about five thousand
// tokens at four characters a token, which is the estimate every tokeniser agrees on for English.
//
// It is a mark and not a limit. Nothing is refused for being over it: a crew that wants a long level
// keeps one, and the point is that it chose to.
const Mark = 20_000

// A Reading is one level of context and how big it is.
type Reading struct {
	// Scope is "crew", "workspace", "project" or "session".
	Scope string
	// Name is what that level is called where a person reads it.
	Name string
	// Characters is how long its body is.
	Characters int
}

// Read measures one level.
func Read(scope, name, body string) Reading {
	return Reading{Scope: scope, Name: name, Characters: len(body)}
}

// Over says whether this level is past the mark.
func (r Reading) Over() bool { return r.Characters > Mark }

// Cell is the size as a listing shows it, in one column. A level nobody has written to says so:
// zero characters is the normal state of a fresh crew, and a column of noughts reads as a defect.
func (r Reading) Cell() string {
	if r.Characters == 0 {
		return "nothing written yet"
	}
	if r.Over() {
		return commas(r.Characters) + " over the mark"
	}
	return commas(r.Characters)
}

// Note is the one line a listing carries under it, and empty for a level under the mark. It says the
// number and who reads it, because "over the mark" on its own says nothing about what it costs.
func (r Reading) Note() string {
	if !r.Over() {
		return ""
	}
	return fmt.Sprintf("%s is over the %s character mark, at %s. %s",
		r.Label(), commas(Mark), commas(r.Characters), r.reach())
}

// Say is what a person is told at the moment they write a level this big: the note, and then the one
// thing to do about it. Empty for a level under the mark, so a caller prints it without asking.
func (r Reading) Say() string {
	if !r.Over() {
		return ""
	}
	return fmt.Sprintf("%s\n\n%s\n\n%s", r.Note(), r.cost(), r.advice())
}

// Label names the level the way a person says it: "crew", or "workspace atlantic-blue". The crew is
// the crew, because there is one and it has no name of its own, and a line built from the scope and
// the name side by side read "crew crew".
func (r Reading) Label() string {
	if r.Scope == "crew" || r.Name == "" {
		return r.Scope
	}
	return r.Scope + " " + r.Name
}

// reach is who carries this level, which is the whole reason its size matters. A level read by one
// session costs that session; the crew's is read by all of them.
func (r Reading) reach() string {
	switch r.Scope {
	case "crew":
		return "Every session in every workspace reads it."
	case "workspace":
		return "Every session in this workspace reads it."
	case "project":
		return "Every session in this project reads it."
	default:
		return "This session reads it."
	}
}

// cost says what the size buys nobody. A session reads its context before it reads the repository it
// was made to work on, so the level is the first thing it carries and the last place a rule is found.
func (r Reading) cost() string {
	return fmt.Sprintf("A session reads this level before it reads a line of the repository it works on.\n"+
		"So a rule you add here is a rule in %s characters.", commas(r.Characters))
}

// advice is the one move that makes a level smaller: put what is not true of everything under it one
// level down. A project has no level under it that a person writes, so its answer is the repository,
// which the session reads anyway.
func (r Reading) advice() string {
	switch r.Scope {
	case "crew":
		return "Move what is not true of every workspace down a level:\n" +
			"  quay context set <workspace>            what one organisation does\n" +
			"  quay context set <workspace>/<project>  what one piece of work does"
	case "workspace":
		return "Move what is not true of every project in it down a level:\n" +
			"  quay context set <workspace>/<project>  what one piece of work does"
	default:
		return "Put what belongs to the code in the repository, which every session in it reads\n" +
			"anyway. Keep this level for what the repository cannot say."
	}
}

// Characters writes a count the way a sentence says one, so a command that reports what it wrote and
// a listing that reports what is there spell the same number the same way.
func Characters(count int) string {
	if count == 1 {
		return "1 character"
	}
	return commas(count) + " characters"
}

// commas groups a count in threes. A hundred thousand characters read as 100179 is a number nobody
// takes in, and the size of this thing is the entire point of printing it.
func commas(count int) string {
	digits := strconv.Itoa(count)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return sign + out.String()
}
