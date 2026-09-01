package role

import (
	"strings"
	"testing"
)

// The role that reads a plan before anybody builds it.
//
// The brief is the whole of this role: it holds no verbs, it writes nothing, and what it does is
// decided by the method written into its file. So the method is what is held up here, clause by
// clause, rather than the manifest alone.
//
// The clauses are read off the shipped file. A test that derived them from the file would agree with
// whatever the file said, which is why each one is written out.

// planCritic is the role this file is about.
const planCritic = "plan-critic"

// theClasses are the seven a finding may be. Six are imported from spec-kit's analyze.md, and the
// first is krewe's own: analyze.md checks a plan against itself and never asks whether the plan is
// the right product, which is the half of the failure that has no published source.
var theClasses = []struct {
	class string
	// says is the phrase the brief has to carry for that class to be named rather than implied.
	says string
}{
	{"does not serve the sentence", "Does not serve the sentence"},
	{"duplication", "Duplication"},
	{"ambiguity", "Ambiguity"},
	{"underspecification", "Underspecification"},
	{"conflict with the declared standards", "Conflict with the declared standards"},
	{"coverage gap", "Coverage gap"},
	{"inconsistency", "Inconsistency"},
}

// missingClasses is the check itself, kept apart from the test that runs it so a mutated brief can
// be put through the same code. A check that has never been watched catching something is worth
// nothing, and this one runs over a file that will keep being edited.
func missingClasses(brief string) []string {
	var missing []string
	for _, one := range theClasses {
		if !strings.Contains(brief, one.says) {
			missing = append(missing, one.class)
		}
	}
	return missing
}

// shippedPlanCritic reads the role off disk, and fails rather than skipping when it is not there. A
// test that skips when the role is missing reports a pass on a build that lost it.
func shippedPlanCritic(t *testing.T) Role {
	t.Helper()
	one, err := One(shipped + "/" + planCritic)
	if err != nil {
		t.Fatalf("reading the %s role: %v", planCritic, err)
	}
	return one
}

// The sad path first. A brief with a class taken out of it is caught, and named, by the same
// function the check below runs. Without this, a check that stopped matching would report a clean
// brief exactly as a clean brief does.
func TestTheClassCheckCatchesAClassTakenOutOfTheBrief(t *testing.T) {
	whole := shippedPlanCritic(t).Brief
	for _, one := range theClasses {
		t.Run(one.class, func(t *testing.T) {
			mutated := strings.ReplaceAll(whole, one.says, "Something else entirely")
			if mutated == whole {
				t.Fatalf("removing %q changed nothing, so the mutation never happened", one.says)
			}
			missing := missingClasses(mutated)
			if len(missing) != 1 {
				t.Fatalf("the check found %v missing, want only %q", missing, one.class)
			}
			if missing[0] != one.class {
				t.Fatalf("the check names %q and %q was taken out", missing[0], one.class)
			}
		})
	}
}

// And the happy path, which is the one the issue asks for by name: a brief that carries every class
// is not reported as missing one. A check that fires on everything stops every change to the file.
func TestTheShippedBriefNamesAllSevenClassesOfFinding(t *testing.T) {
	brief := shippedPlanCritic(t).Brief
	if missing := missingClasses(brief); len(missing) != 0 {
		t.Fatalf("the brief does not name %v, and a class it does not name is a class nobody looks for", missing)
	}
	t.Logf("the brief names %d classes of finding in %d bytes", len(theClasses), len(brief))
}

// The class that is ours. Six of the seven are imported, and the seventh is the whole reason this
// role exists: the source checks a plan against itself, and a plan can be perfectly consistent about
// the wrong product.
func TestTheBriefRequiresARequirementToTraceToTheJobsSentence(t *testing.T) {
	brief := shippedPlanCritic(t).Brief
	for _, want := range []string{
		// The sentence is what the job carries, so the brief has to say to read it.
		"The sentence.",
		"what a person does with what gets built",
		// And say what to do when the plan does not answer it.
		"the plan and the sentence disagree",
		// A plan with no sentence to trace to is the gap that makes every other class cheap.
		"If there is no sentence",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not say %q, so a plan that serves nobody reads as a clean plan", want)
		}
	}
}

// A finding nobody can locate is a finding somebody else has to find again.
func TestTheBriefRequiresALocationForEveryFinding(t *testing.T) {
	brief := shippedPlanCritic(t).Brief
	for _, want := range []string{"Where it is.", "the heading or the section number"} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not say %q, so a report can name a class and no place", want)
		}
	}
}

// The sharper half of the import: the report tests what is written, never what will run. Nothing has
// run when this role reads, so a finding about behaviour is a finding about nothing.
func TestTheBriefTestsTheRequirementsRatherThanTheBuild(t *testing.T) {
	brief := shippedPlanCritic(t).Brief
	for _, want := range []string{
		"unit tests for English",
		// The source's own example, in the words this crew's failure gave it.
		"is the number of cards written down",
		"what a person types and what they get back",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not say %q, so it reads as a review of a build that does not exist", want)
		}
	}
}

// A role that refuses everything stops every run, so the brief has to say what a plan that holds up
// gets. This is the direction a test about catching a bad plan never reaches.
func TestTheBriefSaysWhatHappensWhenThePlanHoldsUp(t *testing.T) {
	brief := shippedPlanCritic(t).Brief
	for _, want := range []string{
		"Say so, in one line",
		"Never invent a finding",
		"say which five are clean",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not say %q, so a clean plan and a broken one read the same", want)
		}
	}
}

// It reports and it changes nothing. The manifest is half of that, and the brief is the other half:
// krewe has no word for a file, so what stops this role editing the plan it is reading is prose.
func TestThePlanCriticReportsAndChangesNothing(t *testing.T) {
	one := shippedPlanCritic(t)
	if len(one.Verbs) != 0 {
		t.Errorf("%s may %s, and a role that reads a plan needs to call nothing",
			planCritic, strings.Join(one.Verbs, ", "))
	}
	for _, verb := range Grantable {
		if one.May(verb) {
			t.Errorf("%s may %s", planCritic, verb)
		}
	}
	for _, want := range []string{"You change no file", "You write no code and no design"} {
		if !strings.Contains(one.Brief, want) {
			t.Errorf("the brief does not say %q, and nothing else says it", want)
		}
	}
}

// Where the method came from, kept where a reader of the role finds it. `krewe role show` prints the
// brief in full, and it prints no other file, so the notice belongs in the brief rather than beside
// it.
func TestTheBriefRecordsTheLicenceAndTheAddressItWasImportedFrom(t *testing.T) {
	brief := shippedPlanCritic(t).Brief
	for _, want := range []string{
		"github/spec-kit",
		"templates/commands/analyze.md",
		"templates/commands/checklist.md",
		"MIT",
		"GitHub, Inc.",
		"https://github.com/github/spec-kit/blob/main/LICENSE",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not record %q, and a reader of the role has nowhere else to look", want)
		}
	}
}

// It reads a plan and it judges whether the plan is the right product, which is the expensive
// judgement rather than a file written to a specification. It receives the context because the
// declared standards it checks a plan against are in it.
func TestThePlanCriticRunsOnOpusAndReceivesWhatItReads(t *testing.T) {
	one := shippedPlanCritic(t)
	if one.Model != "opus" {
		t.Errorf("%s runs on %q, and judging whether a plan is the right product is the expensive judgement",
			planCritic, one.Model)
	}
	for _, material := range []string{MaterialJob, MaterialContext, MaterialSkills} {
		if !one.Gets(material) {
			t.Errorf("%s receives %s, and it cannot read a plan without %s",
				planCritic, strings.Join(one.Receives, ", "), material)
		}
	}
	if len(one.Brief) > BriefLimit {
		t.Errorf("the brief is %d bytes and the ceiling is %d", len(one.Brief), BriefLimit)
	}
}
