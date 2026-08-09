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

// The jira skill is an api skill like linear: same shape, different API. It authenticates as an
// email and token pair, and where the instance lives is workspace context rather than skill
// content, so the brief has to say where to find it instead of carrying an address.
func TestTheShippedJiraSkillLoads(t *testing.T) {
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}

	var jira *Skill
	for i := range skills {
		if skills[i].Name == "jira" {
			jira = &skills[i]
		}
	}
	if jira == nil {
		t.Fatal("skills/ does not hold the jira skill")
	}

	found := false
	for _, declared := range jira.Binaries {
		if declared == "curl" {
			found = true
		}
	}
	if !found {
		t.Error("the jira skill does not declare curl, which is the only tool its brief uses")
	}

	for _, name := range []string{"JIRA_EMAIL", "JIRA_API_TOKEN"} {
		what, declared := jira.Secrets[name]
		if !declared {
			t.Errorf("the jira skill does not name %s, and Jira authenticates with the pair", name)
			continue
		}
		if strings.TrimSpace(what) == "" {
			t.Errorf("%s carries no line saying what it is", name)
		}
	}

	brief := strings.ToLower(jira.Brief)
	for _, said := range []string{"issue", "comment", "transition"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and reading and moving issues is the whole of what this skill is", said)
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

// The aws skill reads and never mutates: describe, list and logs are free, and anything that
// changes infrastructure ships through a pull request. The heaviest binary in the image, which is
// why its cost is stated where the skill is declared.
func TestTheShippedAwsSkillLoads(t *testing.T) {
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}

	var aws *Skill
	for i := range skills {
		if skills[i].Name == "aws" {
			aws = &skills[i]
		}
	}
	if aws == nil {
		t.Fatal("skills/ does not hold the aws skill")
	}

	found := false
	for _, declared := range aws.Binaries {
		if declared == "aws" {
			found = true
		}
	}
	if !found {
		t.Error("the aws skill does not declare the aws binary")
	}

	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		what, declared := aws.Secrets[name]
		if !declared {
			t.Errorf("the aws skill does not name %s, and the command line reads the pair from the environment", name)
			continue
		}
		if strings.TrimSpace(what) == "" {
			t.Errorf("%s carries no line saying what it is", name)
		}
	}

	brief := strings.ToLower(aws.Brief)
	for _, said := range []string{"describe", "never", "pull request"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and read but never mutate is the whole of what this skill is", said)
		}
	}
}
