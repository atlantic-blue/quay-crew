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

// The rule that infrastructure is not ready until the identity that applies it has been asked whether
// it may, followed from the directory this build ships to the file a session opens.
//
// The unit tier reads skills/deploy-identity off disk and holds the words to the rule. What only this
// tier reaches is the crossing: seeding writes the brief into rows, attaching it to the system decides
// which sessions hold it, and a dispatch reads it back out, writes it into the workspace's directory
// and mounts it. A brief truncated in a column, a skill imported and never attached, or a mount
// pointing at a file nobody wrote all look identical from the unit tier, and each leaves the session
// with a rule it cannot read.
//
// The workspace here sets no secret and attaches nothing, which is the workspace an operator has on
// their first day and is the whole of "without being told to". It is also the workspace of a project
// whose pipeline authenticates by federated identity, where there is no cloud credential to set.
func TestASessionOnAFreshSystemIsGivenTheDeployIdentityRule(t *testing.T) {
	s, boxes, _ := aFreshSystemSeededFromDisk(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "write the terraform for the transcript service",
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
	holding := make([]string, 0, len(listed.GetSkills()))
	for _, one := range listed.GetSkills() {
		holding = append(holding, one.GetName())
		if one.GetName() == "deploy-identity" {
			held = one
		}
	}
	if held == nil {
		t.Fatalf("the session holds %v, and the deploy-identity skill is not among them", holding)
	}
	// Held and not given is the failure this skill cannot afford, and it is why the manifest names no
	// secret and no binary: the workspaces that deploy are the ones most likely to be missing one.
	if held.GetLeftOut() != "" {
		t.Errorf("the session holds the deploy-identity skill and is not given it: %q", held.GetLeftOut())
	}

	box, found := sandboxFor(boxes, session)
	if !found {
		t.Fatalf("the system built no sandbox for session %s", session)
	}
	at := skill.DirIn(sandbox.SkillsPath, "deploy-identity")
	var source string
	for _, mount := range box.Mounts {
		if mount.Target != at {
			continue
		}
		source = mount.Source
		if !mount.ReadOnly {
			t.Error("the deploy-identity skill is mounted writable, so a session can edit the rule it is held to")
		}
	}
	if source == "" {
		t.Fatalf("the sandbox mounts nothing at %s, so the index names a brief the container does not carry", at)
	}

	// The text the session actually opens, after the round trip through the database. Held to the file
	// this build ships rather than to a phrase, because a truncation at any point leaves a brief that
	// still contains whatever word it was checked for.
	shipped, err := os.ReadFile(filepath.Join("../../skills", "deploy-identity", skill.BriefFile))
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
	if !strings.Contains(string(written), "iam:SimulatePrincipalPolicy") {
		t.Error("the brief the session reads never names the simulator, which is the whole rule")
	}
}
