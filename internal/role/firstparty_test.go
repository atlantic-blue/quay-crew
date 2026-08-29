package role

import (
	"os"
	"path/filepath"
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

// The line every brief owes a reader. Each one describes a boundary quay does not hold a session to,
// and a role whose file does not say so implies an isolation that is not there.
func TestEveryShippedRoleSaysWhatQuayDoesNotEnforce(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		if !strings.HasPrefix(one.Brief, "## What quay does not enforce") {
			t.Errorf("%s does not open by saying what quay does not enforce, so its brief reads as a boundary the crew keeps",
				one.Name)
		}
	}
}

// A role that names no model is refused at import, so every one of the twelve names one. This holds
// each of them to the model it was written for rather than to a default that could quietly move.
func TestTheShippedRolesRunOnTheModelsTheyWereWrittenFor(t *testing.T) {
	want := map[string]string{
		"architect": "opus", "assessor": "sonnet", "codebase-mapper": "sonnet",
		"debugger": "sonnet", "designer": "opus", "implementer": "sonnet",
		"marketing": "opus", "marketing-researcher": "opus", "security": "sonnet",
		"test-writer": "sonnet", "verifier": "sonnet", "wrapper": "sonnet",
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
// The assessor's brief spawns a security review and reads what came back; nothing else spawns
// anything, and a role that quietly gained a verb would go unnoticed without this.
func TestOnlyTheAssessorMayDeclareJob(t *testing.T) {
	roles, err := All(shipped)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	for _, one := range roles {
		switch one.Name {
		case "assessor":
			for _, verb := range []string{VerbJobCreate, VerbJobRead} {
				if !one.May(verb) {
					t.Errorf("the assessor may not %s, and its brief spawns a security review and reads the result", verb)
				}
			}
			if one.May(VerbJobStop) || one.May(VerbJobAnswer) {
				t.Error("the assessor may stop or answer job, and its brief asks for neither")
			}
		default:
			if len(one.May_) != 0 {
				t.Errorf("%s may %s, and its brief spawns nothing; default deny is what makes a grant mean something",
					one.Name, strings.Join(one.May_, ", "))
			}
		}
	}
}
