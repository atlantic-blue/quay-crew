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

// SkillsScope is the mark the index of a workspace's skills is written under.
//
// It is a section like a level of context and deliberately not one of them. A level is something
// somebody wrote and the crew keeps; this is rendered from the skills the workspace holds, so reading
// it back would store a derived thing as though a person had typed it.
//
// It still has to be named when a memory file is read back, because text under a mark the build does
// not know is swept into the innermost level rather than dropped. Named, it is recognised and left
// where it is.
const SkillsScope = "skills"

// RoleScope is the mark a session's role brief is written under.
//
// A section like the skills index and deliberately not a level of context: it is rendered from the
// role the session runs as, so reading it back would store the crew's own rendering as though a
// person had typed it, and the next task would render it again underneath itself.
const RoleScope = "role"

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

// Marked says whether a memory file was composed by the crew, which is what its marks are evidence
// of. A file with none of them was written by somebody else: it may be a note an agent left, or a
// CLAUDE.md that was there before any of this existed, but whoever wrote it had never seen what the
// store holds, so it cannot be read as an edit of it.
func Marked(body string) bool {
	return strings.Contains(body, sectionOpen)
}

// WithoutSection returns a body with one marked section removed: the mark line and everything under
// it, up to the next mark or the end. The second return says whether the section was there.
//
// Context should never hold a marked section, but a build that wrote the skills index into the
// session's own memory file left one to be swept into stored context by the read back of a later
// build. What was swept is rendered state, not something somebody typed, so where it is found it is
// dropped rather than kept.
func WithoutSection(body, scope string) (string, bool) {
	mark := sectionOpen + scope + sectionClose
	var out strings.Builder
	dropping, found := false, false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, sectionOpen) && strings.HasSuffix(trimmed, sectionClose) {
			dropping = trimmed == mark
			if dropping {
				found = true
				continue
			}
		}
		if dropping {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	if !found {
		return body, false
	}
	return strings.TrimSpace(out.String()), true
}

// Decompose reads a memory file back into what each level says.
//
// Anything before the first mark, or under a mark this build does not know, belongs to the last scope
// given: text appended to the end of the file is what an agent writing a note actually does, and the
// innermost level is where a note about this job belongs. Nothing is thrown away.
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
