package main

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// TestSkillListOfASessionSaysWhatItActuallyHolds addresses a session and reads back the crew's own
// skill, which no workspace attachment row records: the answer has to come from the same resolver
// the sandbox is built from.
func TestSkillListOfASessionSaysWhatItActuallyHolds(t *testing.T) {
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Skills: []skill.Skill{{Name: "git", Version: 1, Summary: "Branch first.", Brief: "Branch first."}},
	})

	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	handle := sessionFrom(t, mustRun(t, client, "dispatch", "hello"))

	listed := mustRun(t, client, "skill", "list", "me/house-bills/"+handle[:8])
	if !strings.Contains(listed, "git") {
		t.Fatalf("the session's listing does not name the crew's git skill: %q", listed)
	}
}
