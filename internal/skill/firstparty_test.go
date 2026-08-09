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

// The github skill is the second shipped one, and it is separate from git on purpose: git needs a
// repository and nothing else, github needs a credential, the network, and it does things that
// cannot be undone.
func TestTheShippedGitHubSkillLoads(t *testing.T) {
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}

	var github *Skill
	for i := range skills {
		if skills[i].Name == "github" {
			github = &skills[i]
		}
	}
	if github == nil {
		t.Fatal("skills/ does not hold the github skill")
	}

	for _, binary := range []string{"git", "gh"} {
		found := false
		for _, declared := range github.Binaries {
			if declared == binary {
				found = true
			}
		}
		if !found {
			t.Errorf("the github skill does not declare the %s binary, so a sandbox missing it is discovered halfway through instead of refused with a sentence", binary)
		}
	}

	what, declared := github.Secrets["GH_TOKEN"]
	if !declared {
		t.Error("the github skill does not name GH_TOKEN, and gh cannot authenticate without it")
	}
	if strings.TrimSpace(what) == "" {
		t.Error("GH_TOKEN carries no line saying what it is")
	}

	brief := strings.ToLower(github.Brief)
	for _, said := range []string{"pull request", "branch", "merge"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and how pull requests are opened here is the whole of what this skill is", said)
		}
	}
}

// The terraform skill carries the standing rule in its brief: plans are free, applies never happen
// from a session. The binary is the heaviest thing a skill has asked of the image so far, which is
// why its cost is stated where the skill is declared.
func TestTheShippedTerraformSkillLoads(t *testing.T) {
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}

	var terraform *Skill
	for i := range skills {
		if skills[i].Name == "terraform" {
			terraform = &skills[i]
		}
	}
	if terraform == nil {
		t.Fatal("skills/ does not hold the terraform skill")
	}

	found := false
	for _, declared := range terraform.Binaries {
		if declared == "terraform" {
			found = true
		}
	}
	if !found {
		t.Error("the terraform skill does not declare the terraform binary")
	}

	brief := strings.ToLower(terraform.Brief)
	for _, said := range []string{"plan", "never", "pull request"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and plan but never apply is the whole of what this skill is", said)
		}
	}
	if !strings.Contains(brief, "apply") {
		t.Error("the brief never mentions apply, so the one thing a session must not do goes unsaid")
	}
}
