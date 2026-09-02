package job_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A plan is read off what the session answered, in one shape the system can find. Anything it cannot
// read is not a plan: it is prose about planning, and putting that in front of a person to approve
// is the same compression fault one level up.
func TestAPlanIsReadOffTheReply(t *testing.T) {
	steps, err := job.ReadPlan(`Here is what I will do.

Step 1: read the design and the issue
Step 2: build the address that takes a link
Step 3: write the tests

That is the whole of it.`)
	if err != nil {
		t.Fatalf("this reply carries a plan, and it was refused: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("read %d steps, want 3: %v", len(steps), steps)
	}
	if steps[0].Number != 1 || steps[0].Text != "read the design and the issue" {
		t.Fatalf("the first step read back as %+v", steps[0])
	}
	// Kept as the system's own lines rather than as the reply, so what a person approves and what the
	// session is later held to are the same text.
	kept := job.PlanText(steps)
	if strings.Contains(kept, "That is the whole of it") {
		t.Fatalf("the kept plan carries the reasoning around it: %q", kept)
	}
	if kept != "Step 1: read the design and the issue\n"+
		"Step 2: build the address that takes a link\n"+
		"Step 3: write the tests" {
		t.Fatalf("the plan was kept as %q", kept)
	}
}

func TestAReplyWithNoPlanIsRefused(t *testing.T) {
	_, err := job.ReadPlan("I will read the design, then build the page, then write some tests.")
	if err == nil {
		t.Fatal("prose about planning was read as a plan")
	}
	if !strings.Contains(err.Error(), "Step 1:") {
		t.Fatalf("the refusal does not say what shape to write: %v", err)
	}
}

func TestAnEmptyReplyIsRefused(t *testing.T) {
	if _, err := job.ReadPlan(""); err == nil {
		t.Fatal("an empty reply was read as a plan")
	}
}

// The ceiling is the whole point. A plan as long as the work costs the reading and buys nothing.
func TestAPlanLongerThanTheCeilingIsRefused(t *testing.T) {
	var lines []string
	for i := 1; i <= job.PlanSteps+1; i++ {
		lines = append(lines, fmt.Sprintf("Step %d: do the %dth thing", i, i))
	}
	_, err := job.ReadPlan(strings.Join(lines, "\n"))
	if err == nil {
		t.Fatalf("a plan of %d steps was accepted", job.PlanSteps+1)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", job.PlanSteps)) {
		t.Fatalf("the refusal does not name the ceiling: %v", err)
	}
}

func TestAStepLongerThanTheCeilingIsRefused(t *testing.T) {
	_, err := job.ReadPlan("Step 1: " + strings.Repeat("a", job.PlanStepLimit+1))
	if err == nil {
		t.Fatal("a step over the ceiling was accepted")
	}
	if !strings.Contains(err.Error(), "step 1") {
		t.Fatalf("the refusal does not name the step: %v", err)
	}
}

// The numbers are how the work is later accounted for, so a plan that numbers its steps 1, 2, 2 or
// 1, 3, 4 leaves an account nobody can read.
func TestAPlanNumberedWithAGapOrARepeatIsRefused(t *testing.T) {
	for _, reply := range []string{
		"Step 1: read it\nStep 3: build it",
		"Step 1: read it\nStep 1: build it",
		"Step 2: read it\nStep 3: build it",
	} {
		if _, err := job.ReadPlan(reply); err == nil {
			t.Fatalf("this numbering was accepted: %q", reply)
		}
	}
}

// Out of order is not the same fault as missing. A model that writes its steps in a jumbled order
// still numbered them all, so the plan is kept in the order the numbers give.
func TestAPlanWrittenOutOfOrderIsKeptInOrder(t *testing.T) {
	steps, err := job.ReadPlan("Step 2: build it\nStep 1: read it")
	if err != nil {
		t.Fatalf("a plan numbered 1 and 2 was refused: %v", err)
	}
	if job.PlanText(steps) != "Step 1: read it\nStep 2: build it" {
		t.Fatalf("the plan was kept as %q", job.PlanText(steps))
	}
}

// The sentence is the trigger, so nothing new is declared and nothing else is planned.
func TestOnlyAJobThatStatesTheSentenceIsPlanned(t *testing.T) {
	for _, one := range []struct {
		name    string
		job     *job.Job
		planned bool
	}{
		{"states the sentence, at the top", &job.Job{Product: "paste a link and get the text"}, true},
		{"an errand, which states none", &job.Job{Brief: "read the electricity bill"}, false},
		{"under another job, which is part of a plan already approved",
			&job.Job{Product: "paste a link and get the text", Parent: "abc"}, false},
	} {
		if got := job.Planned(one.job); got != one.planned {
			t.Fatalf("%s: planned is %t, want %t", one.name, got, one.planned)
		}
	}
}

func TestOnlyYesApprovesThePlan(t *testing.T) {
	for _, yes := range []string{"yes", "Yes", " YES "} {
		if !job.ApprovesThePlan(yes) {
			t.Fatalf("%q did not approve the plan", yes)
		}
	}
	for _, no := range []string{"", "no", "yes, but build the page first", "sure", "ok"} {
		if job.ApprovesThePlan(no) {
			t.Fatalf("%q was read as an approval", no)
		}
	}
}

// The measurement that makes an approval worth something: the numbers the session recorded, held
// against the numbers the plan carries.
func TestTheRecordIsHeldAgainstTheApprovedPlan(t *testing.T) {
	plan := "Step 1: read the design\nStep 2: build the address\nStep 3: write the tests"
	for _, one := range []struct {
		name     string
		recorded []string
		missing  []int
	}{
		{"every step accounted for",
			[]string{"1: read the design", "2: built it", "3: tests are green"}, nil},
		{"the shapes a session writes a number in",
			[]string{"1. read the design", "step 2: built it", "3) tests are green"}, nil},
		{"one step never accounted for",
			[]string{"1: read the design", "2: built it"}, []int{3}},
		{"nothing recorded at all", nil, []int{1, 2, 3}},
		{"work nobody planned, and the plan still unaccounted for",
			[]string{"fixed the linter", "wrote the changelog"}, []int{1, 2, 3}},
		{"a number in the words is not an account",
			[]string{"1: read the design", "built 2 factor authentication", "3: tests are green"}, []int{2}},
	} {
		t.Run(one.name, func(t *testing.T) {
			var recorded []job.Step
			for i, said := range one.recorded {
				recorded = append(recorded, job.Step{Seq: i + 1, Summary: said})
			}
			missing := job.NotAccountedFor(plan, recorded)
			if len(missing) != len(one.missing) {
				t.Fatalf("%d steps are unaccounted for, want %d: %+v", len(missing), len(one.missing), missing)
			}
			for i, want := range one.missing {
				if missing[i].Number != want {
					t.Fatalf("step %d is unaccounted for, want step %d", missing[i].Number, want)
				}
			}
		})
	}
}

// A job with no plan is an errand, and an errand is accountable to nothing.
func TestAJobWithNoPlanIsHeldToNothing(t *testing.T) {
	if missing := job.NotAccountedFor("", nil); missing != nil {
		t.Fatalf("a job with no plan is missing %+v", missing)
	}
}

// The reason has to be actionable without opening the session, so it names the steps rather than
// saying the plan was not followed.
func TestTheReasonNamesTheStepsNothingAccountedFor(t *testing.T) {
	reason := job.PlanNotFollowed([]job.PlanStep{{Number: 3, Text: "write the tests"}})
	for _, phrase := range []string{"step 3", "write the tests"} {
		if !strings.Contains(reason, phrase) {
			t.Fatalf("the reason is %q, want it to say %q", reason, phrase)
		}
	}
}

// The first task asks for the plan and for no work, and it is written from the sentence rather than
// from the brief alone: a plan written from the brief carries whatever misreading the brief carries,
// which is the failure the whole gate exists to catch.
func TestAJobThatOwesAPlanIsAskedForOneAndForNoWork(t *testing.T) {
	asked := job.Asked(&job.Job{
		Title: "the transcript page", Brief: "build what the design describes",
		Product: "you paste a link and get the text back",
		// Past the reading, which is the stage in front of the plan: a job that has not said what it
		// understood is asked for that first, and it has its own tests.
		Ideation: "Understood: a page that takes a link\nNot: a page that takes an identifier\n" +
			"Confidence: fairly sure\nQuestion 1: which surface is this read on",
		IdeationAnswer: "1: on the command line",
		// And past the list, which is the stage after that one.
		Design: "Vertical 1: a person pastes a link and gets the text back\n" +
			"Shown 1: the transcript prints in the terminal",
		DesignAccepted: true,
		// And past the failing tests those requirements became, which is the last stage in front of the
		// plan: a plan is the steps that turn a red suite green.
		Tests: "Requirement 1: a person pastes a link and gets the text back\n" +
			"Ran 1: 12\nFails 1: TestPastingALinkPrintsTheTranscript",
	})
	for _, phrase := range []string{
		"you paste a link and get the text back", "Do no work yet", "Step 1:",
		// And the reading travels with the plan task, marks and all.
		"a page that takes a link", "1: on the command line", "still an assumption",
		// So does the list a person accepted, in the order they accepted it.
		"A person accepted this list", "Vertical 1: a person pastes a link and gets the text back",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
}

// The person who says no writes no plan. What they said goes back with the plan that was refused, so
// the crew writes the next one from the correction.
func TestASessionToldNoIsGivenTheRefusedPlanAndTheCorrection(t *testing.T) {
	asked := job.Asked(&job.Job{
		Brief: "build what the design describes", Product: "you paste a link and get the text back",
		Plan: "Step 1: build the address that takes a video id",
		Told: "a reader pastes a link, so do not make them find an identifier first",
		Ideation: "Understood: a page that takes a link\nNot: a page that takes an identifier\n" +
			"Confidence: fairly sure\nQuestion 1: which surface is this read on",
		IdeationAnswer: "1: on the command line",
	})
	for _, phrase := range []string{
		"was not approved", "Step 1: build the address that takes a video id",
		"a reader pastes a link", "Do no work yet",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
}

// Once the plan is approved the session is given the work, held to the plan, and asked to record
// each step by its number. Two lines about recording steps, saying it two ways, is how a session
// ends up doing neither, so the ordinary line is replaced rather than joined.
func TestAnApprovedJobIsGivenTheWorkAndThePlanItIsHeldTo(t *testing.T) {
	asked := job.Asked(&job.Job{
		Brief: "build what the design describes", Product: "you paste a link and get the text back",
		Plan: "Step 1: read the design\nStep 2: build the address", PlanApproved: true,
	})
	for _, phrase := range []string{
		"build what the design describes", "Step 1: read the design", "record it with its number",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
	if strings.Contains(asked, "Do no work yet") {
		t.Fatalf("a session with an approved plan was told to do no work: %q", asked)
	}
	if strings.Count(asked, "Record each step as you finish it") > 0 {
		t.Fatalf("the session was told twice how to record a step: %q", asked)
	}
}

// A job that states no sentence is an errand. It is asked for the work, the way it always was.
func TestAnErrandIsAskedForTheWork(t *testing.T) {
	asked := job.Asked(&job.Job{Brief: "read the electricity bill and say when it is due"})
	if strings.Contains(asked, "Do no work yet") {
		t.Fatalf("an errand was asked for a plan: %q", asked)
	}
	if !strings.Contains(asked, "read the electricity bill") {
		t.Fatalf("an errand was not given its brief: %q", asked)
	}
}

// The second ask is recognised off the record rather than off a counter, so a controller that took
// the job over from a dead one reads the same history and does not ask a third time.
func TestTheSecondAskForAPlanIsRecognisedInWhatWasSent(t *testing.T) {
	if !job.AskedForThePlanAgain(job.AskedForAPlanTheSystemCanRead("it carried no plan")) {
		t.Fatal("the second ask for a plan is not recognised as one")
	}
	for _, other := range []string{
		job.Asked(&job.Job{Brief: "build it", Product: "paste a link and get the text"}),
		job.Asked(&job.Job{Brief: "read the bill"}),
	} {
		if job.AskedForThePlanAgain(other) {
			t.Fatalf("a first task was read as the second ask: %q", other)
		}
	}
}

// The question is about the product rather than about the code, so it names the sentence and the
// plan and says what each answer does.
func TestTheQuestionNamesTheSentenceAndThePlan(t *testing.T) {
	question := job.AskingWhetherThisIsThePlan(
		"you paste a link and get the text back", "Step 1: read the design")
	for _, phrase := range []string{
		"you paste a link and get the text back", "Step 1: read the design",
		"Answer yes", "you do not have to write it yourself",
	} {
		if !strings.Contains(question, phrase) {
			t.Fatalf("the question is %q, want it to say %q", question, phrase)
		}
	}
	// It is read by a person in a terminal, so it is held to the ceiling every question is held to.
	if _, err := job.TidyQuestion(question); err != nil {
		t.Fatalf("the question the system puts is refused by its own ceiling: %v", err)
	}
}

// The widest plan the ceilings allow still fits in a question, which is what makes the ceilings the
// thing that keeps this readable rather than a second rule nobody checks.
func TestTheWidestPlanStillFitsInAQuestion(t *testing.T) {
	var lines []string
	for i := 1; i <= job.PlanSteps; i++ {
		lines = append(lines, fmt.Sprintf("Step %d: %s", i, strings.Repeat("a", job.PlanStepLimit)))
	}
	question := job.AskingWhetherThisIsThePlan(strings.Repeat("s", job.ProductLimit), strings.Join(lines, "\n"))
	if _, err := job.TidyQuestion(question); err != nil {
		t.Fatalf("the widest plan makes a question the system refuses: %v", err)
	}
}
