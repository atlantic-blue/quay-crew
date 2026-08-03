package features_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/features"
)

func TestTheCatalogCarriesEveryFeatureFile(t *testing.T) {
	catalog := features.All()
	if len(catalog) < 5 {
		t.Fatalf("the catalog holds %d features, want every feature file: %+v", len(catalog), catalog)
	}

	for _, feature := range catalog {
		if feature.Title == "" {
			t.Errorf("a feature has no title: %+v", feature)
		}
		if len(feature.Scenarios) == 0 {
			t.Errorf("%q lists no scenarios, so it claims a capability it does not hold up", feature.Title)
		}
		if feature.Summary == "" {
			t.Errorf("%q has no summary, so the list says what it is called and not what it is for", feature.Title)
		}
	}
}

// TestTheCatalogIsTheFilesRatherThanACopy: a hand written list would drift from the scenarios the
// moment somebody added one, and a list of capabilities that drifts is worse than none.
func TestTheCatalogIsTheFilesRatherThanACopy(t *testing.T) {
	var found bool
	for _, feature := range features.All() {
		if !strings.Contains(feature.Title, "sandbox keeps") {
			continue
		}
		found = true
		for _, scenario := range feature.Scenarios {
			if strings.Contains(scenario, "replaced sandbox belongs to the same project") {
				return
			}
		}
		t.Fatalf("the sandbox feature lists %v, missing the scenario written in its file", feature.Scenarios)
	}
	if !found {
		t.Fatal("the catalog does not carry the sandbox feature file")
	}
}

// TestTheSummaryStopsAtTheFirstParagraph keeps the notes an author leaves for the next author out of
// what an operator is shown.
func TestTheSummaryStopsAtTheFirstParagraph(t *testing.T) {
	for _, feature := range features.All() {
		if strings.Contains(feature.Summary, "Background") || strings.Contains(feature.Summary, "Scenario") {
			t.Errorf("%q swallowed the body of its file: %q", feature.Title, feature.Summary)
		}
	}
}
