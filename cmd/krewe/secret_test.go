package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// aSystemWatchingItsSandboxes keeps the sandbox double, because the only honest way to check a secret
// was stored correctly is to look at what a session is actually given: nothing reads a value back,
// by design.
func aSystemWatchingItsSandboxes(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *sandbox.FakeProvider) {
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
	client, boxes := aSystemWatchingItsSandboxes(t)

	saying(t, "ghp-piped-not-typed")
	if said := mustRun(t, client, "secret", "set", "GH_TOKEN"); !strings.Contains(said, "GH_TOKEN") {
		t.Fatalf("setting from a pipe did not confirm: %q", said)
	}
	if strings.Contains(mustRun(t, client, "secret", "list"), "ghp-piped") {
		t.Error("the listing printed the value")
	}

	mustRun(t, client, "task", "hello")
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
	client, boxes := aSystemWatchingItsSandboxes(t)

	saying(t, "ghp-with-a-newline\n")
	mustRun(t, client, "secret", "set", "GH_TOKEN")
	mustRun(t, client, "task", "hello")

	if got, _ := carried(boxes, "GH_TOKEN"); got != "ghp-with-a-newline" {
		t.Fatalf("the session was given %q, so the newline travelled with it", got)
	}
}

// With a pipe, both arguments are an address and a name. Without one, the last argument is still the
// value, so every script that already exists keeps working.
func TestTheWorkspaceCanBeNamedWithThePipedForm(t *testing.T) {
	client, _ := aSystemWatchingItsSandboxes(t)
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
	client, boxes := aSystemWatchingItsSandboxes(t)

	mustRun(t, client, "secret", "set", "GH_TOKEN", "ghp-typed")
	mustRun(t, client, "secret", "set", "me", "OTHER", "also-typed")
	mustRun(t, client, "task", "hello")

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
	client, _ := aSystemWatchingItsSandboxes(t)

	err := refused(t, client, "secret", "set", "GH_TOKEN")
	for _, want := range []string{"GH_TOKEN", "nothing is being piped in", "krewe secret set GH_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// An empty pipe is a file that turned out to have nothing in it, and storing an empty credential
// leaves a session failing to authenticate with a secret the listing says is set.
func TestAnEmptyPipeIsRefusedRatherThanStored(t *testing.T) {
	client, _ := aSystemWatchingItsSandboxes(t)

	saying(t, "   \n")
	if err := refused(t, client, "secret", "set", "GH_TOKEN"); !strings.Contains(err.Error(), "was not set") {
		t.Errorf("an empty pipe was stored: %s", err)
	}
	if listed := mustRun(t, client, "secret", "list"); !strings.Contains(listed, "no secrets in this system") {
		t.Errorf("an empty value was stored anyway: %q", listed)
	}
}

func TestTheUsageOffersThePipedForm(t *testing.T) {
	client, _ := aSystemWatchingItsSandboxes(t)

	printed := mustRun(t, client, "help")
	if !strings.Contains(printed, "standard input") {
		t.Errorf("the usage does not offer the piped form: %q", printed)
	}
}

// The command every workspace stops repeating. "system" where a workspace goes, the same word a skill,
// a hook and a piece of context already take.
func TestASecretSetOnTheSystemReachesAWorkspaceMadeAfterwards(t *testing.T) {
	boxes := &sandbox.FakeProvider{}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: boxes, Secrets: secrets.NewMemory(),
	})

	saying(t, "ghp-shared")
	said := mustRun(t, client, "secret", "set", "system", "GH_TOKEN")
	if !strings.Contains(said, "every workspace") {
		t.Fatalf("setting on the system did not say who gets it: %q", said)
	}

	// Made after the secret was set, which is the case that used to cost a round of setting up.
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "task", "hello")

	got, given := carried(boxes, "GH_TOKEN")
	if !given {
		t.Fatal("the session was not given GH_TOKEN, and the system holds it")
	}
	if got != "ghp-shared" {
		t.Fatalf("the session was given %q, want ghp-shared", got)
	}
}

// A listing that showed the system's secrets under a workspace name would say a workspace set
// something it never set, and one that hid them would say a token is missing that is already there.
func TestAListingSaysWhichSecretsTheSystemHolds(t *testing.T) {
	client, _ := aSystemWatchingItsSandboxes(t)

	saying(t, "ghp-shared")
	mustRun(t, client, "secret", "set", "system", "GH_TOKEN")
	saying(t, "sk-mine")
	mustRun(t, client, "secret", "set", "STRIPE_KEY")

	listed := mustRun(t, client, "secret", "list")
	if !strings.Contains(listed, "system") {
		t.Fatalf("the listing does not say the system holds one: %q", listed)
	}
	for _, want := range []string{"GH_TOKEN", "STRIPE_KEY"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("the listing does not name %s: %q", want, listed)
		}
	}

	// Narrowed to the system, only the system's.
	only := mustRun(t, client, "secret", "list", "system")
	if !strings.Contains(only, "GH_TOKEN") {
		t.Fatalf("krewe secret list system does not name the system's own: %q", only)
	}
	if strings.Contains(only, "STRIPE_KEY") {
		t.Fatalf("krewe secret list system named a workspace's own: %q", only)
	}
}

// A system secret belongs to no workspace and survives every one of them, so a removal that counted it
// would say a shared token is about to be lost with the workspace.
func TestRemovingAWorkspaceDoesNotClaimTheSystemsSecrets(t *testing.T) {
	client, _ := aSystemWatchingItsSandboxes(t)

	saying(t, "ghp-shared")
	mustRun(t, client, "secret", "set", "system", "GH_TOKEN")

	name, holds, err := whatAWorkspaceHolds(context.Background(), client, workspaceIDOf(t, client, "me"))
	if err != nil {
		t.Fatalf("whatAWorkspaceHolds: %v", err)
	}
	if name != "me" {
		t.Fatalf("it names %q, want me", name)
	}
	if !strings.Contains(holds, "0 secrets") {
		t.Fatalf("removing the workspace says it holds %q, and the system's secret is not its to lose", holds)
	}
}

// workspaceIDOf is the identifier behind a name, which the removal call needs and a listing has.
func workspaceIDOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, want string) string {
	t.Helper()
	resp, err := client.ListWorkspaces(context.Background(), &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, workspace := range resp.GetWorkspaces() {
		if workspace.GetName() == want {
			return workspace.GetId()
		}
	}
	t.Fatalf("there is no workspace called %s", want)
	return ""
}
