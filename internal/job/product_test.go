package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The sentence is one line wherever it is read: in a listing, on `krewe job show` and in front of a
// session. A person pastes it out of a document, so it arrives wrapped often enough to matter.
func TestTheSentenceIsStoredAsOneLine(t *testing.T) {
	d := declared()
	d.Product = "  paste a link\n  and get the text back  "
	if got := d.Tidied().Product; got != "paste a link and get the text back" {
		t.Fatalf("the sentence is stored as %q", got)
	}
}

// It is one sentence a person would say. A paragraph here is a design document arriving by the back
// door, which is the thing this field exists to sit above.
func TestASentenceOverTheCeilingIsRefused(t *testing.T) {
	d := declared()
	d.Product = strings.Repeat("a", job.ProductLimit+1)
	err := d.Validate()
	if err == nil {
		t.Fatal("a sentence of 201 bytes was accepted")
	}
	for _, phrase := range []string{"201", "200", "what somebody does"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("the refusal is %q, want it to say %q", err, phrase)
		}
	}
}

func TestASentenceAtTheCeilingIsKept(t *testing.T) {
	d := declared()
	d.Product = strings.Repeat("a", job.ProductLimit)
	if err := d.Validate(); err != nil {
		t.Fatalf("a sentence of %d bytes was refused: %v", job.ProductLimit, err)
	}
}

// The point of the field. A session given the brief alone builds what the brief says, which is how a
// faithful run delivers something nobody can use.
func TestTheSessionIsAskedForTheSentenceAboveTheBrief(t *testing.T) {
	asked := job.Asked(&job.Job{
		Brief:   "make the address read /videos?id=<video id>",
		Product: "paste a link and get the text back",
	})
	for _, phrase := range []string{
		"paste a link and get the text back",
		"the sentence wins",
		"make the address read /videos?id=<video id>",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
	if strings.Index(asked, "paste a link") > strings.Index(asked, "make the address") {
		t.Fatalf("the brief is above the sentence it serves:\n%s", asked)
	}
}

// A job that carries no sentence has none put above its brief.
func TestAJobWithNoSentenceIsAskedItsBriefWithNothingAboveIt(t *testing.T) {
	brief := "open the bill and say when it is due"
	asked := job.Asked(&job.Job{Brief: brief})
	if !strings.Contains(asked, brief) {
		t.Fatalf("the session was asked %q, want %q", asked, brief)
	}
	if strings.Contains(asked, "This job serves one sentence") {
		t.Fatalf("the session was asked %q, and this job carries no sentence", asked)
	}
}

// The sentence and the line about the pull request are both the system's, and a job carrying both
// gets both.
func TestAJobCarryingASentenceAndARepositoryGetsBoth(t *testing.T) {
	// Its plan is approved, because a job that states a sentence writes its plan before it is asked
	// for any work: the task that carries the brief and the repository is the one after the approval.
	asked := job.Asked(&job.Job{
		Brief: "sort the listing", Product: "paste a link and get the text back",
		Repository: "atlantic-blue/quay-crew",
		Plan:       "Step 1: sort it", PlanApproved: true,
	})
	for _, phrase := range []string{"paste a link and get the text back", "sort the listing", "Do not merge"} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
}
