//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
)

// Reading a level's context back out of the system, against the real database.
//
// The unit tier proves the command prints what it is given and refuses an empty level, against a
// store in memory. What only this tier reaches is the crossing the command exists for: the operator
// writes prose into a column as one process runs, and reads it back as another. During the
// acceptance run of 29 August the only way to recover a workspace's context was a query against this
// table, which is the whole reason `krewe context show` was written, so an answer that is not what the
// column holds is worse than no command.
//
// Byte for byte is the assertion, at every level and on a body larger than any page of prose. A
// truncation, an encoding, or a column that quietly keeps the first few thousand bytes shows here and
// nowhere else, and each one would hand somebody a level to edit that the system does not hold.

// aBodyLargerThanAPage is prose of about 70 kilobytes, which is past the point where a column of the
// wrong type starts dropping the end of it. A system's own context is already this size.
func aBodyLargerThanAPage() string {
	var b strings.Builder
	for b.Len() < 70_000 {
		b.WriteString("Never touch production data. Deploy through the pipeline, never from a shell.\n\n")
	}
	return b.String()
}

// TestEveryLevelComesBackByteForByteThroughTheListing writes a body at each of the three levels a
// system has and reads each one back, comparing against what was written rather than against the
// write's own answer, which could hand back what it was sent while the database held none of it.
func TestEveryLevelComesBackByteForByteThroughTheListing(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	// Prose, with the shapes a tidy up would eat: no trailing newline, a trailing space, a blank
	// line, a tab, and something outside the Latin alphabet.
	levels := []struct {
		scope, owner, body string
	}{
		{"system", "", "No acronyms.  \n\n\tSpell things out.\nОдно слово, одно значение."},
		{"workspace", workspace, aBodyLargerThanAPage()},
		{"project", project, "pay the water bill first"},
	}
	for _, level := range levels {
		if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
			Scope: level.scope, Owner: level.owner, Body: level.body,
		}); err != nil {
			t.Fatalf("SetContext at the %s level: %v", level.scope, err)
		}
	}

	read := 0
	for _, level := range levels {
		got := bodyOnPostgres(t, s, level.scope, level.owner)
		if got != level.body {
			t.Errorf("the %s level came back %d bytes and %d went in",
				level.scope, len(got), len(level.body))
			continue
		}
		// Held to the body first and to a length after, so a column that came back empty cannot
		// satisfy this by matching an equally empty expectation.
		if len(got) == 0 {
			t.Errorf("the %s level came back empty, and a level with nothing in it is unreadable",
				level.scope)
			continue
		}
		read++
	}
	if read != len(levels) {
		t.Fatalf("%d levels came back whole and a system has %d", read, len(levels))
	}
	t.Logf("read %d levels back off the database", read)
}

// What comes out goes back in unchanged. This is the pair the issue asked for, run against the real
// column: `krewe context show system > file` and `krewe context set system < file`.
func TestWhatIsReadBackIsWrittenBackUnchanged(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, _ := aProjectOnPostgres(t, s)
	body := "Never touch production data.  \n\nDeploy through the pipeline, never from a shell."

	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: "workspace", Owner: workspace, Body: body,
	}); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	first := bodyOnPostgres(t, s, "workspace", workspace)
	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: "workspace", Owner: workspace, Body: first,
	}); err != nil {
		t.Fatalf("setting back what was read: %v", err)
	}
	second := bodyOnPostgres(t, s, "workspace", workspace)

	if first != body {
		t.Fatalf("the level read back %q and %q went in", first, body)
	}
	if second != first {
		t.Fatalf("the round trip changed the level: %q became %q", first, second)
	}
}

// A level is added to rather than overwritten, which is what could not be done at all before the read
// existed. The operator reads the level out, appends, and writes it back.
func TestALevelIsAddedToRatherThanOverwritten(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: "system", Body: "Never touch production data.\n",
	}); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	held := bodyOnPostgres(t, s, "system", "")
	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: "system", Body: held + "\nDeploy through the pipeline.\n",
	}); err != nil {
		t.Fatalf("setting the level back with a paragraph added: %v", err)
	}

	got := bodyOnPostgres(t, s, "system", "")
	for _, want := range []string{"Never touch production data.", "Deploy through the pipeline."} {
		if !strings.Contains(got, want) {
			t.Errorf("the system level no longer says %q: %q", want, got)
		}
	}
}

// A project's context is held in the store, and the row describing it was dropped whenever the system
// could not name the directories on disk. A control plane told no data directory then reported every
// project as saying nothing, and `krewe context clear` read the same rows, so it announced a level as
// already empty while it held a body. This system has no data directory, which is the condition.
func TestAProjectReadsBackOnASystemWithNoDataDirectory(t *testing.T) {
	s, _ := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: "project", Owner: project, Body: "pay the water bill first",
	}); err != nil {
		t.Fatalf("SetContext: %v", err)
	}
	if got := bodyOnPostgres(t, s, "project", project); got != "pay the water bill first" {
		t.Fatalf("the project level read back %q, and the database holds a body", got)
	}
}

// bodyOnPostgres is what a level says, read the way the command reads it: out of the listing, off the
// real database.
func bodyOnPostgres(t *testing.T, s *controlplane.Server, scope, owner string) string {
	t.Helper()
	resp, err := s.ListContexts(context.Background(), &quaycrewv1.ListContextsRequest{})
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	for _, dir := range resp.GetDirs() {
		if dir.GetScope() == scope && dir.GetOwner() == owner {
			return dir.GetBody()
		}
	}
	t.Fatalf("the listing carries no %s level at all, so there is nothing to read back", scope)
	return ""
}
