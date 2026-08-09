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

// The linear skill is a brief over an API: no binary of its own beyond curl, a key by name, and the
// knowledge of which queries matter. What holds for the heavier skills holds here too: it loads by
// the same rules as an import, or it is the example everybody copies wrongly.
func TestTheShippedLinearSkillLoads(t *testing.T) {
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}

	var linear *Skill
	for i := range skills {
		if skills[i].Name == "linear" {
			linear = &skills[i]
		}
	}
	if linear == nil {
		t.Fatal("skills/ does not hold the linear skill")
	}

	found := false
	for _, declared := range linear.Binaries {
		if declared == "curl" {
			found = true
		}
	}
	if !found {
		t.Error("the linear skill does not declare curl, which is the only tool its brief uses")
	}

	what, declared := linear.Secrets["LINEAR_API_KEY"]
	if !declared {
		t.Error("the linear skill does not name LINEAR_API_KEY, so the brief has nothing to authenticate with")
	}
	if strings.TrimSpace(what) == "" {
		t.Error("LINEAR_API_KEY carries no line saying what it is")
	}

	brief := strings.ToLower(linear.Brief)
	for _, said := range []string{"graphql", "issue", "comment"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and reading and updating issues is the whole of what this skill is", said)
		}
	}
}
