package skill

import (
	"strings"
	"testing"
)

// The git skill is the first skill the crew ships, in skills/ at the root of this repository, and this
// holds it to the same rules an imported skill answers to. A first party skill that does not load is
// worse than none: it is the example everybody copies.
func TestTheShippedGitSkillLoads(t *testing.T) {
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("no skills found in skills/, so this test proves nothing")
	}

	var git *Skill
	for i := range skills {
		if skills[i].Name == "git" {
			git = &skills[i]
		}
	}
	if git == nil {
		t.Fatal("skills/ does not hold the git skill")
	}

	found := false
	for _, binary := range git.Binaries {
		if binary == "git" {
			found = true
		}
	}
	if !found {
		t.Error("the git skill does not declare the git binary, so a sandbox missing it is discovered halfway through instead of refused with a sentence")
	}

	what, declared := git.Secrets["GH_TOKEN"]
	if !declared {
		t.Error("the git skill does not name GH_TOKEN, so the credential helper in the image has nothing to read")
	}
	if strings.TrimSpace(what) == "" {
		t.Error("GH_TOKEN carries no line saying what it is, so a refusal cannot say which credential to go and get")
	}

	for _, said := range []string{"clone", "branch", "commit"} {
		if !strings.Contains(strings.ToLower(git.Brief), said) {
			t.Errorf("the brief never says %q, and how work is done here is the whole of what this skill is", said)
		}
	}
}
