package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/job"
)

// The brief the acceptance run was given, and the ones next to it. Each asks the job to hold until
// something outside it reports, and a job holds until nothing.
func TestABriefThatAsksTheJobToWaitIsRefused(t *testing.T) {
	for _, brief := range []string{
		"fix the defect, push, watch the checks and merge on green",
		"open the pull request and wait for the checks to land",
		"push the branch, then poll continuous integration until it is green",
		"push it and monitor the workflow, then report",
		"open the pull request and merge it when the checks pass",
		"squash and merge the pull request once it is reviewed",
		"raise the pull request, wait for CI, merge on green",
	} {
		d := declared()
		d.Brief = brief
		err := d.Validate()
		if err == nil {
			t.Errorf("%q was accepted, and no job can do it", brief)
			continue
		}
		for _, phrase := range []string{"cannot wait", "flow", "quay flow import"} {
			if !strings.Contains(err.Error(), phrase) {
				t.Errorf("the refusal of %q says %q, want it to say %q", brief, err, phrase)
			}
		}
	}
}

// The refusal quotes the words that were typed, because a person shown their own sentence sees what
// to change.
func TestTheRefusalQuotesTheBrief(t *testing.T) {
	d := declared()
	d.Brief = "fix the defect, push, watch the checks and merge on green"

	err := d.Validate()
	if err == nil {
		t.Fatal("the brief was accepted")
	}
	if !strings.Contains(err.Error(), "watch the checks") {
		t.Fatalf("the refusal says %q, want it to quote the words in the brief", err)
	}
}

// Ordinary work that happens to hold one of these words is not a wait. Every line here is a brief
// somebody would write for this repository, and refusing any of them would make the rule the thing
// people work around.
func TestABriefThatDoesOrdinaryWorkIsAccepted(t *testing.T) {
	for _, brief := range []string{
		"open the bill and say when it is due",
		"merge origin/main into the branch, then run the gates",
		"resolve the merge conflict in the store and push",
		"open the pull request and do not merge it",
		"push the branch and open the pull request; the merge is somebody else's",
		"run the test suite and wait for it to finish before reporting",
		"check the spelling of every heading in the README",
		"watch out for a session that answers without a pull request",
		"write the merge strategy down in docs/ORCHESTRATION.md",
	} {
		d := declared()
		d.Brief = brief
		if err := d.Validate(); err != nil {
			t.Errorf("%q was refused: %v", brief, err)
		}
	}
}

// The phrase is read off the brief, so the caller sees the same words twice: once as they typed
// them and once in the refusal.
func TestOnlyAFlowCanNamesThePhrase(t *testing.T) {
	for brief, want := range map[string]string{
		"push, then watch the checks":                "watch the checks",
		"merge on green":                             "merge on green",
		"open the bill and say when it is due":       "",
		"open the pull request and do not merge it":  "",
		"merge origin/main into the branch and push": "",
		"wait\n  for the checks":                     "wait for the checks",
	} {
		if got := job.OnlyAFlowCan(brief); got != want {
			t.Errorf("%q asks for %q, want %q", brief, got, want)
		}
	}
}
