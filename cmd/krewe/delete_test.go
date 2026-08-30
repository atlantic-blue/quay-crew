package main

import (
	"strings"
	"testing"
)

// A system only ever grew: a workspace made by a typo was there for good, a secret could be
// overwritten but never removed, and starting again meant going around the tool entirely, into
// Docker and the data directory.
func TestAWorkspaceCanBeRemovedWithEverythingUnderIt(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "typo")
	mustRun(t, client, "project", "create", "notes")
	mustRun(t, client, "task", "hello")
	mustRun(t, client, "secret", "set", "typo", "GH_TOKEN", "ghp-1234")

	saying(t, "typo\n")
	removed := mustRun(t, client, "workspace", "delete", "typo")
	for _, want := range []string{"1 project", "1 session", "1 secret", "deleted workspace typo"} {
		if !strings.Contains(removed, want) {
			t.Errorf("the removal does not account for %q: %q", want, removed)
		}
	}

	if listed := mustRun(t, client, "workspace", "list"); strings.Contains(listed, "typo") {
		t.Fatalf("the workspace is still there: %q", listed)
	}
	// What it held goes with it, which is the half a listing of workspaces cannot show.
	if listed := mustRun(t, client, "secret", "list"); strings.Contains(listed, "GH_TOKEN") {
		t.Errorf("its secrets outlived it: %q", listed)
	}
}

// Deleting what you are standing in leaves the tool pointing at something gone, which every command
// then refuses about. It steps back out rather than leaving the operator there without saying so.
func TestDeletingWhereYouAreStandingMovesYouOut(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "typo")
	mustRun(t, client, "project", "create", "notes")

	saying(t, "typo\n")
	removed := mustRun(t, client, "workspace", "delete", "typo")
	if !strings.Contains(removed, "you are now nowhere") {
		t.Fatalf("the removal did not say it moved you: %q", removed)
	}
	here, err := currentPath()
	if err != nil {
		t.Fatal(err)
	}
	if !here.IsZero() {
		t.Fatalf("still standing in %s", here)
	}
}

func TestAProjectCanBeRemovedAndYouStepBackToItsWorkspace(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "notes")
	mustRun(t, client, "task", "hello")

	saying(t, "notes\n")
	removed := mustRun(t, client, "project", "delete", "me/notes")
	if !strings.Contains(removed, "deleted project notes") || !strings.Contains(removed, "1 session") {
		t.Fatalf("the removal did not say what went: %q", removed)
	}
	if listed := mustRun(t, client, "project", "list", "me"); strings.Contains(listed, "notes") {
		t.Fatalf("the project is still there: %q", listed)
	}
	if here, _ := currentPath(); here.Project != "" || here.Workspace != "me" {
		t.Errorf("you were left standing in %s", here)
	}
}

// The only guard this tool can offer, since it takes no flags and there is no --yes to require.
// Conversations do not come back.
func TestTheWrongNameRemovesNothing(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "keep-me")

	saying(t, "something-else\n")
	err := refused(t, client, "workspace", "delete", "keep-me")
	if !strings.Contains(err.Error(), "nothing was removed") {
		t.Errorf("the refusal is unclear: %s", err)
	}
	if listed := mustRun(t, client, "workspace", "list"); !strings.Contains(listed, "keep-me") {
		t.Fatalf("it was removed anyway: %q", listed)
	}
}

func TestAnEmptyConfirmationRemovesNothing(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "keep-me")

	saying(t, "\n")
	if err := refused(t, client, "workspace", "delete", "keep-me"); err == nil {
		t.Fatal("an empty confirmation was accepted")
	}
	if listed := mustRun(t, client, "workspace", "list"); !strings.Contains(listed, "keep-me") {
		t.Fatalf("it was removed anyway: %q", listed)
	}
}

// A workspace address handed to the project command would otherwise resolve to the workspace and
// take its first project, or nothing, neither of which is what was asked for.
func TestDeletingAProjectRefusesAWorkspaceAddress(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	saying(t, "me\n")
	err := refused(t, client, "project", "delete", "me")
	if !strings.Contains(err.Error(), "krewe workspace delete") {
		t.Errorf("the refusal does not name the command that does it: %s", err)
	}
}

func TestTheUsageNamesBothRemovals(t *testing.T) {
	client := testClient(t)

	printed := mustRun(t, client, "help")
	for _, want := range []string{"workspace delete", "project delete"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the usage does not name %q", want)
		}
	}
}
