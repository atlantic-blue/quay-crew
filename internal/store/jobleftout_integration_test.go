//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
)

// What a declaration says about the credentials the workspace does not have, over the real database
// and the skills this build ships.
//
// A workspace with no credential took a whole tree of job and said nothing, and every session in it
// would have died on its first clone. The unit tier proves the sentence against a skill a test made
// up. What only this tier reaches is the crossing: the workspace, the project, the role and the
// secret are all rows, the skills are the ones an operator actually gets, and the answer is assembled
// from rows a different call wrote.

// declarationLeftOut declares one job and answers with the skills the crew said its session starts
// without, keyed by name against the reason.
func declarationLeftOut(t *testing.T, s *controlplane.Server, project, named string) map[string]string {
	t.Helper()
	declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "fix the defect", Brief: "clone it and push a branch", Role: named,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	out := map[string]string{}
	for _, one := range declared.GetLeftOut() {
		out[one.GetName()] = one.GetLeftOut()
	}
	return out
}

// TestDeclaringJobNamesTheSkillsTheWorkspaceCannotSupply is the whole of what this change buys,
// against the database that holds it.
func TestDeclaringJobNamesTheSkillsTheWorkspaceCannotSupply(t *testing.T) {
	s, _ := aCrewThatHoldsSkills(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	// The git and github skills this build ships both name GH_TOKEN, and nothing has set it, which is
	// exactly the workspace the acceptance run made.
	left := declarationLeftOut(t, s, project, "")
	for _, name := range []string{"git", "github"} {
		why, named := left[name]
		if !named {
			t.Fatalf("the declaration names %v, want it to name the %s skill", keysOf(left), name)
		}
		for _, want := range []string{"GH_TOKEN", "quay secret set"} {
			if !strings.Contains(why, want) {
				t.Errorf("the declaration says the %s skill is left out saying %q, want it to say %q",
					name, why, want)
			}
		}
	}

	// Setting it is the whole of the fix for those two. The others this build ships name credentials
	// of their own and are still reported, which is the answer being about this workspace rather than
	// a flag that flips once.
	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace: workspace, Key: "GH_TOKEN", Value: "a token",
	}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	after := declarationLeftOut(t, s, project, "")
	for _, name := range []string{"git", "github"} {
		if _, named := after[name]; named {
			t.Errorf("the declaration still names the %s skill once GH_TOKEN is set: %v", name, keysOf(after))
		}
	}
	if len(after) == 0 {
		t.Error("the declaration names nothing at all, so this says the answer is a flag rather than a reading of the workspace")
	}
}

// A role that does not receive skills is given none of them by design, so there is no gap to report.
// The role is a row here rather than a manifest a test parsed, which is the half a unit tier cannot
// reach: the boundary is read back by the code that answers the declaration.
func TestAJobWhoseRoleReceivesNoSkillsIsToldNothingAboutThem(t *testing.T) {
	s, _ := aCrewThatHoldsSkills(t)
	workspace, project := aProjectOnPostgres(t, s)
	importRoleOnPostgres(t, s, workspace, "backlog-clearer", 1, "job", "context")
	importRoleOnPostgres(t, s, workspace, "releaser", 1, "job", "skills")

	if left := declarationLeftOut(t, s, project, "backlog-clearer"); len(left) > 0 {
		t.Errorf("the declaration names %v, want nothing for a role that receives no skills", keysOf(left))
	}
	// And the same workspace, the same missing secret, a role that does receive them: without this
	// the check above would pass on a crew that says nothing to anybody.
	if left := declarationLeftOut(t, s, project, "releaser"); len(left) == 0 {
		t.Error("the declaration names nothing for a role that does receive skills")
	}
}

// keysOf is what a listing held, for a failure to print.
func keysOf(held map[string]string) []string {
	var names []string
	for name := range held {
		names = append(names, name)
	}
	return names
}
