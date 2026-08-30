package main

import (
	"fmt"
	"io"
	"strings"
)

// A listing that says nothing about where it looked cannot be read. The operator ran `quay job
// list`, saw one row, and asked where the other nine were: they were in the project next door, and
// a narrowed result and an empty crew look identical on the screen. So every listing here ends by
// naming what it read, the way k9s keeps the namespace in its header.
//
// Where the listing narrows to the place the operator is standing, it also says the word that
// widens it. Advice that cannot be typed is worse than none, so the word is the one the listing
// takes: `crew`, which already means the level above every workspace everywhere else in this tool.
type scope struct {
	// what the listing holds, as a plural noun: jobs, sessions, projects.
	what string
	// where it looked, as an address. Empty means it read the whole crew.
	where string
	// wider is the sentence that says how to look further, and is empty on a listing that already
	// read everything it can reach.
	wider string
}

// wholeCrew reports whether the listing read every workspace rather than one address.
func (s scope) wholeCrew() bool { return s.where == "" }

// crewWide is the scope of a listing that read every workspace.
func crewWide(what string) scope { return scope{what: what} }

// narrowedTo is the scope of a listing that read one address, against the command that widens it.
func narrowedTo(what, where, wider string) scope {
	return scope{what: what, where: strings.TrimSpace(where), wider: wider}
}

// nothing says an empty listing, which is the case the operator cannot read at all: "no jobs" from
// one project and "no jobs" from a crew holding nine of them are the same three words.
func (s scope) nothing(out io.Writer) {
	if s.where == "" {
		fmt.Fprintf(out, "no %s in this crew\n", s.what)
		return
	}
	fmt.Fprintf(out, "no %s in %s\n", s.what, s.where)
	if s.wider != "" {
		fmt.Fprintf(out, "%s\n", s.wider)
	}
}

// counted goes under a listing that had rows, and says the same thing the empty one says.
func (s scope) counted(out io.Writer, rows int) {
	if s.where == "" {
		fmt.Fprintf(out, "\n%s in this crew\n", plural(rows, s.what))
		return
	}
	fmt.Fprintf(out, "\n%s in %s", plural(rows, s.what), s.where)
	if s.wider != "" {
		fmt.Fprintf(out, ". %s", s.wider)
	}
	fmt.Fprintln(out)
}

// plural counts a noun: one job, two jobs.
func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, strings.TrimSuffix(noun, "s"))
	}
	return fmt.Sprintf("%d %s", count, noun)
}

// readsTheCrew reports whether the address typed is the word that means every workspace.
func readsTheCrew(typed string) bool { return strings.TrimSpace(typed) == crewScope }

// crewLevel is what a listing of what the crew itself holds says it read. Skills, roles and hooks
// sit at a level rather than at an address, so the crew is a place a listing can name.
const crewLevel = "the crew"

// heldBy is the scope of a listing of what one level holds. The crew's own is everything such a
// listing can reach, so it offers nothing wider.
func heldBy(what, where, wider string) scope {
	if where == crewLevel {
		return scope{what: what, where: crewLevel}
	}
	return narrowedTo(what, where, wider)
}
