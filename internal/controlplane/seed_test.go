package controlplane_test

import (
	"context"
	"log/slog"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/model"
)

// What a fresh system puts in front of every session, rather than what it merely offers.
//
// A skill in the catalogue and attached to nothing reaches the jobs whose operator already knew to go
// and get it, which are not the jobs that need it. The rule about calling something outside this
// process is one nobody knows they need until after they have written the call, so it is taken at the
// system level and every workspace holds it without anybody deciding to.
func TestAFreshSystemPutsEverySessionUnderTheOutboundRule(t *testing.T) {
	s := newServer(&model.FakeRunner{Reply: "done"})
	ctx := context.Background()

	s.Seed(ctx, "../../skills", slog.New(slog.DiscardHandler))

	created, err := s.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	listed, err := s.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Workspace: created.GetWorkspace().GetId()})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}

	var held *quaycrewv1.Skill
	for _, one := range listed.GetSkills() {
		if one.GetName() == "outbound" {
			held = one
		}
	}
	if held == nil {
		t.Fatalf("a workspace on a fresh system holds %v, and the outbound skill is not among them", names(listed))
	}
	if !held.GetSystem() {
		t.Error("the workspace holds the outbound skill as its own attachment, so a workspace made later would not")
	}
}

// Seeding a skill to the system is a decision about which skills are not specific to one kind of
// work. It is worth pinning that the narrow ones stay in the catalogue, or the line in every
// conversation grows one skill at a time and the index becomes the thing nobody reads.
func TestAFreshSystemStillOnlyOffersTheSkillsForOneKindOfWork(t *testing.T) {
	s := newServer(&model.FakeRunner{Reply: "done"})
	ctx := context.Background()

	s.Seed(ctx, "../../skills", slog.New(slog.DiscardHandler))

	created, err := s.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	listed, err := s.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Workspace: created.GetWorkspace().GetId()})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	for _, one := range listed.GetSkills() {
		for _, narrow := range []string{"aws", "terraform", "jira", "linear", "ste"} {
			if one.GetName() == narrow {
				t.Errorf("a fresh system puts the %s skill in front of every session, and it is for one kind of work", narrow)
			}
		}
	}
	// The catalogue still carries them, so attaching one is a decision somebody makes rather than an
	// import they have to do first.
	catalogue, err := s.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	offered := map[string]bool{}
	for _, one := range catalogue.GetSkills() {
		offered[one.GetName()] = true
	}
	for _, want := range []string{"aws", "terraform", "outbound"} {
		if !offered[want] {
			t.Errorf("the catalogue of a fresh system does not hold the %s skill", want)
		}
	}
}

// names is what a listing carried, for a failure that says what was there instead.
func names(listed *quaycrewv1.ListSkillsResponse) []string {
	out := make([]string, 0, len(listed.GetSkills()))
	for _, one := range listed.GetSkills() {
		out = append(out, one.GetName())
	}
	return out
}
