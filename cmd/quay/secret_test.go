package main

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// aCrewWatchingItsSandboxes keeps the sandbox double, because the only honest way to check a secret
// was stored correctly is to look at what a session is actually given: nothing reads a value back,
// by design.
func aCrewWatchingItsSandboxes(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *sandbox.FakeProvider) {
	t.Helper()
	boxes := &sandbox.FakeProvider{}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: boxes, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client, boxes
}

// carried is the value a session was given for a name, and whether it was given one at all.
func carried(boxes *sandbox.FakeProvider, name string) (string, bool) {
	for _, made := range boxes.Created {
		for _, entry := range made.Env {
			if key, value, found := strings.Cut(entry, "="); found && key == name {
				return value, true
			}
		}
	}
	return "", false
}

// The point of the whole change: a credential that never appears in an argument still reaches the
// session, with exactly the value that was piped in.
func TestAPipedSecretReachesTheSessionUnchanged(t *testing.T) {
	client, boxes := aCrewWatchingItsSandboxes(t)

	saying(t, "ghp-piped-not-typed")
	if said := mustRun(t, client, "secret", "set", "GH_TOKEN"); !strings.Contains(said, "GH_TOKEN") {
		t.Fatalf("setting from a pipe did not confirm: %q", said)
	}
	if strings.Contains(mustRun(t, client, "secret", "list"), "ghp-piped") {
		t.Error("the listing printed the value")
	}

	mustRun(t, client, "dispatch", "hello")
	got, given := carried(boxes, "GH_TOKEN")
	if !given {
		t.Fatal("the session was not given GH_TOKEN at all")
	}
	if got != "ghp-piped-not-typed" {
		t.Fatalf("the session was given %q", got)
	}
}

// Every tool that prints a credential ends with a newline. `gh auth token` does, and a token
// carrying one authenticates nothing while looking exactly right in every listing.
func TestATrailingNewlineIsNotPartOfTheSecret(t *testing.T) {
	client, boxes := aCrewWatchingItsSandboxes(t)

	saying(t, "ghp-with-a-newline\n")
	mustRun(t, client, "secret", "set", "GH_TOKEN")
	mustRun(t, client, "dispatch", "hello")

	if got, _ := carried(boxes, "GH_TOKEN"); got != "ghp-with-a-newline" {
		t.Fatalf("the session was given %q, so the newline travelled with it", got)
	}
}

// With a pipe, both arguments are an address and a name. Without one, the last argument is still the
// value, so every script that already exists keeps working.
func TestTheWorkspaceCanBeNamedWithThePipedForm(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)
	// A second workspace, and we stay standing in the first, so naming one has to be what decides
	// where the secret lands. Without this the test cannot tell the argument from the fallback.
	mustRun(t, client, "workspace", "create", "elsewhere")
	mustRun(t, client, "use", "me/house-bills")

	saying(t, "piped-into-a-named-workspace")
	mustRun(t, client, "secret", "set", "elsewhere", "GH_TOKEN")

	elsewhere := mustRun(t, client, "secret", "list", "elsewhere")
	if !strings.Contains(elsewhere, "GH_TOKEN") {
		t.Fatalf("the named workspace did not get the secret: %q", elsewhere)
	}
	if here := mustRun(t, client, "secret", "list", "me"); strings.Contains(here, "GH_TOKEN") {
		t.Fatalf("the secret landed where we were standing rather than where we said: %q", here)
	}
}

func TestAValueAsAnArgumentStillWorks(t *testing.T) {
	client, boxes := aCrewWatchingItsSandboxes(t)

	mustRun(t, client, "secret", "set", "GH_TOKEN", "ghp-typed")
	mustRun(t, client, "secret", "set", "me", "OTHER", "also-typed")
	mustRun(t, client, "dispatch", "hello")

	if got, _ := carried(boxes, "GH_TOKEN"); got != "ghp-typed" {
		t.Errorf("the two argument form broke: %q", got)
	}
	if got, _ := carried(boxes, "OTHER"); got != "also-typed" {
		t.Errorf("the three argument form broke: %q", got)
	}
}

// A name with no value and nothing coming in is somebody who meant to pipe, so the refusal shows
// them how rather than repeating the usage.
func TestANameWithNoValueSaysHowToPipeItIn(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	err := refused(t, client, "secret", "set", "GH_TOKEN")
	for _, want := range []string{"GH_TOKEN", "nothing is being piped in", "quay secret set GH_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// An empty pipe is a file that turned out to have nothing in it, and storing an empty credential
// leaves a session failing to authenticate with a secret the listing says is set.
func TestAnEmptyPipeIsRefusedRatherThanStored(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	saying(t, "   \n")
	if err := refused(t, client, "secret", "set", "GH_TOKEN"); !strings.Contains(err.Error(), "was not set") {
		t.Errorf("an empty pipe was stored: %s", err)
	}
	if listed := mustRun(t, client, "secret", "list"); !strings.Contains(listed, "no secrets set") {
		t.Errorf("an empty value was stored anyway: %q", listed)
	}
}

func TestTheUsageOffersThePipedForm(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	printed := mustRun(t, client, "help")
	if !strings.Contains(printed, "standard input") {
		t.Errorf("the usage does not offer the piped form: %q", printed)
	}
}
