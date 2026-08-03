// Package features holds the executable specification, and hands it out at runtime.
//
// The feature files next to this one are the honest answer to "what does this thing do", because a
// scenario that is wrong fails the build. They are embedded so the answer travels in the binary: an
// operator asking what the product does is usually nowhere near a checkout of it.
package features

import (
	"embed"
	"sort"
	"strings"
)

//go:embed *.feature
var files embed.FS

// Feature is one capability and the scenarios that hold it up.
type Feature struct {
	// Title is the line after "Feature:".
	Title string
	// Summary is the prose under it, which says why the capability exists.
	Summary string
	// Scenarios are the behaviours proved, in the order they are written.
	Scenarios []string
}

// All returns every feature, ordered by title so the list does not shuffle between runs.
func All() []Feature {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}

	catalog := make([]Feature, 0, len(entries))
	for _, entry := range entries {
		contents, err := files.ReadFile(entry.Name())
		if err != nil {
			continue
		}
		if parsed, found := parse(string(contents)); found {
			catalog = append(catalog, parsed)
		}
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Title < catalog[j].Title })
	return catalog
}

// parse reads one feature file. It reads Gherkin's shape rather than its grammar: the title, the
// prose under it, and the scenario names. A comment line above a scenario is the author explaining
// themselves to a reader of the file, not part of the specification, so it is left out.
func parse(contents string) (Feature, bool) {
	var feature Feature
	var summary []string
	inSummary := false

	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Feature:"):
			feature.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "Feature:"))
			inSummary = true
		case strings.HasPrefix(trimmed, "Scenario:"):
			feature.Scenarios = append(feature.Scenarios, strings.TrimSpace(strings.TrimPrefix(trimmed, "Scenario:")))
			inSummary = false
		case strings.HasPrefix(trimmed, "Background:"), strings.HasPrefix(trimmed, "#"):
			inSummary = false
		case inSummary && trimmed != "":
			summary = append(summary, trimmed)
		case trimmed == "" && len(summary) > 0:
			// The summary is the first paragraph. Anything after the blank line that follows it is
			// the author talking to whoever edits the file next.
			inSummary = false
		}
	}
	feature.Summary = strings.Join(summary, " ")
	return feature, feature.Title != ""
}
