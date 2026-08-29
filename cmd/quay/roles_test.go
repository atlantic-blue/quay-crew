package main

import (
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
	} {
		err := refused(t, client, args...)
		if !strings.Contains(err.Error(), "usage: quay role") {
			t.Errorf("quay %s was refused without saying what to type: %v", strings.Join(args, " "), err)
		}
	}
}
