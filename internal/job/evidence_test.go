package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// aWorkingLabel is a label that does what a label is for: it says what was running and how to get
// back to it.
const aWorkingLabel = "the console against a running system, started with krewe console"

// The three kinds, each shown the way its vertical asked to be shown. This is the whole widening: a
// picture answers most verticals and it does not answer all of them.
func TestEachKindOfEvidenceShowsAVerticalWorking(t *testing.T) {
	held := []struct {
		name  string
		kind  job.Kind
		shown job.Evidence
	}{
		{
			name:  "a picture",
			kind:  job.KindPicture,
			shown: job.Evidence{Vertical: 1, Kind: job.KindPicture, File: "home.png", Taken: aWorkingLabel},
		},
		{
			name:  "a recording",
			kind:  job.KindRecording,
			shown: job.Evidence{Vertical: 1, Kind: job.KindRecording, File: "run.webm", Taken: aWorkingLabel},
		},
		{
			name: "steps a person runs",
			kind: job.KindSteps,
			shown: job.Evidence{Vertical: 1, Kind: job.KindSteps, Taken: aWorkingLabel, Steps: []string{
				"run krewe job list, and the job is on the listing",
				"press r, and the listing comes back with the row still there",
			}},
		},
	}
	for _, one := range held {
		t.Run(one.name, func(t *testing.T) {
			vertical := job.Requirement{Number: 1, Text: "a person sees the job", Evidence: one.kind}
			if err := job.ShownWorking(vertical, one.shown); err != nil {
				t.Fatalf("%s does not show the vertical working: %v", one.name, err)
			}
		})
	}
}

// The gate. The vertical says which kind it needs, so a vertical asked to be shown one way and shown
// another has not met it, however good the evidence is on its own terms.
func TestAVerticalIsNotShownWithAKindItDidNotAskFor(t *testing.T) {
	steps := []string{
		"run krewe job list, and the job is on the listing",
		"press r, and the listing comes back with the row still there",
	}
	wrong := []struct {
		name   string
		wanted job.Kind
		shown  job.Evidence
		says   string
	}{
		{
			name:   "a picture where the vertical asked for steps",
			wanted: job.KindSteps,
			shown:  job.Evidence{Kind: job.KindPicture, File: "home.png", Taken: aWorkingLabel},
			says:   "asks to be shown with steps and this offers picture",
		},
		{
			name:   "a picture where the vertical asked for a recording",
			wanted: job.KindRecording,
			shown:  job.Evidence{Kind: job.KindPicture, File: "home.png", Taken: aWorkingLabel},
			says:   "asks to be shown with recording and this offers picture",
		},
		{
			name:   "steps where the vertical asked for a picture",
			wanted: job.KindPicture,
			shown:  job.Evidence{Kind: job.KindSteps, Steps: steps, Taken: aWorkingLabel},
			says:   "asks to be shown with picture and this offers steps",
		},
		{
			name:   "a recording where the vertical asked for a picture",
			wanted: job.KindPicture,
			shown:  job.Evidence{Kind: job.KindRecording, File: "run.webm", Taken: aWorkingLabel},
			says:   "asks to be shown with picture and this offers recording",
		},
	}
	for _, one := range wrong {
		t.Run(one.name, func(t *testing.T) {
			err := one.shown.Holds(one.wanted)
			if err == nil {
				t.Fatal("the wrong kind of evidence met the gate")
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
			// And it says which kind was asked for, because a worker told only that its answer is wrong
			// sends the same kind again.
			if !strings.Contains(err.Error(), string(one.wanted)) {
				t.Fatalf("the refusal does not name the kind that was asked for: %v", err)
			}
		})
	}
}

// A vertical that says nothing about the kind is shown with a picture, which is what every vertical
// written before the kinds existed asked for.
func TestAVerticalThatNamesNoKindIsShownWithAPicture(t *testing.T) {
	shown := job.Evidence{File: "home.png", Taken: aWorkingLabel}

	if err := shown.Holds(""); err != nil {
		t.Fatalf("a vertical naming no kind refused a picture: %v", err)
	}
	if job.KindOrPicture("") != job.KindPicture {
		t.Error("a vertical naming no kind does not read as a picture")
	}
	if job.KindOrPicture("something nobody has heard of") != job.KindPicture {
		t.Error("a vertical naming a word nothing knows does not fall back to a picture")
	}
}

// The words somebody actually writes. A worker writes video, a person writes recording, and a request
// says manual steps: refusing any of those teaches nothing except that the system is fussy.
func TestTheWordsSomebodyWritesForEachKind(t *testing.T) {
	said := map[string]job.Kind{
		"picture": job.KindPicture, "a screenshot": job.KindPicture, "still": job.KindPicture,
		"video": job.KindRecording, "a short recording": job.KindRecording,
		"screencast": job.KindRecording, "a gif": job.KindRecording,
		"steps": job.KindSteps, "manual steps": job.KindSteps, "written steps": job.KindSteps,
		"instructions a person follows": job.KindSteps,
	}
	for word, want := range said {
		kind, err := job.ReadKind(word)
		if err != nil {
			t.Errorf("%q names no kind: %v", word, err)
			continue
		}
		if kind != want {
			t.Errorf("%q reads as %s, want %s", word, kind, want)
		}
	}
}

// The sad path where no kind is named at all: a word this does not know is a session asking for a
// fourth kind of evidence, and it is refused with the three written out.
func TestAWordThatNamesNoKindIsRefusedWithTheThree(t *testing.T) {
	_, err := job.ReadKind("a detailed writeup")

	if err == nil {
		t.Fatal("a word that names no kind was taken as one")
	}
	for _, want := range []string{"picture", "recording", "steps"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q as a kind: %v", want, err)
		}
	}
}

// Each kind with nothing under it. A kind is never a way around the evidence: naming steps and
// writing none is the same failure as naming a picture nobody drew.
func TestAKindWithNoEvidenceUnderItShowsNothing(t *testing.T) {
	empty := []struct {
		name  string
		shown job.Evidence
		says  string
	}{
		{
			name:  "a picture that is not there",
			shown: job.Evidence{Kind: job.KindPicture, Taken: aWorkingLabel},
			says:  "nothing shows this vertical working",
		},
		{
			name:  "a recording that is not there",
			shown: job.Evidence{Kind: job.KindRecording, Taken: aWorkingLabel},
			says:  "asked for a recording",
		},
		{
			name:  "steps that were never written",
			shown: job.Evidence{Kind: job.KindSteps, Taken: aWorkingLabel},
			says:  "asked for steps a person can run",
		},
	}
	for _, one := range empty {
		t.Run(one.name, func(t *testing.T) {
			err := one.shown.Shows()
			if err == nil {
				t.Fatal("a kind with nothing under it showed a vertical working")
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}
}

// Every kind carries the label, and this is the half of the rule the widening had to carry across: a
// recording nobody can reproduce and steps nobody can start are worth what an unlabelled screenshot
// is worth.
func TestEveryKindCarriesItsLabel(t *testing.T) {
	unlabelled := []job.Evidence{
		{Kind: job.KindPicture, File: "home.png"},
		{Kind: job.KindRecording, File: "run.webm"},
		{Kind: job.KindSteps, Steps: []string{
			"run krewe job list, and the job is on the listing",
			"press r, and the listing comes back with the row still there",
		}},
	}
	for _, one := range unlabelled {
		t.Run(string(one.Kind), func(t *testing.T) {
			err := one.Shows()
			if err == nil {
				t.Fatal("evidence with no label was accepted")
			}
			if !strings.Contains(err.Error(), "carries no label") {
				t.Fatalf("the refusal is %q, want it to say the label is missing", err)
			}
		})
	}
}

// A label that says the evidence was generated to illustrate is refused by name, whichever kind it
// sits under. A sample presented as a capture is worse than no picture at all.
func TestASampleIsRefusedUnderEveryKind(t *testing.T) {
	sample := "a mockup of the page, drawn with krewe render"
	held := []job.Evidence{
		{Kind: job.KindPicture, File: "home.png", Taken: sample},
		{Kind: job.KindRecording, File: "run.webm", Taken: sample},
		{Kind: job.KindSteps, Taken: sample, Steps: []string{
			"run krewe job list, and the job is on the listing",
			"press r, and the listing comes back with the row still there",
		}},
	}
	for _, one := range held {
		t.Run(string(one.Kind), func(t *testing.T) {
			err := one.Shows()
			if err == nil {
				t.Fatalf("a sample was accepted as %s", one.Kind)
			}
			if !strings.Contains(err.Error(), "generated to illustrate") {
				t.Fatalf("the refusal is %q, want it to name the sample", err)
			}
		})
	}
}

// The kind that will be abused. A paragraph saying the thing works costs nothing to write and reads
// like evidence on the page, so steps are held to the standard the other two are held to.
func TestProseIsNotSteps(t *testing.T) {
	prose := []struct {
		name  string
		steps []string
		says  string
	}{
		{
			name:  "a paragraph wearing a number",
			steps: []string{"The whole flow works correctly and the value arrives for the person."},
			says:  "steps a person follows are at least",
		},
		{
			name: "nothing anybody can do",
			steps: []string{
				"The listing is refreshed and the row is correct.",
				"Everything else behaves as it did before.",
			},
			says: "says nothing anybody can do",
		},
		{
			name: "an instruction that never says what you see",
			steps: []string{
				"run krewe job list",
				"press r",
			},
			says: "never what it looks like when it worked",
		},
		{
			name: "a step that is a paragraph",
			steps: []string{
				"run krewe job list, and " + strings.Repeat("the row says a great deal ", 20),
				"press r, and the listing comes back",
			},
			says: "one line somebody reads with their hand on the keyboard",
		},
	}
	for _, one := range prose {
		t.Run(one.name, func(t *testing.T) {
			shown := job.Evidence{Kind: job.KindSteps, Steps: one.steps, Taken: aWorkingLabel}

			err := shown.Shows()

			if err == nil {
				t.Fatal("prose was accepted as steps a person can follow")
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}
}

// A file that moves is a recording and a file that does not is a picture. The two are asked for when
// different things need showing, so one is not the other.
func TestAFileIsReadAsTheKindItIs(t *testing.T) {
	moving := []string{"run.webm", "run.mp4", "session.cast", "run.mov", "screen.gif"}
	for _, one := range moving {
		if !job.AFileOfKind(job.KindRecording, one) {
			t.Errorf("%q does not read as a recording", one)
		}
		if job.AFileOfKind(job.KindPicture, one) {
			t.Errorf("%q reads as a still picture and it moves", one)
		}
	}
	still := []string{"home.png", "home.jpg", "home.jpeg", "home.webp", "home.svg"}
	for _, one := range still {
		if !job.AFileOfKind(job.KindPicture, one) {
			t.Errorf("%q does not read as a picture", one)
		}
		if job.AFileOfKind(job.KindRecording, one) {
			t.Errorf("%q reads as a recording and it is one frame", one)
		}
	}
	for _, one := range []string{"it works now", "TestPastingALink", "notes.txt", ""} {
		if job.APicture(one) {
			t.Errorf("%q reads as something a person can look at", one)
		}
	}
}

// What a build worker is told, per kind. A worker asked to show something and given no way to show it
// answers with a sentence instead, which is the failure the picture's own instructions exist to stop.
func TestAWorkerIsToldHowToShowEachKind(t *testing.T) {
	said := map[job.Kind][]string{
		job.KindPicture:   {"krewe render", "Picture: the name of a picture", "tmux capture-pane"},
		job.KindRecording: {"krewe record", "Recording: the name of a recording", "no encoder"},
		job.KindSteps:     {"Steps 1: the first thing a person runs", "what they should see", "not steps"},
	}
	for kind, wants := range said {
		t.Run(string(kind), func(t *testing.T) {
			// The whole brief the worker is handed, rather than the half that says how to capture it. The
			// lines it answers in and the way to produce them are one instruction, and a worker given the
			// second without the first answers in a shape nothing reads.
			told := job.BuildTheVertical(
				&job.Job{Workspace: "acme", Product: "a person sees it"},
				job.Requirement{Number: 2, Text: "a person sees it", Shown: "the row", Evidence: kind},
				[]string{"TestItFails"}, job.Opened{},
			)
			for _, want := range wants {
				if !strings.Contains(told, want) {
					t.Errorf("a worker owed a %s is not told %q: %s", kind, want, told)
				}
			}
		})
	}
}

// aReportOf is a build report in the shape a worker answers in, with the evidence lines of one kind.
func aReportOf(vertical int, evidence string) string {
	return "I built it.\n\nVertical: " + itoa(vertical) + "\nRan: 14\nRed: 0\n" +
		"Passing 1: TestItFails\nChanged 1: internal/vertical.go\n" + evidence +
		"\nTaken: " + aWorkingLabel + "\n\nOutcome: proved"
}

// A report is read in the kind it answered with, so the stage knows what it is holding before it puts
// anything in front of a person.
func TestABuildReportIsReadInTheKindItAnsweredWith(t *testing.T) {
	held := []struct {
		name     string
		evidence string
		kind     job.Kind
		file     string
		steps    int
	}{
		{name: "a picture", evidence: "Picture: home.png", kind: job.KindPicture, file: "home.png"},
		{name: "a recording", evidence: "Recording: run.webm", kind: job.KindRecording, file: "run.webm"},
		{
			name: "steps",
			evidence: "Steps 1: run krewe job list, and the job is on the listing\n" +
				"Steps 2: press r, and the listing comes back with the row there",
			kind: job.KindSteps, steps: 2,
		},
		{
			// A worker numbering its own steps writes the singular, and the number on the line is the step
			// rather than the vertical. Both shapes are read, because a worker holds one vertical and says
			// which on a line of its own.
			name: "steps a worker numbered its own way",
			evidence: "Step 1: run krewe job list, and the job is on the listing\n" +
				"Step 2: press r, and the listing comes back with the row there",
			kind: job.KindSteps, steps: 2,
		},
		{
			// A file that moves, sent under the word for a still. What a person does with it is press
			// play, so the kind follows the evidence rather than the word above it.
			name: "a recording named on a picture line", evidence: "Picture: run.mp4",
			kind: job.KindRecording, file: "run.mp4",
		},
	}
	for _, one := range held {
		t.Run(one.name, func(t *testing.T) {
			report, err := job.ReadBuildReport(aReportOf(1, one.evidence), 1)
			if err != nil {
				t.Fatalf("ReadBuildReport: %v", err)
			}
			shown := report.Evidence()
			if shown.Kind != one.kind {
				t.Fatalf("the report reads as %s, want %s", shown.Kind, one.kind)
			}
			if shown.File != one.file {
				t.Errorf("the report holds file %q, want %q", shown.File, one.file)
			}
			if len(shown.Steps) != one.steps {
				t.Errorf("the report holds %d steps, want %d", len(shown.Steps), one.steps)
			}
			if shown.Taken != aWorkingLabel {
				t.Errorf("the label did not survive the reading: %q", shown.Taken)
			}
		})
	}
}

// The record keeps each kind under the vertical it belongs to, which is the whole of what a person is
// shown at the end: by then every worker's sandbox is gone and this row is all there is.
func TestTheRecordKeepsEachKindUnderItsVertical(t *testing.T) {
	wanted := []job.Requirement{
		{Number: 1, Text: "a person sees the listing", Shown: "the rows", Evidence: job.KindPicture},
		{Number: 2, Text: "a person watches it refresh", Shown: "the rows move", Evidence: job.KindRecording},
		{Number: 3, Text: "a person checks the refusal", Shown: "it is refused", Evidence: job.KindSteps},
	}
	reports := map[int]job.BuildReport{}
	for at, evidence := range []string{
		"Picture: home.png",
		"Recording: run.webm",
		"Steps 1: run krewe job list, and the job is on the listing\n" +
			"Steps 2: press r, and the listing comes back with the row there",
	} {
		report, err := job.ReadBuildReport(aReportOf(at+1, evidence), at+1)
		if err != nil {
			t.Fatalf("vertical %d: %v", at+1, err)
		}
		reports[at+1] = report
	}

	kept := job.BuiltText(wanted, reports)

	if len(job.EvidenceIn(kept)) != 3 {
		t.Fatalf("the record of 3 verticals carries evidence for %d", len(job.EvidenceIn(kept)))
	}
	for _, one := range wanted {
		shown := job.EvidenceFor(kept, one.Number)
		if shown.Kind != one.Evidence {
			t.Errorf("vertical %d is kept as %s and it asked for %s", one.Number, shown.Kind, one.Evidence)
		}
		if err := job.ShownWorking(one, shown); err != nil {
			t.Errorf("what is kept for vertical %d does not show it working: %v", one.Number, err)
		}
	}
	// The steps stay in order and stay under their own vertical. Read back wrong, a worker's first step
	// would be filed under vertical 1 and a person would follow somebody else's instructions.
	steps := job.EvidenceFor(kept, 3).Steps
	if len(steps) != 2 || !strings.HasPrefix(steps[0], "run krewe job list") {
		t.Fatalf("vertical 3 is kept with steps %q", steps)
	}
	if len(job.EvidenceFor(kept, 1).Steps) != 0 {
		t.Error("a step written for vertical 3 is filed under vertical 1")
	}
}

// The stage closes on the kind the list asked for, so a report that answers with another does not
// close it, however green the run was.
func TestTheStageDoesNotCloseOnTheWrongKind(t *testing.T) {
	wanted := []job.Requirement{
		{Number: 1, Text: "a person checks the refusal", Shown: "it is refused", Evidence: job.KindSteps},
	}
	failing := map[int][]string{1: {"TestItFails"}}
	report, err := job.ReadBuildReport(aReportOf(1, "Picture: home.png"), 1)
	if err != nil {
		t.Fatalf("ReadBuildReport: %v", err)
	}

	err = job.BuiltGreen(wanted, failing, map[int]job.BuildReport{1: report})

	if err == nil {
		t.Fatal("the build stage closed on a picture where the vertical asked for steps")
	}
	if !strings.Contains(err.Error(), "steps") {
		t.Fatalf("the refusal does not say which kind was asked for: %v", err)
	}
}

// The list a person accepts is where the kind is decided, because the person accepting the list is the
// person who will be looking at the evidence.
func TestAVerticalOnTheListSaysWhichKindItNeeds(t *testing.T) {
	list, err := job.ReadDesign(
		"Vertical 1: a person sees the listing\nShown 1: the rows\n" +
			"Vertical 2: a person watches the listing refresh\nShown 2: the rows move\n" +
			"Evidence 2: a short video\n" +
			"Vertical 3: a person checks the refusal\nShown 3: it is refused\nEvidence 3: manual steps")
	if err != nil {
		t.Fatalf("ReadDesign: %v", err)
	}

	want := []job.Kind{"", job.KindRecording, job.KindSteps}
	for at, one := range list.Verticals {
		if one.Evidence != want[at] {
			t.Errorf("vertical %d asks for %q, want %q", one.Number, one.Evidence, want[at])
		}
	}

	// It survives the record, because the build stage three stages later reads the kind off this row.
	kept := job.DesignText(list)
	for _, one := range job.RequirementsOf(&job.Job{Design: kept, DesignAccepted: true}) {
		if one.Evidence != want[one.Number-1] {
			t.Errorf("vertical %d is kept asking for %q, want %q", one.Number, one.Evidence, want[one.Number-1])
		}
	}
	// And a vertical that says nothing does not spend the line a person reads saying so.
	if strings.Contains(kept, "Evidence 1:") {
		t.Errorf("the record says what vertical 1 asks for and it asked for nothing: %s", kept)
	}
}

// A word on the list that names no kind is refused there, rather than three stages later when a
// worker answers with it.
func TestAListNamingAKindNothingKnowsIsRefused(t *testing.T) {
	_, err := job.ReadDesign(
		"Vertical 1: a person sees the listing\nShown 1: the rows\nEvidence 1: a detailed writeup")

	if err == nil {
		t.Fatal("a list naming a fourth kind of evidence was accepted")
	}
	if !strings.Contains(err.Error(), "vertical 1") {
		t.Fatalf("the refusal does not say which vertical: %v", err)
	}
}

// What a person is actually asked, which is the point of the whole stage. Steps are written out where
// they are read, and a file is named with the kind it is, because "open run.webm" and "follow these"
// are different instructions.
func TestThePersonIsAskedInTheShapeOfEachKind(t *testing.T) {
	wanted := []job.Requirement{
		{Number: 1, Text: "a person sees the listing", Shown: "the rows", Evidence: job.KindPicture},
		{Number: 2, Text: "a person watches it refresh", Shown: "the rows move", Evidence: job.KindRecording},
		{Number: 3, Text: "a person checks the refusal", Shown: "it is refused", Evidence: job.KindSteps},
	}
	reports := map[int]job.BuildReport{}
	for at, evidence := range []string{
		"Picture: home.png",
		"Recording: run.webm",
		"Steps 1: run krewe job list, and the job is on the listing\n" +
			"Steps 2: press r, and the listing comes back with the row there",
	} {
		report, err := job.ReadBuildReport(aReportOf(at+1, evidence), at+1)
		if err != nil {
			t.Fatalf("vertical %d: %v", at+1, err)
		}
		reports[at+1] = report
	}

	asked := job.AcceptWhatWasBuilt(&job.Job{Workspace: "acme"}, wanted, reports)

	for _, want := range []string{
		"picture: home.png",
		"recording: run.webm",
		"steps:",
		"run krewe job list, and the job is on the listing",
		"press r, and the listing comes back with the row there",
		aWorkingLabel,
		"Answer yes and the job is done",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the question does not say %q:\n%s", want, asked)
		}
	}
}
