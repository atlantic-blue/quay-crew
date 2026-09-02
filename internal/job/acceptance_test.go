package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A job is not done until a person has looked at a picture of the thing running.

// aLabel is a label that says what it is: what was running, and the command that drew it.
const aLabel = "the page at http://localhost:3000, drawn with krewe render while the server was up"

// What counts as a picture. It is the name that decides, because the bytes are in a sandbox and this
// reads a report, and the whole point of the field is that a person can open the thing afterwards.
func TestWhatCountsAsAPicture(t *testing.T) {
	for _, named := range []string{
		"home.png", "vertical1.PNG", "run.mp4", "session.cast", "console.webm",
		"paste.jpeg", "flow.gif", "chart.svg", "screen.webp",
	} {
		if !job.APicture(named) {
			t.Fatalf("%q is not read as a picture", named)
		}
	}
	// And the shapes that read like evidence and are not. Each of these is what an answer says when
	// there is no picture: a description, a file nobody can look at, or a test name.
	for _, named := range []string{
		"", "the screenshot in the pull request", "notes.txt", "report.md", "coverage.html",
		"TestPastingALinkPrintsTheTranscript", "home.png and run.mp4", "a picture of the page",
		"pictures/", "home", "output.log",
	} {
		if job.APicture(named) {
			t.Fatalf("%q is read as a picture", named)
		}
	}
}

// The label, which is the other half. A picture nobody can reproduce is worth nothing, so a label
// that does not say how to get it again is refused.
func TestALabelSaysWhereThePictureCameFromAndHowToGetItAgain(t *testing.T) {
	for _, said := range []string{
		aLabel,
		"the console running under tmux, captured with tmux capture-pane and drawn with krewe render",
		"the command line after ./transcript paste, with the api key set",
		"the built page served from /home/agent/shared/site, at 1280x900",
		"curl against localhost:8080 with the stack composed",
	} {
		if err := job.LabelsIt("home.png", said); err != nil {
			t.Fatalf("%q is refused as a label: %v", said, err)
		}
	}

	sad := []struct {
		name  string
		label string
		says  string
	}{
		{
			name:  "no label at all",
			label: "",
			says:  "carries no label",
		},
		{
			name:  "a label that says nothing about how to get it again",
			label: "a picture of the page working",
			says:  "does not say how to get the picture again",
		},
		{
			// The one this exists for. A sample presented as a capture is worse than no picture, because
			// it reads as evidence and is not, and nothing downstream can tell the two apart.
			name:  "a sample generated to illustrate",
			label: "a mockup of what the page would look like, made with krewe render",
			says:  "generated to illustrate",
		},
		{
			name:  "a drawing of how it would look",
			label: "how it would look once the fetch lands, drawn with krewe render",
			says:  "generated to illustrate",
		},
		{
			name:  "a placeholder",
			label: "placeholder picture, run krewe render when the page exists",
			says:  "generated to illustrate",
		},
		{
			name: "a label longer than the line a person reads",
			label: strings.Repeat("the page at http://localhost:3000 drawn with krewe render, ", 8) +
				"and then some",
			says: "the ceiling is",
		},
	}
	for _, one := range sad {
		t.Run(one.name, func(t *testing.T) {
			err := job.LabelsIt("home.png", one.label)
			if err == nil {
				t.Fatalf("%q was read as a label", one.label)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
			// Every refusal names the file, because a person reading it has to know which picture is
			// being refused when several verticals landed at once.
			if !strings.Contains(err.Error(), "home.png") {
				t.Fatalf("the refusal does not name the file: %v", err)
			}
		})
	}
}

// The gate itself, on one vertical. The three checks above it are the machine reading its own work,
// and this is the one it cannot make for itself.
func TestAVerticalIsNotBuiltUntilSomethingShowsItWorking(t *testing.T) {
	vertical := job.Requirement{Number: 2, Text: "a person pastes a link", Shown: "the text"}

	if err := job.ShownWorking(vertical, job.Picture{
		Vertical: 2, File: "paste.png", Taken: aLabel,
	}); err != nil {
		t.Fatalf("a vertical with a picture and a label is refused: %v", err)
	}

	sad := []struct {
		name string
		shot job.Picture
		says string
	}{
		{
			name: "no picture",
			shot: job.Picture{Vertical: 2, Taken: aLabel},
			says: "nothing shows this vertical working",
		},
		{
			name: "a picture with no label",
			shot: job.Picture{Vertical: 2, File: "paste.png"},
			says: "carries no label",
		},
		{
			name: "a description where a picture should be",
			shot: job.Picture{Vertical: 2, File: "it works now", Taken: aLabel},
			says: "is not a picture",
		},
		{
			name: "a passing test named after the vertical",
			shot: job.Picture{Vertical: 2, File: "TestPastingALink", Taken: aLabel},
			says: "is not a picture",
		},
	}
	for _, one := range sad {
		t.Run(one.name, func(t *testing.T) {
			err := job.ShownWorking(vertical, one.shot)
			if err == nil {
				t.Fatalf("%+v was accepted as showing a vertical working", one.shot)
			}
			for _, phrase := range []string{"vertical 2", vertical.Text, one.says} {
				if !strings.Contains(err.Error(), phrase) {
					t.Fatalf("the refusal is %q, want it to say %q", err, phrase)
				}
			}
		})
	}
}

// A build report carries its picture, and the stage refuses one that does not. This is the same
// refusal as above, arriving through the road every worker's answer takes.
func TestABuildReportWithoutAPictureIsNotABuild(t *testing.T) {
	green := "Vertical: 1\nRan: 14\nRed: 0\nPassing 1: TestIt\nChanged 1: main.go\n"

	report, err := job.ReadBuildReport(green + "Picture: home.png\nTaken: " + aLabel)
	if err != nil {
		t.Fatalf("a report with a picture is refused: %v", err)
	}
	if report.Picture != "home.png" || report.Taken != aLabel {
		t.Fatalf("the report carries picture %q and label %q", report.Picture, report.Taken)
	}

	if _, err := job.ReadBuildReport(green); err == nil {
		t.Fatal("a report with no picture was read as a build")
	} else if !strings.Contains(err.Error(), "nothing shows this vertical working") {
		t.Fatalf("the refusal is %q", err)
	}

	if _, err := job.ReadBuildReport(green + "Picture: home.png"); err == nil {
		t.Fatal("a picture with no label was read as a build")
	} else if !strings.Contains(err.Error(), "carries no label") {
		t.Fatalf("the refusal is %q", err)
	}

	// A path is kept as a name. The picture goes in the workspace's shared folder, which a session
	// sees at one path and a person sees at another, so the file is what travels.
	pathed, err := job.ReadBuildReport(green + "Picture: /home/agent/shared/home.png\nTaken: " + aLabel)
	if err != nil {
		t.Fatalf("a report naming the picture by its path is refused: %v", err)
	}
	if pathed.Picture != "home.png" {
		t.Fatalf("the picture is kept as %q, want the name alone", pathed.Picture)
	}
}

// The record keeps the picture under the vertical it shows, because that is what a person is shown
// once every sandbox that made one is gone.
func TestTheRecordKeepsEachPictureUnderItsVertical(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	reports := map[int]job.BuildReport{}
	for _, vertical := range wanted {
		report, err := job.ReadBuildReport(aBuildReport(vertical.Number, theFailures[vertical.Number]))
		if err != nil {
			t.Fatalf("vertical %d: %v", vertical.Number, err)
		}
		reports[vertical.Number] = report
	}

	kept := job.BuiltText(wanted, reports)
	shots := job.PicturesIn(kept)
	if len(shots) != len(wanted) {
		t.Fatalf("the record of %d verticals carries %d pictures", len(wanted), len(shots))
	}
	for _, vertical := range wanted {
		shot := job.PictureOf(kept, vertical.Number)
		if shot.File == "" || shot.Taken == "" {
			t.Fatalf("vertical %d is in the record with picture %q and label %q",
				vertical.Number, shot.File, shot.Taken)
		}
		if err := shot.Shows(); err != nil {
			t.Fatalf("the picture kept for vertical %d does not show it working: %v", vertical.Number, err)
		}
	}
	// Each under its own number. Read back wrong, one vertical would be covered twice and another not
	// at all, and a person would accept a picture of something else.
	if job.PictureOf(kept, 1).File == job.PictureOf(kept, 2).File {
		t.Fatal("two verticals are recorded against one picture")
	}
	if job.PictureOf(kept, 9).File != "" {
		t.Fatal("a vertical nobody built has a picture")
	}
}

// The word that lands a job. It is the plan's word and the list's word, deliberately, because three
// gates with three words for yes teaches somebody three habits and punishes two of them.
func TestOnlyOneWordAcceptsWhatWasBuilt(t *testing.T) {
	for _, said := range []string{"yes", "Yes", " yes ", "YES"} {
		if !job.AcceptsWhatWasBuilt(said) {
			t.Fatalf("%q does not accept the build", said)
		}
	}
	for _, said := range []string{
		"", "no", "yes but the second one is empty", "looks good", "ship it",
		"the value arrived", "accepted",
	} {
		if job.AcceptsWhatWasBuilt(said) {
			t.Fatalf("%q accepts the build, and it is not the word", said)
		}
	}
}

// The gate on the ordinary settling road. A job whose verticals are built reaches done one way, and
// this is what says so.
func TestAJobWhoseVerticalsAreBuiltCannotSettleOnItsOwnAnswer(t *testing.T) {
	built := buildingJob()
	built.Build = "Vertical 1: a person pastes a link\nRan 1: 14\nPasses 1: TestIt"
	if !job.NotYetAccepted(built) {
		t.Fatal("a built job nobody accepted can settle on its own answer")
	}

	accepted := *built
	accepted.Accepted = true
	if job.NotYetAccepted(&accepted) {
		t.Fatal("a job a person accepted is still held at the gate")
	}

	// A job that never reached the build stage is not held here. Every errand and every job declared
	// before these stages existed settles the way it always did.
	unbuilt := buildingJob()
	if job.NotYetAccepted(unbuilt) {
		t.Fatal("a job with nothing built is held at the acceptance gate")
	}

	// And a worker is not. The verticals are its parent's, and one part of a plan a person already
	// approved does not get its own acceptance: they would be accepting the same work twice.
	worker := *built
	worker.Parent = "the-job-above"
	if job.NotYetAccepted(&worker) {
		t.Fatal("a build worker is held at its parent's acceptance gate")
	}
}

// What a person is shown. The question carries the picture, where to open it and what it took to
// draw, because a question that says "look at it" with nothing to look at is the failure this stage
// exists to end.
func TestTheQuestionCarriesThePicturesAndWhereToOpenThem(t *testing.T) {
	one := buildingJob()
	wanted := job.RequirementsOf(one)
	reports := map[int]job.BuildReport{}
	for _, vertical := range wanted {
		report, err := job.ReadBuildReport(aBuildReport(vertical.Number, theFailures[vertical.Number]))
		if err != nil {
			t.Fatalf("vertical %d: %v", vertical.Number, err)
		}
		reports[vertical.Number] = report
	}

	asked := job.AcceptWhatWasBuilt(one, wanted, reports)
	if !job.AskedToAccept(asked) {
		t.Fatal("the question about a finished build is not recognised as the acceptance ask")
	}
	for _, phrase := range []string{
		"vertical1.png", "vertical2.png", "krewe render", "krewe where " + one.Workspace,
		"shared folder", "Answer yes",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the question does not say %q:\n%s", phrase, asked)
		}
	}
	for _, vertical := range wanted {
		if !strings.Contains(asked, vertical.Shown) {
			t.Fatalf("the question does not say what vertical %d shows", vertical.Number)
		}
	}
}

// What a build run is asked says how to make a picture, including for a product with no page. A run
// asked for a picture with no way to make one answers with a sentence instead.
func TestABuildRunIsToldHowToShowItsVerticalWorking(t *testing.T) {
	one := buildingJob()
	vertical := job.RequirementsOf(one)[0]
	asked := job.BuildTheVertical(one, vertical, job.FailuresOn(one.Tests)[vertical.Number])
	for _, phrase := range []string{
		"it has to be a picture", "Not a description of it working",
		"krewe render", "tmux capture-pane", "/home/agent/shared", "Then label it",
		"Picture:", "Taken:",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("what the run is asked does not say %q:\n%s", phrase, asked)
		}
	}
}
