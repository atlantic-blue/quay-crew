package job

import (
	"fmt"
	"regexp"
	"strings"
)

// Evidence is what a person looks at before they say the value arrived, and there are three kinds of
// it.
//
// A picture was the first, and it is the one that shipped. It answers most verticals, because most of
// what this system builds is something you can point a camera at: a page, a listing, a terminal that
// prints an answer.
//
// It does not answer all of them. A list that refreshes, a wizard that comes back to where it
// started, a key that is swallowed: a still frame of any of those shows a screen that looks right,
// and the failure is in what happened between two frames. That is a recording.
//
// And some value no capture can show at all. A refusal that arrives at the right moment, a migration
// that is reversible, a permission that is denied to the account it should be denied to: what proves
// those is a person doing them, so the evidence is the steps they follow. This is the kind that will
// be abused, because a paragraph saying the thing works costs nothing to write and reads like
// evidence on the page. So it is held to the standard the other two are held to: each step is
// something somebody can actually run or press, and it says what they should see.
//
// The vertical says which kind it needs, and the stage holds for that kind. A vertical that says
// nothing gets a picture, because a picture is the strongest of the three and it is what every
// vertical written before this got.
//
// Every kind carries the label. Where the evidence came from and what it takes to reproduce it: a
// kind is never a way around the evidence, and steps nobody can run are the same failure as a
// screenshot nobody can reproduce.

// Kind is which of the three a vertical is shown with.
type Kind string

const (
	// KindPicture is a still of the built thing running.
	KindPicture Kind = "picture"
	// KindRecording is a moving picture of it running, for value a still frame cannot carry.
	KindRecording Kind = "recording"
	// KindSteps is what a person runs or presses to see it for themselves, for value no capture shows.
	KindSteps Kind = "steps"
)

// TheKinds are the three, in the order a vertical should reach for them: the strongest evidence
// first, and the one a session can write without running anything last.
var TheKinds = []Kind{KindPicture, KindRecording, KindSteps}

// StepsAtLeast is how many steps are steps. One line is a sentence about the work, and the thing this
// kind has to refuse is a paragraph wearing a number.
const StepsAtLeast = 2

// StepsLineLimit is how long one step may be. It is the line a person reads with their hand on the
// keyboard, and it is the ceiling every other one line field on this row already has.
const StepsLineLimit = TitleLimit

// EvidenceLimit is how long a label may be, and it is the picture's ceiling under a name that covers
// all three kinds.
const EvidenceLimit = TitleLimit

// PictureLimit is what the label's ceiling was called when a picture was the only kind.
const PictureLimit = EvidenceLimit

// theKindSaid is what somebody writes when they mean each kind. A worker writes video and a person
// writes recording, and refusing one of those two teaches nothing except that the system is fussy.
var theKindSaid = map[string]Kind{
	"picture": KindPicture, "pictures": KindPicture, "screenshot": KindPicture, "shot": KindPicture,
	"still": KindPicture, "image": KindPicture, "photo": KindPicture, "capture": KindPicture,

	"recording": KindRecording, "record": KindRecording, "video": KindRecording, "movie": KindRecording,
	"screencast": KindRecording, "cast": KindRecording, "film": KindRecording, "clip": KindRecording,
	"gif": KindRecording,

	"steps": KindSteps, "step": KindSteps, "manual": KindSteps, "instructions": KindSteps,
	"walkthrough": KindSteps, "checklist": KindSteps, "written": KindSteps, "verify": KindSteps,
}

// ReadKind is the kind a word names, and the refusal where it names none.
//
// It reads the last word it recognises rather than the first, so "manual steps" and "a short video"
// both land, and a line that says nothing this knows is refused by name with the three written out.
func ReadKind(said string) (Kind, error) {
	said = TidySentence(said)
	found := Kind("")
	for _, word := range strings.Fields(strings.ToLower(said)) {
		word = strings.Trim(word, ".,:;\"'()")
		if kind, held := theKindSaid[word]; held {
			found = kind
		}
	}
	if found == "" {
		return "", fmt.Errorf("%q does not name a kind of evidence: a vertical is shown with a "+
			"picture, a recording or steps a person can run", said)
	}
	return found, nil
}

// KindOrPicture is the kind a word names, and a picture where it names none.
//
// The default is not a guess. A vertical that says nothing about the kind is shown the way every
// vertical before this one was, and a picture is the strongest of the three: it is a capture of the
// thing running rather than an account of it.
func KindOrPicture(said string) Kind {
	kind, err := ReadKind(said)
	if err != nil {
		return KindPicture
	}
	return kind
}

// aStill is a file that is one frame: what a screenshot is written into.
var aStill = regexp.MustCompile(`(?i)\.(png|jpe?g|webp|svg|avif|apng|bmp|tiff?)$`)

// aMovingPicture is a file that moves: a video, an animation, or a recording of a terminal that plays
// back. A recording of a screen is in here twice for the reason a picture of one is: a product with
// no page still has to be shown working.
var aMovingPicture = regexp.MustCompile(`(?i)\.(mp4|webm|mov|m4v|mkv|gif|gifv|cast|avi)$`)

// aRenderedSample is a label admitting the evidence was generated to illustrate rather than captured
// from something running. Each of these is a word somebody writes when they are being honest about a
// mock up, so the refusal takes them at their word and says what to send instead.
//
// The list is deliberately short and concrete, the way the design stage's list of plumbing is. A long
// list guesses at what somebody might write, and a refusal here costs a task and a person's patience.
var aRenderedSample = regexp.MustCompile(`(?i)\b(mock ?up|mocked|mock|wireframe|placeholder|` +
	`illustrative|illustration|for illustration|hand ?drawn|drawn by hand|figma|sketch|` +
	`what it would look like|how it would look|as it would|would look|artist)\b`)

// reproduces is the shape of a label that says how to get the evidence again: a command, an address,
// or a path. One of the three has to be in there, because a label with none of them is a sentence
// about the evidence rather than a way back to it.
var reproduces = regexp.MustCompile(`(?i)(^|[\s"'` + "`" + `(])(krewe |quay |make |go |npm |npx |yarn |pnpm |` +
	`docker |terraform |cargo |python |node |curl |tmux |git |bash |sh |psql |aws |\./|/[a-z0-9._-]+/|` +
	`https?://|localhost[:/]|127\.0\.0\.1)`)

// pressed is something a person does with their hands. A step is either a thing they run, which
// reproduces above already recognises, or a thing they press.
var pressed = regexp.MustCompile(`(?i)\b(press|presses|click|clicks|type|types|open|opens|select|` +
	`selects|choose|chooses|tap|taps|enter|enters|scroll|scrolls|hover|drag|paste|pastes|visit|` +
	`answer|answers|sign in|log in|switch|switches|filter|filters|stop|start|restart)\b`)

// seen is the half of a step that says what should happen. A step that says what to do and never what
// it looks like when it worked is an instruction rather than a check, and a person following it
// cannot tell a pass from a failure.
var seen = regexp.MustCompile(`(?i)\b(see|sees|seen|show|shows|shown|print|prints|printed|say|says|` +
	`appear|appears|read|reads|list|lists|listed|return|returns|display|displays|come back|comes back|` +
	`get|gets|render|renders|name|names|carry|carries|hold|holds|refuse|refuses|land|lands|` +
	`is there|are there|no longer|nothing|empty|green|red)\b`)

// Evidence is what one vertical is shown with, and the label that says where it came from.
type Evidence struct {
	// Vertical is the vertical this evidence shows working.
	Vertical int
	// Kind is which of the three this is. It is read off what arrived rather than declared twice: a
	// file that moves is a recording, a file that does not is a picture, and steps are steps.
	Kind Kind
	// File is the name of the picture or the recording, in the workspace's shared folder, so a person
	// on the machine can open it after the sandbox that made it is gone. Empty on steps.
	File string
	// Steps are what a person runs or presses to see it themselves, in order. Empty on a capture.
	Steps []string
	// Taken is the label: where the evidence came from and what it takes to get it again.
	Taken string
}

// APicture says whether this name is something a person can open and look at, still or moving.
//
// It is the name that is checked and not the bytes, because the bytes are in a sandbox and this reads
// a report. What it refuses is the answer that says "the screenshot in the pull request" or names a
// text file: neither is something a person can open and look at.
func APicture(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t") {
		return false
	}
	return aStill.MatchString(name) || aMovingPicture.MatchString(name)
}

// AFileOfKind says whether this name is a file of that kind. A still is not a recording and a
// recording is not a still, because the two are asked for when different things need showing.
func AFileOfKind(kind Kind, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t") {
		return false
	}
	switch kind {
	case KindRecording:
		return aMovingPicture.MatchString(name)
	default:
		return aStill.MatchString(name)
	}
}

// LabelsIt is the refusal where a label does not say where the evidence came from, and nil where it
// does.
//
// Two things make a label. It says the evidence came from something running, and it says what to type
// to get it again. Evidence nobody can reproduce is worth nothing, and a generated sample presented
// as a capture is worse than no evidence at all, so the second is refused by name rather than left to
// a reader to notice.
func LabelsIt(what, taken string) error {
	taken = TidySentence(taken)
	if taken == "" {
		return fmt.Errorf("%s carries no label, so nobody can tell what it is evidence of: write a "+
			"%q line saying what was running, the command that produced it, and what has to be up to "+
			"produce it again", what, "Taken 1:")
	}
	if len(taken) > EvidenceLimit {
		return fmt.Errorf("the label on %s is %d characters and the ceiling is %d: say what was "+
			"running and the command behind it, in one line", what, len(taken), EvidenceLimit)
	}
	if found := aRenderedSample.FindString(taken); found != "" {
		return fmt.Errorf("the label on %s says %q, so this was generated to illustrate rather than "+
			"captured from the thing running: start it, capture what it does, and label that. A sample "+
			"presented as a capture is worse than no picture at all", what, strings.ToLower(found))
	}
	if !reproduces.MatchString(taken) {
		return fmt.Errorf("the label on %s does not say how to get it again: name the command, the "+
			"address or the path in it, so a reader can reproduce what you are showing them", what)
	}
	return nil
}

// Shows is the refusal where this evidence does not show anything working, and nil where it does.
//
// It names no vertical, because every caller of it is already writing the vertical into the sentence
// it wraps this in, and the same refusal arriving with the vertical in it twice reads as two
// problems.
func (e Evidence) Shows() error {
	switch e.kind() {
	case KindSteps:
		return e.stepsHold()
	case KindRecording:
		if e.File == "" {
			return fmt.Errorf("nothing shows this vertical working. This vertical asked for a recording: "+
				"run what you built, capture the screen while it runs, join the captures with krewe "+
				"record, and name it on a %q line", "Recording: run.webm")
		}
		if !AFileOfKind(KindRecording, e.File) {
			return fmt.Errorf("%q is not a recording: this vertical asked to be shown something moving, "+
				"so answer with the name of one, with no spaces in it, such as %s",
				e.File, "run.webm, run.mp4 or session.cast")
		}
		return LabelsIt(e.File, e.Taken)
	default:
		if e.File == "" {
			return fmt.Errorf("nothing shows this vertical working. Run what you built, capture what it "+
				"does, put the picture in the shared folder and name it on a %q line. A passing test named "+
				"after a vertical is not a picture of it", "Picture: home.png")
		}
		if !AFileOfKind(KindPicture, e.File) {
			if AFileOfKind(KindRecording, e.File) {
				return fmt.Errorf("%q moves, and this vertical asked for a picture of it: draw one frame "+
					"of what you built with krewe render and name it on a %q line", e.File, "Picture: home.png")
			}
			return fmt.Errorf("%q is not a picture: answer with the name of one still, with no spaces in "+
				"the name, such as %s", e.File, "home.png")
		}
		return LabelsIt(e.File, e.Taken)
	}
}

// stepsHold is the standard written steps are held to, and it is the same standard as the other two
// kinds: something a person can actually do, and a way to tell whether it worked.
func (e Evidence) stepsHold() error {
	if len(e.Steps) == 0 {
		return fmt.Errorf("nothing shows this vertical working. This vertical asked for steps a person "+
			"can run: write each one on a %q line, saying what to run or press and what they should see",
			"Steps 1:")
	}
	if len(e.Steps) < StepsAtLeast {
		return fmt.Errorf("this is %d step, and steps a person follows are at least %d: one line is a "+
			"sentence about the work rather than a way to check it", len(e.Steps), StepsAtLeast)
	}
	for at, step := range e.Steps {
		if len(step) > StepsLineLimit {
			return fmt.Errorf("step %d is %d characters and the ceiling is %d: it is one line somebody "+
				"reads with their hand on the keyboard, so say one thing to do and what they see",
				at+1, len(step), StepsLineLimit)
		}
		if !reproduces.MatchString(step) && !pressed.MatchString(step) {
			return fmt.Errorf("step %d says nothing anybody can do: %q. Each step is a command to run, an "+
				"address to open or a key to press. A paragraph saying the thing works is not steps",
				at+1, step)
		}
		if !seen.MatchString(step) {
			return fmt.Errorf("step %d says what to do and never what it looks like when it worked: %q. "+
				"Say what a person should see, so they can tell a pass from a failure", at+1, step)
		}
	}
	return LabelsIt("these steps", e.Taken)
}

// Holds is the refusal where this evidence is not the kind the vertical asked for.
//
// The kind is the vertical's to decide, so this is the gate rather than a preference. A vertical that
// asked for steps and is offered a picture has not met it, and the refusal says which kind was asked
// for: a worker told only that its answer is wrong sends another picture.
func (e Evidence) Holds(wanted Kind) error {
	if wanted == "" {
		wanted = KindPicture
	}
	if e.kind() != wanted {
		return fmt.Errorf("this vertical asks to be shown with %s and this offers %s: %s",
			wanted, e.kind(), whatThatKindWants(wanted))
	}
	return e.Shows()
}

// whatThatKindWants is the line that says how to answer with the kind that was asked for. It is the
// end of every wrong kind refusal, because the refusal a worker cannot act on costs another task.
func whatThatKindWants(wanted Kind) string {
	switch wanted {
	case KindRecording:
		return "capture the screen while it runs, join the captures with krewe record, and name the " +
			"recording on a \"Recording:\" line"
	case KindSteps:
		return "write what a person runs or presses to see it themselves, one per \"Steps 1:\" line, " +
			"each saying what they should see"
	default:
		return "start what you built, draw it with krewe render, and name the picture on a " +
			"\"Picture:\" line"
	}
}

// ShowIt is what a build worker is told about the evidence it owes for one vertical.
//
// It says how, because a worker that is asked to show something and given no way to show it answers
// with a sentence instead. Each kind spells its own way out, and the terminal is spelled out twice
// for the same reason: a product with no page is the shape a session reaches for a description of.
func ShowIt(wanted Requirement) string {
	switch wanted.Evidence {
	case KindRecording:
		return fmt.Sprintf("Then show vertical %d working, and this one asked for a recording: a still "+
			"frame cannot carry what it does.\n\n"+
			"Start what you built and capture it while it runs. A page: draw it with krewe render at "+
			"each step worth seeing. A terminal: run it under tmux and capture the screen every half "+
			"second with tmux capture-pane -t <session> -e -p > frame-01.txt. Then join the captures: "+
			"krewe record %s frame-*.txt. Put the file in /home/agent/shared, which is this workspace's "+
			"shared folder, so a person can open it after this session is gone. If this machine has no "+
			"encoder, krewe record says so: then say what you would have shown in steps a person can "+
			"run, and a person attaches a recording by hand.\n\n"+
			"Then label it. The label says what was running and the command behind it, so somebody else "+
			"can get the same recording.", wanted.Number, "vertical.webm")
	case KindSteps:
		return fmt.Sprintf("Then show vertical %d working, and this one asked for steps rather than a "+
			"capture: what it does is not something a picture shows.\n\n"+
			"Write the steps a person follows to see it themselves. Each step is one thing they run or "+
			"press, and it says what they should see. Name the command, the address or the key, and "+
			"write the steps on \"Steps 1:\", \"Steps 2:\" lines. A paragraph saying the thing works is "+
			"not steps, and it is refused.\n\n"+
			"Then label them. The label says what has to be running before step 1 and how to get there, "+
			"so somebody else can follow them.", wanted.Number)
	default:
		return fmt.Sprintf("Then show vertical %d working, and it has to be a picture. Not a description "+
			"of it working, not a passing test named after it, and not a sample you generated to "+
			"illustrate what it would look like.\n\n"+
			"Start what you built and capture it. A page: krewe render http://localhost:3000 %s. A "+
			"terminal: run it, capture the screen with tmux capture-pane, and draw that capture with "+
			"krewe render. Put the file in /home/agent/shared, which is this workspace's shared folder, so "+
			"a person can open it after this session is gone.\n\n"+
			"Then label it. The label says what was running and the command that drew it, so somebody else "+
			"can get the same picture.", wanted.Number, "vertical.png")
	}
}

// evidenceLine is the shape evidence and its label arrive in, and the shape the record keeps them in.
// Read off the reply the way every other report in these stages is read, so what the system finds is
// what the worker meant to say.
//
// Steps are written in full here and never as the singular. The number on a line in the record is the
// vertical it belongs to, and a worker's own "Step 1:" numbers the step instead, so reading that shape
// out of the record would file every worker's first step under vertical 1.
var evidenceLine = regexp.MustCompile(
	`(?im)^[ \t]*(picture|recording|steps|taken)[ \t]*(\d*)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)

// EvidenceIn is everything a kept build record holds for a person to look at, in the order the
// verticals are written.
//
// It reads the record rather than a column of its own, for the reason the stage is read off the row:
// the evidence arrived inside the build reports and a second copy of it could only disagree with the
// first.
func EvidenceIn(kept string) []Evidence {
	held := map[int]*Evidence{}
	var order []int
	for _, found := range evidenceLine.FindAllStringSubmatch(kept, -1) {
		number := numberIn(found[2])
		said := TidySentence(found[3])
		if said == "" {
			continue
		}
		one, seen := held[number]
		if !seen {
			one = &Evidence{Vertical: number, Kind: KindPicture}
			held[number], order = one, append(order, number)
		}
		switch strings.ToLower(found[1]) {
		case "taken":
			if one.Taken == "" {
				one.Taken = said
			}
		case "steps":
			one.Kind = KindSteps
			one.Steps = append(one.Steps, said)
		case "recording":
			if one.File == "" {
				one.Kind, one.File = KindRecording, theFileIn(said)
			}
		default:
			if one.File == "" {
				one.File = theFileIn(said)
				// A file that moves is a recording however it was labelled, because what a person does with
				// it is press play. The kind follows the evidence rather than the word above it.
				one.Kind = KindPicture
				if AFileOfKind(KindRecording, one.File) {
					one.Kind = KindRecording
				}
			}
		}
	}
	shown := make([]Evidence, 0, len(order))
	for _, number := range order {
		shown = append(shown, *held[number])
	}
	return shown
}

// EvidenceFor is what a build record holds for one vertical, and an empty one where it holds nothing.
func EvidenceFor(kept string, vertical int) Evidence {
	for _, shown := range EvidenceIn(kept) {
		if shown.Vertical == vertical {
			return shown
		}
	}
	return Evidence{}
}

// numberIn is the vertical a line is written under, and zero where the line carries no number.
// Evidence written under no number belongs to vertical zero, which no accepted list has, so it is
// refused by name rather than attached to the first vertical that happens to be there.
func numberIn(digits string) int {
	number := 0
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0
		}
		number = number*10 + int(digit-'0')
	}
	if digits == "" {
		return 0
	}
	return number
}

// theFileIn is the name a worker wrote, without the folders it wrote in front of it. A person opens
// the shared folder, so what the record keeps is the name they will see there.
func theFileIn(said string) string {
	at := strings.LastIndexAny(said, "/\\")
	return said[at+1:]
}

// kind is which of the three this is, with the default in one place. Evidence that says nothing about
// its kind is a picture, and a caller that builds one by hand should not have to say so.
func (e Evidence) kind() Kind {
	if e.Kind == "" {
		return KindPicture
	}
	return e.Kind
}
