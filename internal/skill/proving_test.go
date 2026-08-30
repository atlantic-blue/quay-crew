package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shipped is the skill of that name in skills/, or nil. The skills are read from skills/ at the root of
// this repository, which is the directory the image carries, so a manifest that stops loading fails
// here rather than on somebody's first run.
func shipped(t *testing.T, name string) *Skill {
	t.Helper()
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("no skills found in skills/, so this test proves nothing")
	}
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i]
		}
	}
	return nil
}

// The proving skill says one thing: name the assumption that would waste the most work if it is
// false, and prove it in the runtime that has to run it. What it is for is in its brief, and this
// holds the brief to the three sentences it exists to carry.
func TestTheShippedProvingSkillLoads(t *testing.T) {
	proving := shipped(t, "proving")
	if proving == nil {
		t.Fatal("skills/ does not hold the proving skill")
	}

	brief := strings.ToLower(proving.Brief)
	for _, said := range []string{"assumption", "runtime", "not yet proved"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and proving the riskiest assumption where it has to hold is the whole of what this skill is", said)
		}
	}

	// The summary is the line every session holding the skill reads on every conversation, so it has
	// to say when to reach for the skill rather than what the skill is called.
	summary := strings.ToLower(proving.Summary)
	for _, said := range []string{"design", "runtime"} {
		if !strings.Contains(summary, said) {
			t.Errorf("the summary %q never says %q, so nothing tells a session designing something to open the brief", proving.Summary, said)
		}
	}
}

// A skill naming a secret the workspace has not set is left out of the session, and one naming a
// binary the image does not carry refuses the task. This skill is prose and nothing else, so it can
// name neither: a skill that reaches every session must have nothing that can leave it out of one.
func TestTheProvingSkillNeedsNothingThatCouldLeaveItOut(t *testing.T) {
	proving := shipped(t, "proving")
	if proving == nil {
		t.Fatal("skills/ does not hold the proving skill")
	}
	if len(proving.Secrets) != 0 {
		t.Errorf("the proving skill names the secrets %v, so a workspace that has not set them is a workspace where a design job is never offered it", proving.Secrets)
	}
	if len(proving.Binaries) != 0 {
		t.Errorf("the proving skill names the binaries %v, so an image without one refuses every task in every workspace", proving.Binaries)
	}
	if proving.HasSetup {
		t.Error("the proving skill runs a setup script, and there is nothing for it to set up")
	}
}

// The detail is in files beside the brief, which are mounted and cost nothing until the model opens
// one. A skill whose whole content is in the brief is paying the page ceiling for what a session
// reads once in ten jobs.
func TestTheProvingSkillKeepsItsMethodBesideTheBrief(t *testing.T) {
	proving := shipped(t, "proving")
	if proving == nil {
		t.Fatal("skills/ does not hold the proving skill")
	}
	if !strings.Contains(proving.Brief, "method.md") {
		t.Error("the brief does not point at method.md, so the detail beside it is never opened")
	}
	if _, err := os.Stat(filepath.Join(proving.Dir, "method.md")); err != nil {
		t.Errorf("the brief points at a file that is not there: %v", err)
	}
}
