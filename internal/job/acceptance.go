package job

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
)

// A job is not done until a person has looked at a picture of the thing running.
//
// The build stage closes on three checks the machine can make: the run executed something, nothing
// fails, and a file was written to make it pass. All three are the machine reading its own work, and
// a session that finishes the work writes the answer that says the work is right. It says so in good
// faith, because from inside the session there is nothing to compare against.
//
// The fourth check is a person. It is visual, and that is the whole of it: a screenshot or a
// recording of the built thing actually running, not a description of it working, not a passing test
// named after it, and not a sample generated to illustrate what it would look like. A picture and a
// paragraph both read as evidence on the page and are worth completely different amounts.
//
// So every vertical arrives with a picture and a label. The label says where the picture came from
// and what it takes to reproduce it, and it is part of the record rather than part of the prose: a
// reader who cannot reproduce a picture concludes the code does not do what was claimed, and they are
// right to.
//
// Then the job holds. The person answers, and only their word lands it. An answer that is not the
// acceptance names what was wrong and the verticals go back to the build stage, which already knows
// how to fan out again. Nothing here settles a job on the system's own account, which is the point:
// the three stages before this one all end with a person, and the one that decides whether the work
// was worth doing cannot be the one that ends without them.

// TheAcceptanceAsk is the phrase the question about a finished build opens with, and it is how
// anything holding a reply can tell that answer from every other one a job gets.
//
// It matters because that answer is the only one that ends the job rather than continuing it. The
// verticals were built by a worker each and the row itself never ran, so there is no session waiting
// to be told to carry on, and the word a person writes here is the last thing that happens.
const TheAcceptanceAsk = "Every vertical is built and its tests pass. Look at the pictures and say " +
	"whether the value arrived."

// AskedToAccept says whether this question is the one a finished build puts to a person.
func AskedToAccept(question string) bool { return strings.Contains(question, TheAcceptanceAsk) }

// PictureLimit is how long the label on one picture may be. It is the line a person reads beside the
// picture, and it is the ceiling every other one line field on this row already has.
const PictureLimit = TitleLimit

// aPictureFile is a name that is a picture: a still, or a recording of one running. A terminal
// product is in here twice, as a recording of the session and as a drawn capture of the screen,
// because a product with no page still has to be shown working.
var aPictureFile = regexp.MustCompile(
	`(?i)\.(png|jpe?g|gif|webp|svg|apng|avif|mp4|webm|mov|m4v|gifv|cast)$`)

// aRenderedSample is a label admitting the picture was generated to illustrate rather than captured
// from something running. Each of these is a word somebody writes when they are being honest about a
// mock up, so the refusal takes them at their word and says what to send instead.
//
// The list is deliberately short and concrete, the way the design stage's list of plumbing is. A long
// list guesses at what somebody might write, and a refusal here costs a task and a person's patience.
var aRenderedSample = regexp.MustCompile(`(?i)\b(mock ?up|mocked|mock|wireframe|placeholder|` +
	`illustrative|illustration|for illustration|hand ?drawn|drawn by hand|figma|sketch|` +
	`what it would look like|how it would look|as it would|would look|artist)\b`)

// reproduces is the shape of a label that says how to get the picture again: a command, an address,
// or a path. One of the three has to be in there, because a label with none of them is a sentence
// about the picture rather than a way back to it.
var reproduces = regexp.MustCompile(`(?i)(^|[\s"'` + "`" + `(])(krewe |quay |make |go |npm |npx |yarn |pnpm |` +
	`docker |terraform |cargo |python |node |curl |tmux |git |bash |sh |\./|/[a-z0-9._-]+/|` +
	`https?://|localhost[:/]|127\.0\.0\.1)`)

// Picture is one picture of one vertical working, with the label that says where it came from.
type Picture struct {
	// Vertical is the vertical this picture shows working.
	Vertical int
	// File is the name of the picture, in the workspace's shared folder, so a person on the machine
	// can open it after the sandbox that made it is gone.
	File string
	// Taken is the label: where the picture came from and what it takes to reproduce it.
	Taken string
}

// APicture says whether this name is a picture rather than a description of one.
//
// It is the name that is checked and not the bytes, because the bytes are in a sandbox and this
// reads a report. What it refuses is the answer that says "the screenshot in the pull request" or
// names a text file: neither is something a person can open and look at.
func APicture(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t") {
		return false
	}
	return aPictureFile.MatchString(name)
}

// LabelsIt is the refusal where a label does not say where the picture came from, and nil where it
// does.
//
// Two things make a label. It says the picture was captured from something running, and it says what
// to type to get it again. A picture nobody can reproduce is worth nothing, and a generated sample
// presented as a capture is worse than no picture at all, so the second is refused by name rather
// than left to a reader to notice.
func LabelsIt(file, taken string) error {
	taken = TidySentence(taken)
	if taken == "" {
		return fmt.Errorf("%q carries no label, so nobody can tell what it is a picture of: write a "+
			"%q line saying what was running, the command that drew it, and what has to be up to draw it "+
			"again", file, "Taken 1:")
	}
	if len(taken) > PictureLimit {
		return fmt.Errorf("the label on %q is %d characters and the ceiling is %d: say what was "+
			"running and the command that drew it, in one line", file, len(taken), PictureLimit)
	}
	if found := aRenderedSample.FindString(taken); found != "" {
		return fmt.Errorf("the label on %q says %q, so this is a picture generated to illustrate rather "+
			"than one captured from the thing running: start it, draw what it does, and label that. A "+
			"sample presented as a capture is worse than no picture at all", file, strings.ToLower(found))
	}
	if !reproduces.MatchString(taken) {
		return fmt.Errorf("the label on %q does not say how to get the picture again: name the command, "+
			"the address or the path in it, so a reader can reproduce what you are showing them", file)
	}
	return nil
}

// Shows is the refusal where this picture does not show anything working, and nil where it does.
//
// It names no vertical, because every caller of it is already writing the vertical into the sentence
// it wraps this in, and the same refusal arriving with the vertical in it twice reads as two
// problems.
func (p Picture) Shows() error {
	if p.File == "" {
		return fmt.Errorf("nothing shows this vertical working. Run what you built, capture what it "+
			"does, put the picture in the shared folder and name it on a %q line. A passing test named "+
			"after a vertical is not a picture of it", "Picture: home.png")
	}
	if !APicture(p.File) {
		return fmt.Errorf("%q is not a picture: answer with the name of one still or one recording, "+
			"with no spaces in the name, such as %s", p.File, "home.png, run.mp4 or session.cast")
	}
	return LabelsIt(p.File, p.Taken)
}

// ShownWorking is the picture of one vertical, and the refusal where nothing shows it working, with
// the vertical named so a person reading it knows which one to go and look at.
func ShownWorking(vertical Requirement, shot Picture) error {
	if err := shot.Shows(); err != nil {
		return fmt.Errorf("vertical %d, %q: %w", vertical.Number, vertical.Text, err)
	}
	return nil
}

// ShowItWorking is what a build worker is told about the picture it owes.
//
// It says how, because a worker that is asked for a picture and given no way to make one answers
// with a sentence instead. The terminal is spelled out for the same reason: a product with no page
// is the shape a session reaches for a description of.
func ShowItWorking(wanted Requirement) string {
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

// pictureLine is the shape a picture and its label arrive in, and the shape the record keeps them in.
// Read off the reply the way every other report in these stages is read, so what the system finds is
// what the worker meant to say.
var pictureLine = regexp.MustCompile(
	`(?im)^[ \t]*(picture|taken)[ \t]*(\d*)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)

// PicturesIn is every picture a kept build record carries, in the order the verticals are written.
//
// It reads the record rather than a column of its own, for the reason the stage is read off the row:
// the pictures arrived inside the build reports and a second copy of them could only disagree with
// the first.
func PicturesIn(kept string) []Picture {
	held := map[int]*Picture{}
	var order []int
	for _, found := range pictureLine.FindAllStringSubmatch(kept, -1) {
		number := numberIn(found[2])
		said := TidySentence(found[3])
		if said == "" {
			continue
		}
		one, seen := held[number]
		if !seen {
			one = &Picture{Vertical: number}
			held[number], order = one, append(order, number)
		}
		if strings.EqualFold(found[1], "taken") {
			if one.Taken == "" {
				one.Taken = said
			}
			continue
		}
		if one.File == "" {
			one.File = path.Base(said)
		}
	}
	shots := make([]Picture, 0, len(order))
	for _, number := range order {
		shots = append(shots, *held[number])
	}
	return shots
}

// numberIn is the vertical a line is written under, and zero where the line carries no number. A
// picture written under no number belongs to vertical zero, which no accepted list has, so it is
// refused by name rather than attached to the first vertical that happens to be there.
func numberIn(digits string) int {
	number, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return number
}

// PictureOf is the picture a build record holds for one vertical, and an empty one where it holds
// none.
func PictureOf(kept string, vertical int) Picture {
	for _, shot := range PicturesIn(kept) {
		if shot.Vertical == vertical {
			return shot
		}
	}
	return Picture{}
}

// Accepted says whether a person looked at what was built and said the value arrived.
func Accepted(one *Job) bool { return one != nil && one.Accepted }

// WaitingForItsAcceptance says whether a person has answered the question about what was built and
// the system has not acted on their answer yet.
//
// It is the last gate, so it sits behind every other one. What it turns on is the question the row
// carries: an answer to anything else is an answer to that other thing, and reading it as an
// acceptance would end a job on a word about something else entirely.
func WaitingForItsAcceptance(one *Job) bool {
	return Built(one) && !Accepted(one) && one.Told != "" && AskedToAccept(one.Question)
}

// NotYetAccepted says whether this job would be calling itself done with nobody having looked at it.
//
// It guards the ordinary settling road rather than this stage's own. A job whose verticals are built
// reaches done one way, which is a person accepting them, and every other road into done on such a
// row is a session settling its own work.
func NotYetAccepted(one *Job) bool {
	return one != nil && one.Parent == "" && Built(one) && !Accepted(one)
}

// SentBackToBuild says whether the workers already on this vertical answered before a person looked at what
// they built and said the value did not arrive.
//
// It is read off two facts the row already carries rather than off a column of its own. A job
// arriving at the build stage for the first time was told nothing, because approving its plan cleared
// that, and it has no workers. A job that came back carries what the person said and the workers that
// built the thing they rejected.
func SentBackToBuild(one *Job, workers int) bool {
	return one != nil && one.Told != "" && workers > 0 && !Accepted(one)
}

// AcceptsWhatWasBuilt says whether an answer is the acceptance.
//
// The plan's word and the list's word, deliberately the same one. Three gates a person answers, each
// with its own word for yes, is a system that teaches somebody three habits and then punishes two of
// them.
func AcceptsWhatWasBuilt(answer string) bool {
	return strings.EqualFold(TidySentence(answer), theAnswerThatApproves)
}

// AcceptWhatWasBuilt is what a person is asked when every vertical is green and every one of them has
// a picture.
//
// The pictures come first and the counts come second, because the counts are the machine's three
// checks and the person is here for the fourth. Each one names its file, where to open it and what it
// took to draw, so the question can be answered from the terminal it arrived in.
func AcceptWhatWasBuilt(one *Job, wanted []Requirement, reports map[int]BuildReport) string {
	var lines []string
	for _, vertical := range wanted {
		report := reports[vertical.Number]
		lines = append(lines, fmt.Sprintf("%d. %s\n   shown: %s\n   picture: %s\n   taken: %s\n   "+
			"%d tests ran, %d of them named as passing now", vertical.Number, vertical.Text,
			vertical.Shown, report.Picture, report.Taken, report.Ran, len(report.Passing)))
	}
	return fmt.Sprintf("%s\n\n%s\n\nThe pictures are in this workspace's shared folder, and krewe "+
		"where %s says where that is on this machine.\n\nOpen them. Answer %s and the job is done. "+
		"Answer with what is missing instead, and the verticals go back to be built again from what "+
		"you said. Nothing else happens on this job until you do.",
		TheAcceptanceAsk, strings.Join(lines, "\n"), one.Workspace, theAnswerThatApproves)
}

// AcceptedIt is the record of the job ending: how many verticals a person looked at, and how many
// pictures they were shown.
func AcceptedIt(kept string) string {
	verticals, _ := BuiltOn(kept)
	return fmt.Sprintf("a person looked at %s of this job's %s and said the value arrived",
		pluralPictures(len(PicturesIn(kept))), pluralVerticals(verticals))
}

// SendItBackToBuild is what the verticals are told when the answer was not the acceptance.
//
// It carries the person's words whole rather than a summary of them, for the reason the request is
// kept whole: a summary of what somebody said is the compression that loses the thing they said.
func SendItBackToBuild(said string) string {
	return fmt.Sprintf("A person looked at what was built and it is not accepted. This is what they "+
		"said, in their words:\n\n%s\n\nBuild it again against that, and show it working.", said)
}

// NotAccepted is why a job that reached the end of the build stage stopped short of done.
//
// It is the refusal on the ordinary settling road: the work is answered for and nobody has looked at
// it. The reason says what is missing and the one command that ends it, because a person reading a
// stopped job has to be able to act on it without opening the session.
func NotAccepted(one *Job) string {
	shots := len(PicturesIn(one.Build))
	if shots == 0 {
		return fmt.Sprintf("this job's verticals are built and nothing shows any of them running, so "+
			"there is nothing for anybody to look at. A job is not done until a person has looked at a "+
			"picture. Read what was built with krewe job show %s", one.ID)
	}
	return fmt.Sprintf("this job's verticals are built and nobody has accepted them, so it is the "+
		"system calling its own work done. Look at the %s on the record and answer the job: krewe job "+
		"answer %s %q", pluralPictures(shots), one.ID, theAnswerThatApproves)
}

// pluralPictures and pluralVerticals keep a sentence readable for one and for several.
func pluralPictures(count int) string {
	if count == 1 {
		return "1 picture"
	}
	return fmt.Sprintf("%d pictures", count)
}

func pluralVerticals(count int) string {
	if count == 1 {
		return "1 vertical"
	}
	return fmt.Sprintf("%d verticals", count)
}

// acceptIt is what happens to a job once a person has answered the question about what was built.
//
// Two roads and no third. The word that accepts writes their acceptance on the row, and every other
// answer sends the verticals back to the build stage with what they said.
//
// It records the acceptance rather than landing the job, and the difference matters. The job still
// owes what every other job owes at its ending: the pull request its work is read in, and an account
// of the plan a person approved. Landing here would trade one unproved ending for another, so what
// this writes is the permission, and the ordinary road below carries the job to done under it. What
// stops that road ending without this is NotYetAccepted.
func (c *Controller) acceptIt(ctx context.Context, one *Job) {
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	if !AcceptsWhatWasBuilt(one.Told) {
		c.sendItBackToBuild(ctx, one)
		return
	}
	record := c.event(ctx, one, EventAccepted, AcceptedIt(one.Build))
	if _, err := c.store.AcceptJob(ctx, one.ID, one.Told, record); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not record that a person accepted a job",
				"job", one.ID, "error", err)
		}
		// Nothing landed either way, so a later tick reads the same row and does the same thing. The
		// person's word is on the row already, which is what makes that safe: it is not lost by the write
		// failing, and it cannot be counted twice by the write happening again.
		return
	}
	c.exported(ctx, record)
}

// sendItBackToBuild puts the verticals back to the build stage with what the person said was missing.
//
// The record of what was built goes, and their words stay on the row as the thing the next fan out is
// built against. That is what makes it the build stage picking the work up again rather than a new
// job: every worker's pull request is still open, the branch is still there, and what changed is one
// person's reading of whether the value arrived.
func (c *Controller) sendItBackToBuild(ctx context.Context, one *Job) {
	record := c.event(ctx, one, EventSentBack, oneLine(SendItBackToBuild(one.Told)))
	if _, err := c.store.SendJobBackToBuild(ctx, one.ID, record); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not send a job that was not accepted back to be built",
				"job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
}

// TheValueArrived is the line the session that finishes an accepted job is given, so what it writes
// its ending against is the acceptance rather than its own reading of the work.
const TheValueArrived = "A person looked at a picture of every vertical of this job running and " +
	"said the value arrived. What is left is the ending every job has: push the work, open the pull " +
	"request, and answer with its address. Build nothing further: what they accepted is what is built."
