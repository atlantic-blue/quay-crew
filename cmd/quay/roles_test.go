package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// aRoleDir writes a role's directory and returns the path, so a test can import the way an operator
// does rather than by building a request.
func aRoleDir(t *testing.T, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "role.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ROLE.md"),
		[]byte("Write the tests. Do not write the code."), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const testWriterManifest = `name: test-writer
version: 1
summary: writes the tests for a job, from the job alone
model: opus
receives:
  - job
  - context
`

func TestARoleIsImportedFromItsDirectoryAndSaysHowToAttachIt(t *testing.T) {
	client := testClient(t)
	dir := aRoleDir(t, "test-writer", testWriterManifest)

	printed := mustRun(t, client, "role", "import", dir)
	for _, want := range []string{"imported test-writer version 1", "opus", "context, job",
		"quay role attach test-writer"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the import does not say %q: %q", want, printed)
		}
	}
}

// The boundary is the part worth reading, so a listing says it rather than leaving an operator to
// open the role's own files.
func TestTheRoleListingSaysTheModelAndTheBoundary(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))

	printed := mustRun(t, client, "role", "list")
	for _, want := range []string{"test-writer", "v1", "runs on opus", "receives context, job"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the listing does not say %q: %q", want, printed)
		}
	}
}

func TestARoleIsAttachedToAWorkspaceAndTakenAwayAgain(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))

	if printed := mustRun(t, client, "role", "attach", "test-writer"); !strings.Contains(printed, "me holds the test-writer role") {
		t.Errorf("attaching said: %q", printed)
	}
	if printed := mustRun(t, client, "role", "list", "me"); !strings.Contains(printed, "test-writer") {
		t.Errorf("the workspace's listing does not hold it: %q", printed)
	}

	if printed := mustRun(t, client, "role", "detach", "test-writer"); !strings.Contains(printed, "no longer holds") {
		t.Errorf("detaching said: %q", printed)
	}
	if printed := mustRun(t, client, "role", "list", "me"); !strings.Contains(printed, "no roles in me") {
		t.Errorf("the workspace still holds it: %q", printed)
	}
}

// The word crew where a workspace goes, which is what quay skill attach and quay context set already
// take. A second word for the same level would be one to remember for no reason.
func TestARoleIsGivenToTheWholeCrew(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))

	printed := mustRun(t, client, "role", "attach", "crew", "test-writer")
	if !strings.Contains(printed, "the crew holds the test-writer role") {
		t.Errorf("attaching to the crew said: %q", printed)
	}
	listed := mustRun(t, client, "role", "list", "me")
	if !strings.Contains(listed, "held by the crew") {
		t.Errorf("the workspace's listing does not say where it came from: %q", listed)
	}
}

// A malformed role is refused before anything is sent, so the operator reads the sentence naming
// what is wrong rather than a transport error.
func TestAMalformedRoleIsRefusedBeforeItIsSent(t *testing.T) {
	client := testClient(t)
	dir := aRoleDir(t, "test-writer", `name: test-writer
version: 1
summary: writes the tests
model: opus
receives:
  - the whole repository
`)

	err := refused(t, client, "role", "import", dir)
	if !strings.Contains(err.Error(), "the whole repository") {
		t.Errorf("the refusal does not name what is wrong: %v", err)
	}
	// Nothing was sent, so the crew holds nothing.
	listed, listErr := client.ListRoles(t.Context(), &quaycrewv1.ListRolesRequest{})
	if listErr != nil {
		t.Fatalf("ListRoles: %v", listErr)
	}
	if len(listed.GetRoles()) != 0 {
		t.Errorf("the crew holds %d roles after a refused import", len(listed.GetRoles()))
	}
}

func TestTheRoleCommandNamesWhatItTakes(t *testing.T) {
	client := testClient(t)

	for _, args := range [][]string{
		{"role"},
		{"role", "nonsense"},
		{"role", "import"},
		{"role", "list", "one", "two"},
		{"role", "attach"},
		{"role", "show"},
		{"role", "show", "one", "two", "three"},
	} {
		err := refused(t, client, args...)
		if !strings.Contains(err.Error(), "usage: quay role") {
			t.Errorf("quay %s was refused without saying what to type: %v", strings.Join(args, " "), err)
		}
	}
}

// aBrief is a role's instruction with the shapes a brief actually has: paragraphs, a blank line, a
// list and trailing punctuation. A renderer that reflows or trims any of it prints a role the crew
// does not hold, which is the one thing this command must not do.
const aBrief = `You write the tests and nothing else.

Read the contract first. Then:

- the success case
- every error the contract names

Never write the implementation.`

// briefManifest names a role that may call things, so the boundary and the verbs both have
// something to say.
const briefManifest = `name: test-writer
version: 3
summary: writes the tests for a job, from the job alone
model: opus
receives:
  - job
  - context
may:
  - job.create
  - job.read
`

// aRoleDirWithBrief writes a role whose brief is given rather than the one line aRoleDir writes.
func aRoleDirWithBrief(t *testing.T, name, manifest, brief string) string {
	t.Helper()
	dir := aRoleDir(t, name, manifest)
	if err := os.WriteFile(filepath.Join(dir, "ROLE.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The point of the command. An operator auditing a run needs the clause that produced it, so the
// brief comes back as it went in rather than summarised, wrapped or cut.
func TestARoleIsReadBackWholeWithItsBrief(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "role", "import", aRoleDirWithBrief(t, "test-writer", briefManifest, aBrief))

	printed := mustRun(t, client, "role", "show", "test-writer")
	for _, want := range []string{
		"test-writer v3", "writes the tests for a job, from the job alone",
		"runs on opus", "receives context, job", "may call job.create, job.read",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("showing the role does not say %q: %q", want, printed)
		}
	}
	if !strings.Contains(printed, aBrief) {
		t.Errorf("the brief did not come back whole:\n%s", printed)
	}
}

// Empty is what a role that may call nothing declared, and it is the default. Printed only when
// something is granted, the line reads as missing rather than as a boundary, which is exactly the
// misreading a role exists to prevent.
func TestARoleThatMayCallNothingSaysSo(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))

	printed := mustRun(t, client, "role", "show", "test-writer")
	if !strings.Contains(printed, "may call nothing") {
		t.Errorf("it does not say the role may call nothing: %q", printed)
	}
}

// Who holds it, because a brief that reaches nobody and a brief every session runs under read the
// same otherwise, and that is the difference an audit came to find.
func TestShowingARoleSaysWhoHoldsIt(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))

	if printed := mustRun(t, client, "role", "show", "test-writer"); !strings.Contains(printed, "nothing holds it") {
		t.Errorf("a role nobody attached does not say so: %q", printed)
	}

	mustRun(t, client, "role", "attach", "test-writer")
	if printed := mustRun(t, client, "role", "show", "test-writer"); !strings.Contains(printed, "attached by acme") {
		t.Errorf("it does not name the workspace holding it: %q", printed)
	}

	mustRun(t, client, "role", "attach", "crew", "test-writer")
	if printed := mustRun(t, client, "role", "show", "test-writer"); !strings.Contains(printed, "held by the crew") {
		t.Errorf("it does not say the crew holds it: %q", printed)
	}
}

// A workspace pins the version it attached, so showing at that address has to read that version
// rather than the newest the crew has. Reading the wrong one is the whole failure this command
// exists to end: an operator diffing a brief against a run that was never given it.
func TestShowingARoleAtAWorkspaceReadsTheVersionItPinned(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "role", "import", aRoleDirWithBrief(t, "test-writer", testWriterManifest, "Version one says this."))
	mustRun(t, client, "role", "attach", "test-writer")
	mustRun(t, client, "role", "import", aRoleDirWithBrief(t, "test-writer", `name: test-writer
version: 2
summary: writes the tests for a job, from the job alone
model: opus
receives:
  - job
  - context
`, "Version two says something else."))

	held := mustRun(t, client, "role", "show", "acme", "test-writer")
	if !strings.Contains(held, "Version one says this.") {
		t.Errorf("the workspace's pinned version was not what came back: %q", held)
	}
	crew := mustRun(t, client, "role", "show", "test-writer")
	if !strings.Contains(crew, "Version two says something else.") {
		t.Errorf("the crew's newest version was not what came back: %q", crew)
	}
}

// A refusal that only says no leaves the operator guessing between a typo, a role they never
// imported and a workspace that never attached one.
func TestNamingARoleThatIsNotThereNamesTheOnesThatAre(t *testing.T) {
	client := testClient(t)

	empty := refused(t, client, "role", "show", "orchestrator")
	if !strings.Contains(empty.Error(), "holds no roles at all") {
		t.Errorf("a crew holding nothing did not say so: %v", empty)
	}

	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))
	mustRun(t, client, "role", "import", aRoleDir(t, "implementer", strings.Replace(
		testWriterManifest, "name: test-writer", "name: implementer", 1)))

	typo := refused(t, client, "role", "show", "test-writter")
	if !strings.Contains(typo.Error(), "test-writer") {
		t.Errorf("a near miss was not named: %v", typo)
	}
	if strings.Contains(typo.Error(), "implementer") {
		t.Errorf("a name nothing like the one typed was offered as near: %v", typo)
	}

	nothingLikeIt := refused(t, client, "role", "show", "orchestrator")
	for _, want := range []string{"test-writer", "implementer"} {
		if !strings.Contains(nothingLikeIt.Error(), want) {
			t.Errorf("with no near miss it does not name what is held (%s): %v", want, nothingLikeIt)
		}
	}
}

// A brief the crew has lost is not a brief that says nothing, and rendering the two the same way
// would let an audit read an empty page as a role with no instructions. Import refuses an empty
// brief, so this reaches the renderer directly, which is the only place the case can exist.
func TestARoleWithNoBriefStillPrintsWhatItIs(t *testing.T) {
	var out bytes.Buffer
	writeRole(&out, &quaycrewv1.GetRoleResponse{
		Role: &quaycrewv1.Role{
			Name: "test-writer", Version: 1, Summary: "writes the tests",
			Model: "opus", Receives: []string{"context", "job"},
		},
	})
	printed := out.String()
	for _, want := range []string{"test-writer v1", "runs on opus", "receives context, job", "may call nothing"} {
		if !strings.Contains(printed, want) {
			t.Errorf("a role with no brief does not say %q: %q", want, printed)
		}
	}
}
