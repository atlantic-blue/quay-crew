//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/contextsize"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// How big a level of context is, against a real database.
//
// The size is read off the body the listing hands back, so everything the tool and the console say
// about it rests on the whole body surviving the round trip. A column that truncated, or a driver
// that cut a long value, would leave every reading honest about a body that is not the one a session
// carries, and the crew would report a level as small while sessions read a large one.
//
// The number is the one the acceptance run of 29 August 2026 read out of this table with
// `select scope, owner, length(body) from contexts`, which is the reading this whole change exists
// to make unnecessary.
const crewOnTheDay = 100_179

func TestALongContextKeepsItsSizeThroughPostgres(t *testing.T) {
	kept := openPostgres(t)
	s := aCrewNamed(t, kept, "controller-a", 0, &model.FakeRunner{Reply: "ok"})
	ctx := context.Background()
	workspace, _ := aProjectOnPostgres(t, s)

	// Prose rather than one repeated letter: a driver or a column that mangles a long value tends to
	// do it at a boundary, and a body of identical bytes hides that.
	rules := strings.Repeat("# Working rules\nNever commit without asking. Always ship tests.\n", 1_400)
	rules += strings.Repeat("x", crewOnTheDay-len(rules))
	if len(rules) != crewOnTheDay {
		t.Fatalf("the body is %d characters, want %d", len(rules), crewOnTheDay)
	}

	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: string(store.ContextCrew), Body: rules,
	}); err != nil {
		t.Fatalf("SetContext: %v", err)
	}
	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: string(store.ContextWorkspace), Owner: workspace, Body: strings.Repeat("b", 1_886),
	}); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	listed, err := s.ListContexts(ctx, &quaycrewv1.ListContextsRequest{})
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	readings := map[string]contextsize.Reading{}
	for _, dir := range listed.GetDirs() {
		readings[dir.GetScope()] = contextsize.Read(dir.GetScope(), dir.GetName(), dir.GetBody())
	}

	crew, found := readings["crew"]
	if !found {
		t.Fatal("the listing has no crew level, so nothing can report its size")
	}
	if crew.Characters != crewOnTheDay {
		t.Errorf("the crew level comes back at %d characters, want %d: what a session reads and what "+
			"the crew reports are two different bodies", crew.Characters, crewOnTheDay)
	}
	if !crew.Over() {
		t.Error("the crew level holds a hundred thousand characters and the crew does not call it large")
	}
	if !strings.Contains(crew.Note(), "100,179") {
		t.Errorf("the crew level's note does not say its size: %q", crew.Note())
	}

	// The other level is under the mark, so the database round trip does not turn every level into a
	// warning: a crew that warns about all of them is a crew nobody reads the warnings of.
	if workspaceLevel := readings["workspace"]; workspaceLevel.Over() {
		t.Errorf("a level of %d characters is called large", workspaceLevel.Characters)
	}
}
