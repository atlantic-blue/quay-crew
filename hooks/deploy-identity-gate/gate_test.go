package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The change the incident actually had: Terraform for six resources, and a pull request that said
// nothing about whether the identity applying it could create any of them.
var theInfrastructure = []string{"infra/main.tf", "infra/variables.tf"}

// The report the skill asks for, which is what a body has to carry to get past the gate.
const theReportSaid = `**What.** The transcript service, as Terraform.

Deploy identity arn:aws:iam::230345688874:role/transcript-deploy, checked with
iam:SimulatePrincipalPolicy: s3:CreateBucket allowed, dynamodb:CreateTable allowed,
lambda:CreateFunction allowed, iam:PassRole allowed. Nothing came back refused.`

// A pull request that creates infrastructure and says nothing about the identity is the pull request
// this gate exists for. Every spelling of it, because a gate that knows one spelling is a gate the
// next spelling walks through.
func TestAPullRequestThatCreatesInfrastructureAndSaysNothingIsRefused(t *testing.T) {
	for _, command := range []string{
		`gh pr create --title "the transcript service" --body "What. The service, as terraform. Why. It has to run somewhere."`,
		`gh pr create -t "the transcript service" -b "ships the stack"`,
		`gh pr create --title x --body="ships the stack"`,
		`gh --repo atlantic-blue/transcript pr create --title x --body y`,
		`sudo gh pr create --title x --body y`,
		`bash -c "gh pr create --title x --body y"`,
		`git push -u origin work && gh pr create --title x --body y`,
		// No body on the line at all: --fill writes one from the commit messages, so there is
		// nothing that could carry the report.
		`gh pr create --fill`,
		// The same call made underneath gh's own command.
		`gh api -X POST repos/atlantic-blue/transcript/pulls -f title=x -f body="ships the stack"`,
		`curl -X POST https://api.github.com/repos/atlantic-blue/transcript/pulls -d '{"title":"x","body":"ships the stack"}'`,
	} {
		refusal, refused := Decide(command, theInfrastructure)
		if !refused {
			t.Errorf("the gate let this through, and it is the pull request the incident opened:\n  %s", command)
			continue
		}
		if !strings.Contains(refusal.String(), "iam:SimulatePrincipalPolicy") {
			t.Errorf("the refusal does not name the question to ask, so the session is left guessing:\n  %s", refusal)
		}
		if !strings.Contains(refusal.String(), "main.tf") {
			t.Errorf("the refusal does not name the infrastructure it read, so nobody can check the gate was right:\n  %s", refusal)
		}
	}
}

// The other half of the rule, and the half that holds when the change cannot be read at all. The
// session asked, it was told no, and it is opening the pull request anyway.
func TestAPullRequestReportingADeniedActionIsRefused(t *testing.T) {
	for _, body := range []string{
		"Deploy identity arn:aws:iam::230345688874:user/terraform_user.\ns3:CreateBucket implicitDeny",
		"| s3:CreateBucket | implicitDeny |",
		"dynamodb:CreateTable  explicitDeny  the boundary policy refuses it",
	} {
		// No infrastructure in the change, so the only thing that can refuse this is the denial
		// itself. That is deliberate: the diff is read best effort and this half must not depend on it.
		refusal, refused := Decide(`gh pr create --title x --body `+quoted(body), nil)
		if !refused {
			t.Errorf("the gate opened a pull request over a denial:\n  %s", body)
			continue
		}
		if !strings.Contains(refusal.String(), "stops the work being ready") {
			t.Errorf("the refusal does not say the work is not ready, which is the whole rule:\n  %s", refusal)
		}
	}
}

// The refusal a gate gives is the only thing a session reads, so it carries the way through rather
// than a name to go and look up.
func TestTheRefusalSaysWhatToDoInstead(t *testing.T) {
	refusal, refused := Decide(`gh pr create --title x --body "ships the stack"`, theInfrastructure)
	if !refused {
		t.Fatal("the gate let the pull request through")
	}
	for _, said := range []string{
		"krewe target",
		"simulate-principal-policy",
		"in one call",
		"not run is not the same as passed",
		"/home/agent/skills/deploy-identity/SKILL.md",
	} {
		if !strings.Contains(refusal.String(), said) {
			t.Errorf("the refusal never says %q:\n%s", said, refusal)
		}
	}
}

// The direction that decides whether this gate is worth having. A gate that refuses wrongly blocks
// the work, and every role opens a pull request on every slice.
func TestTheWorkEveryRoleDoesGoesThrough(t *testing.T) {
	for _, one := range []struct {
		command string
		changed []string
	}{
		// The change touches no infrastructure, which is almost every change.
		{`gh pr create --title "519: feat: a gate" --body "What. Why."`, []string{"internal/skill/skill.go", "README.md"}},
		{`gh pr create --fill`, []string{"docs/HOOKS.md"}},
		// Infrastructure, and the body carries the report.
		{`gh pr create --title x --body ` + quoted(theReportSaid), theInfrastructure},
		// The honest third answer. No credential in the session and no simulator to call, said out
		// loud, which is what tells whoever merges to look first.
		{`gh pr create --title x --body "The deploy identity check did not run: this sandbox has no aws command line."`, theInfrastructure},
		// Not opening a pull request at all.
		{`gh pr view 12`, theInfrastructure},
		{`gh pr list`, theInfrastructure},
		{`gh api repos/atlantic-blue/transcript/pulls`, theInfrastructure},
		{`git push -u origin 519-feat-a-gate`, theInfrastructure},
		{`terraform validate`, theInfrastructure},
		{`git commit -m "feat: gh pr create is gated now"`, theInfrastructure},
	} {
		if refusal, refused := Decide(one.command, one.changed); refused {
			t.Errorf("the gate refused work a role does on every slice:\n  %s\n  %s", one.command, refusal)
		}
	}
}

// The words the simulator answers with are also the words anybody uses to explain what it answers.
// A gate that cannot tell those apart refuses the document that teaches the rule, and this pull
// request is one of those documents.
func TestAPageExplainingTheDecisionsIsNotAReportOfOne(t *testing.T) {
	body := `**What.** A gate that reads the deploy identity report.

allowed is a pass. implicitDeny means nothing grants the action, and explicitDeny means something
refuses it. Both are a no.`
	if refusal, refused := Decide(`gh pr create --title x --body `+quoted(body), []string{"docs/HOOKS.md"}); refused {
		t.Errorf("the gate refused a page that explains the decisions rather than reporting one:\n%s", refusal)
	}
}

// A body given as a file is the form the skill's own instructions produce, and a gate that could not
// read it would refuse every pull request written properly.
func TestABodyGivenAsAFileIsRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	if err := os.WriteFile(path, []byte(theReportSaid), 0o600); err != nil {
		t.Fatalf("writing the body: %v", err)
	}
	if refusal, refused := Decide(`gh pr create --title x --body-file `+path, theInfrastructure); refused {
		t.Errorf("the gate refused a body it could have read:\n%s", refusal)
	}

	empty := filepath.Join(dir, "nothing.md")
	if err := os.WriteFile(empty, []byte("What. Why."), 0o600); err != nil {
		t.Fatalf("writing the body: %v", err)
	}
	if _, refused := Decide(`gh pr create --title x -F `+empty, theInfrastructure); !refused {
		t.Error("the gate read a body file that carries no report and let it through")
	}
}

// Half a report is not a report. An identity with no actions says nothing was asked, and actions with
// no identity say nobody was asked about them.
func TestHalfAReportIsRefused(t *testing.T) {
	for _, body := range []string{
		"Deploy identity arn:aws:iam::230345688874:role/transcript-deploy. All good.",
		"Checked s3:CreateBucket and dynamodb:CreateTable. All allowed.",
	} {
		if _, refused := Decide(`gh pr create --title x --body `+quoted(body), theInfrastructure); !refused {
			t.Errorf("the gate accepted half a report:\n  %s", body)
		}
	}
}

// The question asked before the change is read, because reading a change means running git and this
// hook fires on every command a session runs.
func TestOnlyACommandThatOpensAPullRequestCostsAnything(t *testing.T) {
	for _, command := range []string{
		`gh pr create --fill`,
		`gh api -X POST repos/o/r/pulls -f title=x`,
		`curl -X POST https://api.github.com/repos/o/r/pulls -d '{}'`,
		`bash -c "gh pr create --fill"`,
	} {
		if !OpensAPullRequest(command) {
			t.Errorf("this opens a pull request and the gate would not have read the change: %s", command)
		}
	}
	for _, command := range []string{
		`ls`,
		`git status`,
		`gh pr view 12`,
		`gh pr merge 12`,
		`gh api repos/o/r/pulls`,
		`curl https://api.github.com/repos/o/r/pulls`,
	} {
		if OpensAPullRequest(command) {
			t.Errorf("this opens nothing and the gate would run git over it: %s", command)
		}
	}
}

// What the gate recognises as infrastructure, and what it counts.
func TestInfrastructureIsTerraformByName(t *testing.T) {
	built := Infrastructure([]string{"infra/main.tf", "README.md", "infra/lambda.tf.json", "infra/main.tfvars"})
	if len(built) != 2 {
		t.Fatalf("read %v, and the change declares two files of resources", built)
	}
	many := Infrastructure([]string{"a.tf", "b.tf", "c.tf", "d.tf", "e.tf", "f.tf"})
	if len(many) != 5 || !strings.Contains(many[4], "2 more") {
		t.Errorf("a refusal naming every file is one nobody reads to the end: %v", many)
	}
}

// quoted puts a body on a command line the way a session does.
func quoted(body string) string {
	return "'" + strings.ReplaceAll(body, "'", "") + "'"
}
