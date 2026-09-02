package main

import (
	"fmt"
	"io"
	"strings"
)

// A listing that says nothing about where it looked cannot be read. The operator ran `krewe job
// list`, saw one row, and asked where the other nine were: they were in the project next door, and
// a narrowed result and an empty system look identical on the screen. So every listing here ends by
// naming what it read, the way k9s keeps the namespace in its header.
//
// Where the listing narrows to the place the operator is standing, it also says the word that
// widens it. Advice that cannot be typed is worse than none, so the word is the one the listing
// takes: `system`, which already means the level above every workspace everywhere else in this tool.
type scope struct {
	// what the listing holds, as a plural noun: jobs, sessions, projects.
	what string
	// where it looked, as an address. Empty means it read the whole system.
	where string
	// wider is the sentence that says how to look further, and is empty on a listing that already
	// read everything it can reach.
	wider string
	// directories is the sentence that says how to get from a row to the directory it is kept in. A
	// listing is where somebody looks when they are holding a file and do not know where to put it,
	// and until this line was here the answer was not on any screen in the system.
	//
	// A sentence rather than a column. A path is around a hundred characters and `krewe sessions`
	// already carries thirteen columns, so a column of them makes every row wrap and the listing
	// stops being readable at all.
	directories string
}

// systemWide is the scope of a listing that read every workspace.
func systemWide(what string) scope { return scope{what: what} }

// narrowedTo is the scope of a listing that read one address, against the command that widens it.
func narrowedTo(what, where, wider string) scope {
	return scope{what: what, where: strings.TrimSpace(where), wider: wider}
}

// locatable marks a listing whose rows are kept in a directory somebody may want to put a file in,
// and says how to reach it. Only the three that name a workspace, a project or a session take it.
func (s scope) locatable(advice string) scope {
	s.directories = advice
	return s
}

// nothing says an empty listing, which is the case the operator cannot read at all: "no jobs" from
// one project and "no jobs" from a system holding nine of them are the same three words.
func (s scope) nothing(out io.Writer) {
	if s.where == "" {
		fmt.Fprintf(out, "no %s in this system\n", s.what)
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
		fmt.Fprintf(out, "\n%s in this system\n", plural(rows, s.what))
		s.locations(out)
		return
	}
	fmt.Fprintf(out, "\n%s in %s", plural(rows, s.what), s.where)
	if s.wider != "" {
		fmt.Fprintf(out, ". %s", s.wider)
	}
	fmt.Fprintln(out)
	s.locations(out)
}

// locations is the line under a listing that says how to reach the directory a row is kept in.
func (s scope) locations(out io.Writer) {
	if s.directories == "" {
		return
	}
	fmt.Fprintf(out, "%s\n", s.directories)
}

// plural counts a noun: one job, two jobs.
func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, strings.TrimSuffix(noun, "s"))
	}
	return fmt.Sprintf("%d %s", count, noun)
}

// readsTheSystem reports whether the address typed is the word that means every workspace.
func readsTheSystem(typed string) bool { return strings.TrimSpace(typed) == systemScope }

// systemLevel is what a listing of what the system itself holds says it read. Skills, roles and hooks
// sit at a level rather than at an address, so the system is a place a listing can name.
const systemLevel = "the system"

// heldBy is the scope of a listing of what one level holds. The system's own is everything such a
// listing can reach, so it offers nothing wider.
func heldBy(what, where, wider string) scope {
	if where == systemLevel {
		return scope{what: what, where: systemLevel}
	}
	return narrowedTo(what, where, wider)
}
