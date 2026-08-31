package role

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// shipped is where the roles this build ships live, from this package's directory.
const shipped = "../../roles"

// The load bearing check: every role in roles/ answers to the rules an imported role answers to. It
// reads the directory rather than a list written here, so a role added tomorrow is covered without
// anybody remembering to add it, and All refuses an empty directory, so a roles/
// that lost its contents fails rather than reporting a clean run over nothing.
//
// A first party role that does not import is worse than none: it is the example everybody copies.
func TestEveryShippedRoleImports(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	// The count is reported so a run that found two roles cannot read as a run that found them all.
	t.Logf("roles/ holds %d roles", len(roles))

	onDisk, err := os.ReadDir(shipped)
	if err != nil {
		t.Fatalf("reading %s: %v", shipped, err)
	}
	directories := 0
	for _, entry := range onDisk {
		if entry.IsDir() {
			directories++
		}
	}
	if directories != len(roles) {
		t.Errorf("roles/ holds %d directories and only %d of them load, so one ships and cannot be imported",
			directories, len(roles))
	}

	for _, one := range roles {
		if one.Name != filepath.Base(one.Dir) {
			t.Errorf("%s calls itself %q, and a role is the directory it lives in", one.Dir, one.Name)
		}
		if !one.Gets(MaterialJob) {
			t.Errorf("%s does not receive %s, and a session with no job to do is not a task", one.Name, MaterialJob)
		}
		if len(one.Brief) > BriefLimit {
			t.Errorf("%s has a brief of %d bytes and the ceiling is %d", one.Name, len(one.Brief), BriefLimit)
		}
	}
}

// The line every brief owes a reader. Each one describes a boundary krewe does not hold a session to,
// and a role whose file does not say so implies an isolation that is not there.
func TestEveryShippedRoleSaysWhatKreweDoesNotEnforce(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		if !strings.HasPrefix(one.Brief, "## What krewe does not enforce") {
			t.Errorf("%s does not open by saying what krewe does not enforce, so its brief reads as a boundary the system keeps",
				one.Name)
		}
	}
}

// A role that names no model is refused at import, so every one of them names one. This holds each
// to the model it was written for rather than to a default that could quietly move.
func TestTheShippedRolesRunOnTheModelsTheyWereWrittenFor(t *testing.T) {
	want := map[string]string{
		"architect": "opus", "assessor": "sonnet", "codebase-mapper": "sonnet",
		"debugger": "sonnet", "designer": "opus", "implementer": "sonnet",
		"marketing": "opus", "marketing-researcher": "opus", "security": "sonnet",
		"test-writer": "sonnet", "verifier": "sonnet", "wrapper": "sonnet",
		"orchestrator": "opus", "infrastructure-writer": "opus", "releaser": "sonnet",
		"writer": "opus",
	}
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	if len(roles) != len(want) {
		t.Fatalf("roles/ holds %d roles and this test knows %d of them", len(roles), len(want))
	}
	for _, one := range roles {
		expected, known := want[one.Name]
		if !known {
			t.Errorf("roles/ holds %q, which this test does not know about", one.Name)
			continue
		}
		if one.Model != expected {
			t.Errorf("%s runs on %q and this build ships it on %q", one.Name, one.Model, expected)
		}
	}
}

// Default deny is the rule, so a verb in a shipped role is something its brief actually asks for.
// The table is written out rather than derived, because a grant that appeared without anybody
// deciding it is exactly what this catches, and a test that derived the answer from the files would
// agree with whatever the files said.
func TestEachShippedRoleMayOnlyWhatItsBriefAsksFor(t *testing.T) {
	want := map[string][]string{
		// Its brief declares a security review and reads what came back.
		"assessor": {VerbJobCreate, VerbJobRead},
		// It declares the tree, reads the answers, and stops a child that has gone wrong.
		"orchestrator": {VerbJobCreate, VerbJobRead, VerbJobStop},
		// It declares one child per deliverable that has its own review.
		"infrastructure-writer": {VerbJobCreate, VerbJobRead},
		// It reads the job it was given and releases it. A session that can push and can also fan
		// out could spend a whole budget on pushes nobody reviewed.
		"releaser": {VerbJobRead},
		// It writes about work this system did, so it reads that work rather than being handed it. A
		// role that writes about the system and cannot read the system is the failure issue 543
		// describes: the facts get typed into its brief by hand, and the brief outgrows the piece.
		"marketing": {VerbJobRead},
	}
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	granted := 0
	for _, one := range roles {
		expected, known := want[one.Name]
		if !known {
			if len(one.Verbs) != 0 {
				t.Errorf("%s may %s, and its brief declares nothing; default deny is what makes a grant mean something",
					one.Name, strings.Join(one.Verbs, ", "))
			}
			continue
		}
		granted++
		if strings.Join(one.Verbs, ", ") != strings.Join(sorted(expected), ", ") {
			t.Errorf("%s may %s, and this build grants it %s",
				one.Name, strings.Join(one.Verbs, ", "), strings.Join(sorted(expected), ", "))
		}
	}
	if granted != len(want) {
		t.Errorf("%d of the %d roles this test grants verbs to are in roles/", granted, len(want))
	}
}

// sorted is what a manifest's list becomes when it is read, so the comparison above is against the
// same shape rather than against the order somebody typed.
func sorted(verbs []string) []string {
	out := append([]string(nil), verbs...)
	sort.Strings(out)
	return out
}

// Every role can push. A push is not a deploy: what runs a pipeline is a merge, and no role merges,
// so taking the push away removes the operator's sight of work in flight and stops nothing.
//
// A repository reaches a session here through the git skill and nothing is cloned for one, so a role
// that does not receive `skills` holds no git tool and cannot open a pull request whatever its brief
// says. That makes `receives: skills` the whole of "this role can push", and it is why this is a
// check on the manifest rather than on the prose.
func TestEveryShippedRoleReceivesTheSkillsItWouldPushWith(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		if !one.Gets(MaterialSkills) {
			t.Errorf("%s receives %s, so its sandbox holds no git tool and it cannot push what it wrote",
				one.Name, strings.Join(one.Receives, ", "))
		}
	}
	t.Logf("%d roles receive skills", len(roles))
}

// The claim a brief may no longer make.
//
// The orchestrator's brief used to say that merging is not a verb the system has, so nothing stops a
// session merging except the brief itself. That was honest when it was written and it is false now:
// the system ships a hook that reads each command before it runs and refuses one that merges.
//
// A brief is prose, so nothing but this stops the sentence coming back. It is written as the claim
// rather than as the correction, because what matters is that no role tells a session the gate is
// not there.
func TestNoShippedBriefTellsASessionThatNothingStopsItMerging(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		if strings.Contains(one.Brief, "nothing stops you from merging") {
			t.Errorf("%s tells a session nothing stops it merging, and a hook does", one.Name)
		}
	}
}

// And the other direction, because the sweep above passes over a brief that says nothing at all. The
// orchestrator is the role that ends a slice, so it is the one that has to say where the gate is.
func TestTheOrchestratorSaysTheMergeIsRefusedByAHook(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		if one.Name != "orchestrator" {
			continue
		}
		if !strings.Contains(one.Brief, "refuses one that merges") {
			t.Error("the orchestrator's brief does not say a hook refuses a merge, so a session reads the gate as advice")
		}
		return
	}
	t.Fatal("this build ships no orchestrator, so this test proves nothing")
}

// The orchestrator says what it will create before it creates it.
//
// A cost rule in prose is advice, and a model can read it, agree with it and still choose a store
// that bills a minimum capacity continuously, which is what happened on 29 August 2026. Advice is
// all this line is too, and the difference is that the system now has somewhere for the session to
// stop: it can name the resources and their cost at rest, ask, and wait. A brief that does not name
// the command is a brief that leaves guessing as the only move it knows about.
func TestTheOrchestratorStatesWhatItWillCreateBeforeItBuilds(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		if one.Name != "orchestrator" {
			continue
		}
		for _, want := range []string{"krewe job ask", "costs while nobody is using it"} {
			if !strings.Contains(one.Brief, want) {
				t.Errorf("the orchestrator's brief does not say %q, so a choice that bills stays invisible "+
					"until it is built", want)
			}
		}
		return
	}
	t.Fatal("this build ships no orchestrator, so this test proves nothing")
}
