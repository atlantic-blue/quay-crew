package main

import (
	"strings"
	"testing"
)

// Where a project ships is a record rather than a person's memory, so it is read and written the way
// a ceiling is: the address, and the three values when you are setting them.

func TestTargetSaysNothingIsDeclaredAndHowToSayIt(t *testing.T) {
	client := aSystemToJobIn(t)

	said := mustRun(t, client, "target")

	if !strings.Contains(said, "me/house-bills") {
		t.Errorf("quay target does not say which project it read: %q", said)
	}
	if !strings.Contains(said, "nowhere") {
		t.Errorf("quay target does not say a project that has not said deploys nowhere: %q", said)
	}
	// A record nobody knows how to write is a record nobody writes.
	if !strings.Contains(said, "--account") {
		t.Errorf("quay target does not say how to declare one: %q", said)
	}
}

func TestTargetIsDeclaredAndReadBack(t *testing.T) {
	client := aSystemToJobIn(t)

	said := mustRun(t, client, "target", "me/house-bills",
		"--account", "123456789012", "--region", "eu-west-2",
		"--identity", "arn:aws:iam::123456789012:role/quay-deploy")

	for _, want := range []string{"123456789012", "eu-west-2", "arn:aws:iam::123456789012:role/quay-deploy"} {
		if !strings.Contains(said, want) {
			t.Errorf("declaring a target does not say %q back: %q", want, said)
		}
	}

	read := mustRun(t, client, "target", "me/house-bills")
	if !strings.Contains(read, "123456789012") || !strings.Contains(read, "eu-west-2") {
		t.Fatalf("the target did not survive being declared: %q", read)
	}
}

// The row the whole record exists for.
func TestProjectListSaysWhereEachProjectDeploys(t *testing.T) {
	client := aSystemToJobIn(t)
	mustRun(t, client, "target", "me/house-bills",
		"--account", "123456789012", "--region", "eu-west-2",
		"--identity", "arn:aws:iam::123456789012:role/quay-deploy")

	listed := mustRun(t, client, "project", "list")

	if !strings.Contains(listed, "123456789012/eu-west-2") {
		t.Fatalf("quay project list does not say where a project deploys: %q", listed)
	}
}

// Half a target is refused where it is typed, so the operator fixes it before anything is written.
func TestTargetRefusesHalfOfOne(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runQuay(t, client, "target", "me/house-bills", "--account", "123456789012")

	if err == nil {
		t.Fatal("half a target was accepted")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Fatalf("the refusal does not name what is missing: %v", err)
	}
}

func TestTargetRefusesAnIdentityFromAnotherAccount(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runQuay(t, client, "target", "me/house-bills",
		"--account", "123456789012", "--region", "eu-west-2",
		"--identity", "arn:aws:iam::999999999999:role/quay-deploy")

	if err == nil {
		t.Fatal("an identity from another account was accepted")
	}
	if !strings.Contains(err.Error(), "999999999999") {
		t.Fatalf("the refusal does not name the account the role is in: %v", err)
	}
}

// A workspace is not a project, and a target belongs to a body of work.
func TestTargetOnAWorkspaceSaysWhichProject(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runQuay(t, client, "target", "me", "--account", "123456789012",
		"--region", "eu-west-2", "--identity", "arn:aws:iam::123456789012:role/quay-deploy")

	if err == nil {
		t.Fatal("a target was declared against a workspace")
	}
	if !strings.Contains(err.Error(), "a deploy target belongs to a project") {
		t.Fatalf("the refusal does not say a target belongs to a project: %v", err)
	}
}

func TestTargetIsCleared(t *testing.T) {
	client := aSystemToJobIn(t)
	mustRun(t, client, "target", "me/house-bills",
		"--account", "123456789012", "--region", "eu-west-2",
		"--identity", "arn:aws:iam::123456789012:role/quay-deploy")

	said := mustRun(t, client, "target", "me/house-bills", "--clear")

	if !strings.Contains(said, "nowhere") {
		t.Fatalf("clearing did not leave the project deploying nowhere: %q", said)
	}
}

// A command nobody can find is a record nobody writes, and the usage is where somebody looks.
func TestTheUsageNamesTheTargetCommand(t *testing.T) {
	client := testClient(t)

	printed, err := asked(t, client, "help")
	if err != nil {
		t.Fatalf("help was refused: %v", err)
	}
	for _, want := range []string{"target [<address>]", "--account", "--region", "--identity"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the usage does not mention %q", want)
		}
	}
}
