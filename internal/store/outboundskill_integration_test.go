//go:build integration

package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
)

// The rule about calling something outside this process, followed from the directory this build ships
// to the file a session actually reads.
//
// The unit tier reads skills/outbound off disk and holds the words to the rule. What only this tier
// reaches is the crossing, and the crossing is the whole delivery: seeding writes the brief into rows,
// attaching it to the system decides which sessions get it, a session reads it back out, writes it
// into the workspace's directory and mounts it. A brief that is truncated in a column, a skill the
// seed imports and never attaches, or a mount pointing at a file nobody wrote all look identical to
// the unit tier and all leave the session with no rule.
//
// The workspace here sets no secret and attaches nothing, which is the workspace an operator has on
// their first day, and is the whole of "without being told to".
func TestASessionOnAFreshSystemIsGivenTheOutboundRule(t *testing.T) {
	s, boxes, _ := aFreshSystemSeededFromDisk(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "fetch the watch page and read the title out of it",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	session := sent.GetId()

	listed, err := s.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Session: session})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	var held *quaycrewv1.Skill
	names := make([]string, 0, len(listed.GetSkills()))
	for _, one := range listed.GetSkills() {
		names = append(names, one.GetName())
		if one.GetName() == "outbound" {
			held = one
		}
	}
	if held == nil {
		t.Fatalf("the session holds %v, and the outbound skill is not among them", names)
	}
	// A skill that is held and not given is the failure this skill cannot afford: it names no secret
	// precisely so that a workspace with no credentials still gets it.
	if held.GetLeftOut() != "" {
		t.Errorf("the session holds the outbound skill and is not given it: %q", held.GetLeftOut())
	}

	box, found := sandboxFor(boxes, session)
	if !found {
		t.Fatalf("the system built no sandbox for session %s", session)
	}
	at := skill.DirIn(sandbox.SkillsPath, "outbound")
	var source string
	for _, mount := range box.Mounts {
		if mount.Target != at {
			continue
		}
		source = mount.Source
		if !mount.ReadOnly {
			t.Error("the outbound skill is mounted writable, and a session may not edit its own capability")
		}
	}
	if source == "" {
		t.Fatalf("the sandbox mounts nothing at %s, so the index names a brief the container does not carry", at)
	}

	// The text the session actually opens, after the round trip through the database. Held to the file
	// this build ships rather than to a phrase, because a truncation at any point leaves a brief that
	// still contains the word it was checked for.
	shipped, err := os.ReadFile(filepath.Join("../../skills", "outbound", skill.BriefFile))
	if err != nil {
		t.Fatalf("reading the brief this build ships: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(source, skill.BriefFile))
	if err != nil {
		t.Fatalf("the mount points at no brief: %v", err)
	}
	if string(written) != string(shipped) {
		t.Errorf("the brief the session reads is %d bytes and the one this build ships is %d",
			len(written), len(shipped))
	}
	if !strings.Contains(string(written), "unknown") {
		t.Error("the brief the session reads never says unknown, which is the whole rule")
	}
}

// The line the session is told on every conversation, out of the database rather than off disk. The
// brief is opened when the work comes up; the summary is what decides whether it ever is.
func TestTheOutboundSummarySurvivesTheDatabase(t *testing.T) {
	s, _, _ := aFreshSystemSeededFromDisk(t)
	ctx := context.Background()
	workspace, _ := aProjectOnPostgres(t, s)

	listed, err := s.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	for _, one := range listed.GetSkills() {
		if one.GetName() != "outbound" {
			continue
		}
		if !one.GetSystem() {
			t.Error("the workspace holds the outbound skill as its own, so a workspace made tomorrow would not")
		}
		summary := strings.ToLower(one.GetSummary())
		if !strings.Contains(summary, "outside this process") {
			t.Errorf("the summary a session is told is %q", one.GetSummary())
		}
		return
	}
	t.Fatal("a workspace on a fresh system does not hold the outbound skill at all")
}
