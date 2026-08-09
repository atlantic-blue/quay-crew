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

// The way off the repository commands. They are still in somebody's fingers, their scripts and their
// notes, so every shape they were typed in has to fail loudly and name the new way: a repository is
// cloned in conversation now, following the git skill.
func TestQuayRepositoryIsGoneAndNamesTheGitSkill(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")

	for _, args := range [][]string{
		{"repository"},
		{"repository", "add", "https://github.com/atlantic-blue/quay-crew.git"},
		{"repository", "list"},
		{"repository", "remove", "quay-crew"},
		{"repository", "add", "acme", "https://github.com/atlantic-blue/quay-crew.git"},
	} {
		said, err := runQuay(t, client, args...)
		if err == nil {
			t.Errorf("quay %s was accepted, and said %q", strings.Join(args, " "), said)
			continue
		}
		for _, wants := range []string{"cloned in conversation", "git skill"} {
			if !strings.Contains(err.Error(), wants) {
				t.Errorf("quay %s is refused with %q, want it to say %q", strings.Join(args, " "), err, wants)
			}
		}
	}
}

// The commands the repository machinery itself replaced must not point at it now that it is gone: a
// refusal naming another removed command is a corridor of locked doors.
func TestTheCommandsThisReplacedSayWhereItWent(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")

	for _, one := range []struct {
		args []string
		says string
	}{
		{[]string{"project", "remote", "set", "https://github.com/a/b.git"}, "git skill"},
		{[]string{"project", "remote"}, "git skill"},
		// Absorbed silently, this would have made "--remote" the project's name.
		{[]string{"project", "create", "thing", "--remote", "https://github.com/a/b.git"}, "git skill"},
	} {
		said, err := runQuay(t, client, one.args...)
		if err == nil {
			t.Errorf("quay %s was accepted, and said %q", strings.Join(one.args, " "), said)
			continue
		}
		if !strings.Contains(err.Error(), one.says) {
			t.Errorf("quay %s is refused with %q, want it to say %q", strings.Join(one.args, " "), err, one.says)
		}
		if strings.Contains(err.Error(), "quay repository") {
			t.Errorf("quay %s is refused with %q, which points at a command that is also gone", strings.Join(one.args, " "), err)
		}
	}

	// And the old flag did not quietly create something called "--remote" either.
	listed := mustRun(t, client, "project", "list")
	if strings.Contains(listed, "--remote") || strings.Contains(listed, "thing") {
		t.Errorf("the refused command created a project anyway: %q", listed)
	}
}
