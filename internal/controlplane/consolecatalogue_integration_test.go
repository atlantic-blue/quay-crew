//go:build integration

package controlplane_test

import (
	"context"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/console"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
)

// The console's two listings of what the system holds, over a real Postgres, the real control plane
// and the real gRPC interface.
//
// The table tests in internal/console build these rows from a double, which answers whatever the case
// wrote. This says what the system actually answers: which of them reach every workspace, and which
// fields come back filled at all. A row claiming something the real call never carries is the failure
// this exists to catch.
func TestTheConsoleListsWhatTheSystemActuallyHolds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	server := controlplane.NewServer(controlplane.Config{
		Store: aRealStore(t, ctx), Runner: &model.FakeRunner{},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := servedOver(t, server)

	importSkillNamed(t, ctx, client, "github")
	importHookNamed(t, ctx, client, "merge-gate")
	// One of the two is given to every workspace, and the other waits for an attachment. That
	// difference is the one thing these rows say that a name and a version cannot.
	if _, err := client.AttachSkill(ctx, &quaycrewv1.AttachSkillRequest{Scope: "system", Name: "github"}); err != nil {
		t.Fatalf("AttachSkill: %v", err)
	}

	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("building the console: %v", err)
	}

	for _, view := range []struct {
		name string
		held string
	}{
		{"skills", "github"},
		{"hooks", "merge-gate"},
	} {
		t.Run(view.name, func(t *testing.T) {
			resource, found := registry.Get(view.name)
			if !found {
				t.Fatalf("the console has no %s view", view.name)
			}
			rows, err := resource.List(ctx, "")
			if err != nil {
				t.Fatalf("listing %s: %v", view.name, err)
			}
			row, ok := rowFor(rows, view.held)
			if !ok {
				t.Fatalf("the %s view does not list %q: %v", view.name, view.held, rows)
			}
			// Every column has a cell. A row one short draws each cell one place to the left of the
			// heading that names it.
			if len(row.Cells) != len(resource.Columns) {
				t.Fatalf("a %s row has %d cells and the view has %d columns", view.name, len(row.Cells), len(resource.Columns))
			}
			// Nothing on the row is a hole. A cell the system never fills reads as a listing that
			// failed rather than as a fact nobody holds.
			for at, column := range resource.Columns {
				if row.Cells[at] == "" {
					t.Fatalf("the %s column of %q is empty over the real system: %q", column.Title, view.held, row.Cells)
				}
			}
			if got := row.Cells[cellAt(t, resource, "version")]; got != "v1" {
				t.Fatalf("%q reads version %q, want the version it was imported at", view.held, got)
			}
		})
	}

	// The reach cell, which is the only thing here the control plane decides rather than the import:
	// the skill was attached to the system, and the hook was not.
	skills, _ := registry.Get("skills")
	skillRows, err := skills.List(ctx, "")
	if err != nil {
		t.Fatalf("listing skills: %v", err)
	}
	github, _ := rowFor(skillRows, "github")
	if got := github.Cells[cellAt(t, skills, "reaches")]; got != "everywhere" {
		t.Fatalf("a skill the system holds reads %q, want everywhere", got)
	}
	hooks, _ := registry.Get("hooks")
	hookRows, err := hooks.List(ctx, "")
	if err != nil {
		t.Fatalf("listing hooks: %v", err)
	}
	gate, _ := rowFor(hookRows, "merge-gate")
	if got := gate.Cells[cellAt(t, hooks, "reaches")]; got != "on attach" {
		t.Fatalf("a hook nobody attached reads %q, want on attach", got)
	}

	// The reason a skill is held and not given is not on this listing, and the console says nothing
	// about it for that reason. The control plane fills it for a workspace and for a session, and
	// leaves it empty here, so a cell reading it would be empty on every row.
	answered, err := client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	for _, one := range answered.GetSkills() {
		if one.GetLeftOut() != "" {
			t.Fatalf("the system's own skills listing says %q is left out, so the console could say why after all",
				one.GetName())
		}
	}
	hooksAnswered, err := client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
	if err != nil {
		t.Fatalf("ListHooks: %v", err)
	}
	for _, one := range hooksAnswered.GetHooks() {
		if one.GetLeftOut() != "" {
			t.Fatalf("the system's own hooks listing says %q is left out, so the console could say why after all",
				one.GetName())
		}
	}
}

func importSkillNamed(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, name string) {
	t.Helper()
	manifest := "name: " + name + "\nversion: 1\nsummary: Open pull requests and issues.\nbinaries: [git]\n"
	if _, err := client.ImportSkill(ctx, &quaycrewv1.ImportSkillRequest{Files: []*quaycrewv1.SkillFile{
		{Path: skill.ManifestFile, Body: []byte(manifest)},
		{Path: skill.BriefFile, Body: []byte("Open one with the gh tool.\n")},
	}}); err != nil {
		t.Fatalf("ImportSkill: %v", err)
	}
}

func importHookNamed(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, name string) {
	t.Helper()
	manifest := "name: " + name + "\nversion: 1\nsummary: Refuses a merge nobody asked for.\nevents:\n  - on: PreToolUse\n    matcher: Bash\n    entry: bin/hook\n"
	if _, err := client.ImportHook(ctx, &quaycrewv1.ImportHookRequest{Files: []*quaycrewv1.HookFile{
		{Path: "hook.yaml", Body: []byte(manifest)},
		{Path: "bin/hook", Body: []byte("#!/bin/sh\nexit 0\n"), Executable: true},
	}}); err != nil {
		t.Fatalf("ImportHook: %v", err)
	}
}
