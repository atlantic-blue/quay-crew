package job_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
)

// What makes a proposed list a list, and the one shape that has to be refused: a list of plumbing.

// aList is what a session answers with when it has read the work: two things a person gets, each
// naming what that person is shown.
const aList = "Here is what I would build.\n\n" +
	"Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Shown 1: the transcript prints in the terminal for a link the person chooses\n" +
	"Vertical 2: a person opens the same transcript in a browser and shares the address\n" +
	"Shown 2: the page renders that transcript at an address the person can send on"

func TestAListOfVerticalsIsReadOffTheReply(t *testing.T) {
	read, err := job.ReadDesign(aList)
	if err != nil {
		t.Fatalf("ReadDesign: %v", err)
	}
	if len(read.Verticals) != 2 {
		t.Fatalf("the list carries %d verticals, want 2", len(read.Verticals))
	}
	if read.Verticals[0].Text != "a person pastes a link on the command line and gets the text back" {
		t.Fatalf("the first vertical is %q", read.Verticals[0].Text)
	}
	if read.Verticals[1].Shown == "" {
		t.Fatalf("the second vertical says nothing about what a person is shown")
	}
	// Kept as the system's own rendering, so what a person accepts and what the session writing the
	// plan is handed are the same lines. The prose around the list does not survive.
	kept := job.DesignText(read)
	if strings.Contains(kept, "Here is what I would build") {
		t.Fatalf("the record kept the reasoning around the list: %q", kept)
	}
	if !strings.HasPrefix(kept, "Vertical 1: ") || !strings.Contains(kept, "\nShown 2: ") {
		t.Fatalf("the record reads back as %q", kept)
	}
	// And it reads back into the same list, which is what lets the plan task and the surfaces read a
	// kept row without a second shape.
	if again := job.DesignIn(kept); len(again.Verticals) != 2 {
		t.Fatalf("the kept record reads back as %d verticals", len(again.Verticals))
	}
}

// The rule that decides whether a list is a list. A database is not a deliverable and nor is a piece
// of infrastructure, so a list made of them is one vertical with its plumbing inside it.
func TestAListOfPlumbingIsRefused(t *testing.T) {
	list := "Vertical 1: the jobs table gains a design column and a migration\n" +
		"Shown 1: the column is written and read in both stores\n" +
		"Vertical 2: a queue carries the proposal to the controller\n" +
		"Shown 2: the topic exists and the consumer is subscribed\n" +
		"Vertical 3: a role is added for the session that writes the list\n" +
		"Shown 3: the role directory holds the model and the brief"

	_, err := job.ReadDesign(list)
	if err == nil {
		t.Fatal("a list of a schema, a queue and a role was accepted as three verticals")
	}
	for _, phrase := range []string{
		"one vertical with its plumbing inside it",
		"required work towards", "name the person",
	} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("the refusal is %q, want it to say %q", err, phrase)
		}
	}
	// And it names the line and the word, so the session knows which line to write again rather than
	// guessing at the whole list.
	for _, phrase := range []string{"vertical 1", "table", "vertical 2", "queue", "vertical 3", "role"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("the refusal is %q, want it to name %q", err, phrase)
		}
	}
}

// One plumbing line among real ones is refused too, and the refusal says to fold it in rather than
// saying the whole list is wrong.
func TestOnePlumbingLineAmongVerticalsIsRefused(t *testing.T) {
	list := "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
		"Shown 1: the transcript prints in the terminal\n" +
		"Vertical 2: the transcripts are stored in a database with an index on the link\n" +
		"Shown 2: the schema is applied and the index exists"

	_, err := job.ReadDesign(list)
	if err == nil {
		t.Fatal("a database was accepted as a vertical of its own")
	}
	if !strings.Contains(err.Error(), "fold it into the vertical it serves") {
		t.Fatalf("the refusal is %q", err)
	}
	if strings.Contains(err.Error(), "one vertical with its plumbing inside it") {
		t.Fatalf("a list with one plumbing line is refused as though it were all plumbing: %q", err)
	}
}

// The same infrastructure with the person it serves named is a vertical, because then the line says
// who is shown the thing working. This is the direction the rule can be wrong in, and it is the
// direction chosen: a refusal costs a task, and a line naming a person is a line somebody can check.
func TestPlumbingThatNamesThePersonItServesIsAVertical(t *testing.T) {
	list := "Vertical 1: a person reads every past transcript from the store without waiting\n" +
		"Shown 1: the person types the command and the list comes back in under a second"

	if _, err := job.ReadDesign(list); err != nil {
		t.Fatalf("a line naming the person it serves was refused: %v", err)
	}
	if word := job.OnlyPlumbing("the queue carries the proposal"); word != "queue" {
		t.Fatalf("a line naming nobody reads as %q", word)
	}
	if word := job.OnlyPlumbing("a person reads it off the queue"); word != "" {
		t.Fatalf("a line naming a person reads as plumbing, %q", word)
	}
}

// A list of one is a list. The rule folds plumbing into the vertical it serves, so one deliverable
// with everything under it is the ordinary outcome of the rule rather than a mistake.
func TestAListOfOneIsAccepted(t *testing.T) {
	read, err := job.ReadDesign("Vertical 1: a person pastes a link and gets the text back\n" +
		"Shown 1: the transcript prints in the terminal")
	if err != nil {
		t.Fatalf("a list of one vertical was refused: %v", err)
	}
	if len(read.Verticals) != 1 {
		t.Fatalf("the list carries %d verticals", len(read.Verticals))
	}
}

// An empty list is refused, and the refusal teaches the shape rather than saying no.
func TestAReplyWithNoListIsRefused(t *testing.T) {
	_, err := job.ReadDesign("I have read the brief and I know what to build.")
	if err == nil {
		t.Fatal("prose about building was read as a list")
	}
	for _, phrase := range []string{"Vertical 1:", "Shown 1:"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("the refusal is %q, want it to say %q", err, phrase)
		}
	}
}

// A vertical nobody can be shown is not a vertical, so the shown line is required rather than
// optional. It is the half that says a person gets something.
func TestAVerticalWithNothingShownIsRefused(t *testing.T) {
	_, err := job.ReadDesign("Vertical 1: a person pastes a link and gets the text back")
	if err == nil {
		t.Fatal("a vertical with nothing shown was accepted")
	}
	if !strings.Contains(err.Error(), "Shown 1:") {
		t.Fatalf("the refusal is %q", err)
	}
}

func TestAListIsHeldToItsCeilings(t *testing.T) {
	long := strings.Repeat("a", job.DesignLineLimit+1)
	for _, one := range []struct {
		name, list, says string
	}{
		{
			"more verticals than a person reads",
			manyVerticals(job.DesignVerticals + 1),
			"it may have 7",
		},
		{
			"numbered with one missing",
			"Vertical 1: a person pastes a link and gets the text back\nShown 1: the terminal prints it\n" +
				"Vertical 3: a person shares the address with somebody\nShown 3: the page renders it",
			"number the verticals from 1 upwards",
		},
		{
			"a line longer than a line",
			"Vertical 1: a person " + long + "\nShown 1: the terminal prints it",
			"it is one line a person reads",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := job.ReadDesign(one.list)
			if err == nil {
				t.Fatalf("the list was accepted")
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}
}

// manyVerticals is a list of n verticals, each one a real deliverable, for the ceiling above.
func manyVerticals(n int) string {
	var lines []string
	for i := 1; i <= n; i++ {
		lines = append(lines,
			"Vertical "+itoa(i)+": a person reads the transcript on surface "+itoa(i),
			"Shown "+itoa(i)+": the person opens surface "+itoa(i)+" and the text is there")
	}
	return strings.Join(lines, "\n")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// The mark that survives onto the record. A vertical the person put on the list stays theirs, which
// is the same mark ideation makes between what a session was told and what it filled in itself.
func TestAVerticalThePersonPutOnTheListStaysTheirs(t *testing.T) {
	read, err := job.ReadDesign(
		"Vertical 1: a person pastes a link and gets the text back\n" +
			"Shown 1: the transcript prints in the terminal\n" +
			"Yours 2: a person exports the transcript as a file they keep\n" +
			"Shown 2: the file lands in the folder the person chose")
	if err != nil {
		t.Fatalf("ReadDesign: %v", err)
	}
	if read.Verticals[0].Yours {
		t.Fatalf("a vertical the crew proposed reads as the person's")
	}
	if !read.Verticals[1].Yours {
		t.Fatalf("a vertical the person put on the list does not read as theirs")
	}
	kept := job.DesignText(read)
	if !strings.Contains(kept, "Yours 2: a person exports") {
		t.Fatalf("the mark did not survive onto the record: %q", kept)
	}
	// And it travels into the plan task, so a plan that dropped it dropped the thing they asked for.
	said := job.WhatWeWouldBuild(&job.Job{Design: kept, DesignAccepted: true})
	for _, phrase := range []string{"A person accepted this list", "Yours 2:", "Carry it as theirs"} {
		if !strings.Contains(said, phrase) {
			t.Fatalf("the plan task says %q, want it to say %q", said, phrase)
		}
	}
}

// A list nobody accepted reaches no plan task. The flag is the difference, which is why the list has
// one and the reading does not.
func TestAnUnacceptedListDoesNotReachThePlan(t *testing.T) {
	if said := job.WhatWeWouldBuild(&job.Job{Design: aList}); said != "" {
		t.Fatalf("a list nobody accepted is handed to the plan: %q", said)
	}
}

// The gates in order, and what each one leaves the job owing. This is the way off the old order,
// where an answered reading owed a plan: it now owes the list, and the plan comes after.
func TestTheListStandsBetweenTheReadingAndThePlan(t *testing.T) {
	one := &job.Job{
		Product: "you paste a link and get the text back",
		Brief:   "build what the design describes",
	}
	if job.WaitingForItsDesign(one) {
		t.Fatal("a job that has not said what it understood owes a list")
	}
	one.Ideation, one.IdeationAnswer = "Understood: a page", "1: on the command line"
	if !job.WaitingForItsDesign(one) || job.WaitingForItsPlan(one) {
		t.Fatal("an answered reading does not owe the list, or owes a plan already")
	}
	// The task it is given is the ask for the list, and it carries what a person agreed with.
	asked := job.Asked(one)
	for _, phrase := range []string{job.TheDesignAsk, "Vertical 1:", "Shown 1:", "1: on the command line"} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the list task is %q, want it to say %q", asked, phrase)
		}
	}
	if strings.Contains(asked, "Step 1:") {
		t.Fatalf("a session that owes a list was asked for a plan: %q", asked)
	}

	// Sent back, and the second ask carries the list that was refused and what the person said.
	one.Design, one.Told = job.DesignText(job.DesignIn(aList)), "the browser one is not needed"
	again := job.Asked(one)
	for _, phrase := range []string{"was not accepted", "the browser one is not needed", "Yours 2:"} {
		if !strings.Contains(again, phrase) {
			t.Fatalf("the second list task is %q, want it to say %q", again, phrase)
		}
	}

	one.Told, one.DesignAccepted = "", true
	if job.WaitingForItsDesign(one) || !job.WaitingForItsTests(one) {
		t.Fatal("an accepted list still owes a list, or owes no failing tests")
	}
	// And the plan comes after those tests, never before them.
	one.Tests = "Requirement 1: a person pastes a link\nRan 1: 12\nFails 1: TestItFails"
	if !job.WaitingForItsPlan(one) {
		t.Fatal("a job whose suite is red owes no plan")
	}
}

// An errand and a job under another never reach this gate, for the reasons the two gates around it
// already keep: an errand has nothing to build a list against, and a step of a flow run follows the
// graph a person imported.
func TestAnErrandAndAStepOfARunAreNeverAskedForAList(t *testing.T) {
	errand := &job.Job{Title: "read the electricity bill", IdeationAnswer: "1: yes"}
	if job.WaitingForItsDesign(errand) {
		t.Fatal("an errand is asked what it would build")
	}
	step := &job.Job{
		Product: "you paste a link and get the text back", Run: "a-run",
		IdeationAnswer: "1: on the command line",
	}
	if job.WaitingForItsDesign(step) {
		t.Fatal("a step of a flow run is asked what it would build")
	}
}

// The acceptance is the plan's word, and one word only. Anything else is the correction the session
// writes the next list from.
func TestOnlyTheOneWordAcceptsTheList(t *testing.T) {
	for _, said := range []string{"yes", "Yes", " yes "} {
		if !job.AcceptsTheList(said) {
			t.Fatalf("%q does not accept the list", said)
		}
	}
	for _, said := range []string{"yes, but drop the second one", "no", "", "sure"} {
		if job.AcceptsTheList(said) {
			t.Fatalf("%q was read as accepting the list", said)
		}
	}
}

// The double and the reader have to agree, because a double looser than the engine manufactures a
// green suite: every test about a job past its reading runs through this reply.
func TestTheDoubleAnswersAListTheSystemCanRead(t *testing.T) {
	if model.DesignAsk != job.TheDesignAsk {
		t.Fatalf("the double watches for %q and the system asks with %q",
			model.DesignAsk, job.TheDesignAsk)
	}
	if model.DesignMarker != job.DesignMarker {
		t.Fatalf("the double marks a list with %q and the system reads %q",
			model.DesignMarker, job.DesignMarker)
	}
	if _, err := job.ReadDesign(model.FakeDesign); err != nil {
		t.Fatalf("the double answers a list the system refuses: %v", err)
	}
}
