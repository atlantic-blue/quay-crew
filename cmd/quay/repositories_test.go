package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// run one invocation and hand back whatever it said, error or not, which is what a test about a refusal
// needs.
func runQuay(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, args, &out, "")
	return out.String(), err
}

func TestAWorkspaceWorksInRepositories(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")

	if said := mustRun(t, client, "repository", "list"); !strings.Contains(said, "no repositories") {
		t.Errorf("a new workspace says %q, want it to say it works in none", said)
	}

	added := mustRun(t, client, "repository", "add", "https://github.com/atlantic-blue/quay-crew.git")
	if !strings.Contains(added, "quay-crew") {
		t.Errorf("adding said %q, want the directory the checkout lands in", added)
	}

	listed := mustRun(t, client, "repository", "list")
	if !strings.Contains(listed, "https://github.com/atlantic-blue/quay-crew.git") {
		t.Errorf("the listing says %q", listed)
	}

	mustRun(t, client, "repository", "remove", "quay-crew")
	if said := mustRun(t, client, "repository", "list"); !strings.Contains(said, "no repositories") {
		t.Errorf("after removing it says %q", said)
	}
}

// The way off the commands this replaced. Every test for the new form passes while the old form does
// something quietly wrong, and that is the shape most "how did this regress" moments actually have.
func TestTheCommandsThisReplacedSayWhereItWent(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")

	for _, one := range []struct {
		args []string
		says string
	}{
		{[]string{"project", "remote", "set", "https://github.com/a/b.git"}, "quay repository"},
		{[]string{"project", "remote"}, "quay repository"},
		// Absorbed silently, this would have made "--remote" the project's name.
		{[]string{"project", "create", "thing", "--remote", "https://github.com/a/b.git"}, "quay repository add"},
	} {
		said, err := runQuay(t, client, one.args...)
		if err == nil {
			t.Errorf("quay %s was accepted, and said %q", strings.Join(one.args, " "), said)
			continue
		}
		if !strings.Contains(err.Error(), one.says) {
			t.Errorf("quay %s is refused with %q, want it to say %q", strings.Join(one.args, " "), err, one.says)
		}
	}

	// And the old flag did not quietly create something called "--remote" either.
	listed := mustRun(t, client, "project", "list")
	if strings.Contains(listed, "--remote") || strings.Contains(listed, "thing") {
		t.Errorf("the refused command created a project anyway: %q", listed)
	}
}
