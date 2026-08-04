package sandbox

import (
	"fmt"
	"strings"
)

// A sandbox's memory files are composed: the model reads two files and the crew has four levels to
// say something at, so each file carries the levels that belong to it, in order, marked.
//
// The marks are how an edit is read back. Something inside the sandbox writing into its own memory has
// learned something, and the crew has to be able to tell which level it belongs to rather than
// throwing the lot into one. They are HTML comments, which is what markdown has for a note the reader
// does not need.
const (
	sectionOpen  = "<!-- quay:"
	sectionClose = " -->"
)

// Section is one level's contribution to a memory file.
type Section struct {
	// Scope names the level, for example "crew".
	Scope string
	Body  string
}

// Compose renders sections into one memory file. Empty sections are left out entirely: a heading with
// nothing under it is noise the model has to read.
func Compose(sections []Section) string {
	var out strings.Builder
	for _, section := range sections {
		if strings.TrimSpace(section.Body) == "" {
			continue
		}
		fmt.Fprintf(&out, "%s%s%s\n%s\n\n", sectionOpen, section.Scope, sectionClose,
			strings.TrimRight(section.Body, "\n"))
	}
	return strings.TrimRight(out.String(), "\n")
}

// Decompose reads a memory file back into what each level says.
//
// Anything before the first mark, or under a mark this build does not know, belongs to the last scope
// given: text appended to the end of the file is what an agent writing a note actually does, and the
// innermost level is where a note about this piece of work belongs. Nothing is thrown away.
func Decompose(body string, scopes []string) map[string]string {
	found := make(map[string]string, len(scopes))
	if len(scopes) == 0 {
		return found
	}
	known := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		known[scope] = true
	}

	current := scopes[len(scopes)-1]
	var section strings.Builder
	flush := func() {
		if text := strings.TrimSpace(section.String()); text != "" {
			if existing := found[current]; existing != "" {
				found[current] = existing + "\n" + text
			} else {
				found[current] = text
			}
		}
		section.Reset()
	}

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, sectionOpen) && strings.HasSuffix(trimmed, sectionClose) {
			scope := strings.TrimSuffix(strings.TrimPrefix(trimmed, sectionOpen), sectionClose)
			if known[scope] {
				flush()
				current = scope
				continue
			}
		}
		section.WriteString(line)
		section.WriteString("\n")
	}
	flush()
	return found
}
