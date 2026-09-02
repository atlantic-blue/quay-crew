package job

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
)

// A job is not done until a person has looked at what it built.
//
// The build stage closes on three checks the machine can make: the run executed something, nothing
// fails, and a file was written to make it pass. All three are the machine reading its own work, and
// a session that finishes the work writes the answer that says the work is right. It says so in good
// faith, because from inside the session there is nothing to compare against.
//
// The fourth check is a person. What they are shown is evidence of the built thing actually running,
// not a description of it working, not a passing test named after it, and not a sample generated to
// illustrate what it would look like.
//
// There are three kinds of evidence and the vertical says which one it needs: a picture, a recording,
// or steps a person runs themselves. See evidence.go for what each kind is and what it is held to.
// Whichever kind arrives, it carries a label saying where it came from and what it takes to reproduce
// it, and the label is part of the record rather than part of the prose: a reader who cannot
// reproduce what they were shown concludes the code does not do what was claimed, and they are right
// to.
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
const TheAcceptanceAsk = "Every vertical is built and its tests pass. Look at what each one is " +
	"shown with and say whether the value arrived."

// AskedToAccept says whether this question is the one a finished build puts to a person.
func AskedToAccept(question string) bool { return strings.Contains(question, TheAcceptanceAsk) }

// ShownWorking is the evidence for one vertical, held to the kind that vertical asked for, with the
// vertical named so a person reading the refusal knows which one to go and look at.
func ShownWorking(vertical Requirement, shown Evidence) error {
	if err := shown.Holds(vertical.Evidence); err != nil {
		return fmt.Errorf("vertical %d, %q: %w", vertical.Number, vertical.Text, err)
	}
	return nil
}

// ShowItWorking is what a build worker is told about the evidence it owes. The kinds are in
// evidence.go, and this is the name the build stage has always called it by.
func ShowItWorking(wanted Requirement) string { return ShowIt(wanted) }

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

// AcceptWhatWasBuilt is what a person is asked when every vertical is green and every one of them is
// shown with the kind of evidence it asked for.
//
// The evidence comes first and the counts come second, because the counts are the machine's three
// checks and the person is here for the fourth. Each vertical says what it is shown with, where to
// open it or what to run, and what it took, so the question can be answered from the terminal it
// arrived in.
func AcceptWhatWasBuilt(one *Job, wanted []Requirement, reports map[int]BuildReport) string {
	var lines []string
	for _, vertical := range wanted {
		report := reports[vertical.Number]
		shown := report.Evidence()
		lines = append(lines, fmt.Sprintf("%d. %s\n   shown: %s\n%s   taken: %s\n   "+
			"%d tests ran, %d of them named as passing now", vertical.Number, vertical.Text,
			vertical.Shown, whatToLookAt(shown), shown.Taken, report.Ran, len(report.Passing)))
	}
	return fmt.Sprintf("%s\n\n%s\n\nAnything with a file is in this workspace's shared folder, and "+
		"krewe where %s says where that is on this machine.\n\nLook at it. Answer %s and the job is "+
		"done. Answer with what is missing instead, and the verticals go back to be built again from "+
		"what you said. Nothing else happens on this job until you do.",
		TheAcceptanceAsk, strings.Join(lines, "\n"), one.Workspace, theAnswerThatApproves)
}

// whatToLookAt is the evidence itself, written the way its kind is read: a file to open, or the steps
// to follow in the order they are followed.
func whatToLookAt(shown Evidence) string {
	if shown.Kind == KindSteps {
		lines := make([]string, 0, len(shown.Steps)+1)
		lines = append(lines, "   steps:")
		for at, step := range shown.Steps {
			lines = append(lines, fmt.Sprintf("     %d. %s", at+1, step))
		}
		return strings.Join(lines, "\n") + "\n"
	}
	return fmt.Sprintf("   %s: %s\n", shown.Kind, shown.File)
}

// AcceptedIt is the record of the job ending: how many verticals a person looked at, and how much
// evidence they were shown.
func AcceptedIt(kept string) string {
	verticals, _ := BuiltOn(kept)
	return fmt.Sprintf("a person looked at the evidence for %d of this job's %s and said the value "+
		"arrived", len(EvidenceIn(kept)), pluralVerticals(verticals))
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
	shown := len(EvidenceIn(one.Build))
	if shown == 0 {
		return fmt.Sprintf("this job's verticals are built and nothing shows any of them running, so "+
			"there is nothing for anybody to look at. A job is not done until a person has looked at what "+
			"it built. Read what was built with krewe job show %s", one.ID)
	}
	return fmt.Sprintf("this job's verticals are built and nobody has accepted them, so it is the "+
		"system calling its own work done. Look at the %s on the record and answer the job: krewe job "+
		"answer %s %q", theEvidenceFor(shown), one.ID, theAnswerThatApproves)
}

// theEvidenceFor and pluralVerticals keep a sentence readable for one and for several.
func theEvidenceFor(count int) string {
	if count == 1 {
		return "the evidence for 1 vertical"
	}
	return fmt.Sprintf("the evidence for %d verticals", count)
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
const TheValueArrived = "A person looked at every vertical of this job running and said the value " +
	"arrived. What is left is the ending every job has: push the work, open the pull request, and " +
	"answer with its address. Build nothing further: what they accepted is what is built."
