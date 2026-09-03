package model

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// FakeRunner is a Runner for tests. It records the last request and returns a canned response.
type FakeRunner struct {
	Reply string
	// Exact answers with Reply and nothing else, whatever the task asked for. It is how a test says "a
	// session that did not do as it was told", which is the only way to write the sad path of a rule
	// the double otherwise follows.
	Exact     bool
	SessionID string
	Err       error
	LastReq   Request
	// Takes is how long a task pretends to take. Zero is instant, which is right for almost every
	// test and wrong for any test about something happening while a task is under way: with an
	// instant model a whole automation finishes before a second command can be typed, and a test
	// of stopping one would be racing rather than testing.
	Takes time.Duration
	// Gate holds a task open until it is closed. Same purpose as Takes and none of its guesswork: a
	// test that waits for a duration is a test that passes on a fast machine, and the thing being
	// tested here is what is true *while* a task runs. Nil runs straight through.
	Gate chan struct{}
	// Started is closed once, when the first task begins, so a test can know a task is under way
	// rather than assume it by the time it took to ask.
	Started chan struct{}

	once sync.Once
}

// compile time check.
var _ Runner = (*FakeRunner)(nil)

// Run records the request and returns the canned response (or Err). The sandbox is ignored.
func (f *FakeRunner) Run(ctx context.Context, _ sandbox.Sandbox, req Request) (Response, error) {
	f.LastReq = req
	if f.Started != nil {
		f.once.Do(func() { close(f.Started) })
	}
	if f.Gate != nil {
		select {
		case <-f.Gate:
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	}
	if f.Takes > 0 {
		select {
		case <-time.After(f.Takes):
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	}
	if f.Err != nil {
		return Response{}, f.Err
	}
	// The name the system gave this conversation comes back, as a runtime that honours the flag reports
	// it. SessionID is what a test uses to stand for a runtime that names the conversation itself.
	sessionID := f.SessionID
	if sessionID == "" {
		sessionID = conversationOf(req, "fake-session")
	}
	return Response{Reply: f.answer(req), ModelSessionID: sessionID}, nil
}

// OutcomeMarker opens the line a job asks a session to end its answer with. It is the same word
// internal/job holds as job.OutcomeMarker, spelled here because internal/job imports this package and
// a double cannot import what imports it. internal/job holds the two together in a test.
const OutcomeMarker = "Outcome:"

// FakeOutcome is the word this double states when the task it was given asked for one. It is
// deliberately the ordinary one: a test about a session that states nothing, or something else, sets
// Reply itself.
const FakeOutcome = "proved"

// UnderstandingAsk is the phrase a task carries when it asks a session what it understood before it
// plans, and UnderstandingMarker opens the first line of what comes back. Both are spelled here
// because internal/job imports this package and a double cannot import what imports it. internal/job
// holds them together in a test, and holds this double's reading to its own reader.
const (
	UnderstandingAsk    = "write no plan yet"
	UnderstandingMarker = "Understood:"
)

// FakeUnderstanding is what this double says when it is asked what it understood.
//
// It is the same rule the outcome line follows: a job that states the sentence is asked what it
// understood before anything else, so a double that answered its plan to that question would make
// every test about a planned job into a test about the double ignoring its task. A test about a
// session that says nothing readable sets Reply itself, or sets Exact.
const FakeUnderstanding = "Understood: the work the brief describes, for the person in the sentence\n" +
	"Not: anything the brief leaves out\n" +
	"Told: the brief says what to build\n" +
	"Assumed: the design in the repository is the current one\n" +
	"Unknown: which surface a person reads the result on\n" +
	"Confidence: fairly sure of the shape, and least sure of the surface\n" +
	"Question 1: which surface does a person read this on"

// DesignAsk is the phrase a task carries when it asks a session what it would build, and DesignMarker
// opens the first line of the list that comes back. Both are spelled here for the reason the two
// above are: internal/job imports this package, so a double cannot import what imports it, and
// internal/job holds them together in a test.
const (
	DesignAsk = "list the verticals you would build"
	// The shown line rather than the first line, because a vertical the person put on the list opens
	// with Yours rather than with Vertical, and both are lists.
	DesignMarker = "Shown 1:"
)

// FakeDesign is what this double says when it is asked what it would build.
//
// Two verticals, each naming the person it serves, because the reader refuses a list that names only
// the work a system does for itself. A test about a list that is refused writes the list it means.
const FakeDesign = "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Shown 1: the transcript prints in the terminal for a link the person chooses\n" +
	"Vertical 2: a person opens the same transcript in a browser and sends the address to somebody\n" +
	"Shown 2: the page renders that transcript at an address the person can share"

// TestAsk is the phrase a task carries when it asks a session for the failing tests of one
// requirement, and TestMarker opens the line of the report that says the run happened. Both are
// spelled here for the reason the four above are: internal/job imports this package.
const (
	TestAsk = "write the tests that prove this requirement, and make them fail"
	// The line that carries the count, because it is the one line every report has: a report names as
	// many failing tests as it wrote and at least one.
	TestMarker = "Ran:"
)

// FakeTestReport is what this double says when it is asked for the failing tests of one requirement.
//
// It names no requirement on a report line, because the stage no longer asks for one: it reads that
// number off the run. What the double reads out of the task is which requirement it was handed, and
// it uses that for the test it names and for the branch its work goes to.
func FakeTestReport(asked string) string {
	requirement := 1
	if found := whichRequirement.FindStringSubmatch(asked); found != nil {
		requirement, _ = strconv.Atoi(found[1])
	}
	return namingWhereTheWorkWent(fmt.Sprintf(
		"I wrote the tests for requirement %d and ran the suite.\n\n"+
			"Ran: 12\nFailing 1: TestRequirement%dFailsUntilSomethingBuildsIt",
		requirement, requirement), asked, requirement)
}

// namingWhereTheWorkWent ends a report the way a session that read its task ends one: a task that
// says the job works in a repository says the answer has to name the pull request the work went to,
// and a job whose answer names none does not land.
//
// A task that already names the pull request its work lands in gets that one back. That is the build
// worker, whose tests are open in a pull request somebody else opened, and a double that opened a
// second one would be doing what this stage exists to stop.
func namingWhereTheWorkWent(reply, asked string, number int) string {
	if found := aPullRequestNamed.FindString(asked); found != "" {
		return reply + "\n\nPushed to the branch, so the work is in " + found
	}
	if found := theRepositoryNamed.FindStringSubmatch(asked); found != nil {
		return reply + fmt.Sprintf("\n\nPushed the branch and opened https://github.com/%s/pull/%d",
			found[1], number)
	}
	return reply
}

// theRepositoryNamed is the repository a task says the job works in, and aPullRequestNamed is a pull
// request address a task carries. The system writes both lines, so the double reads what it was
// given rather than being told the address by whoever wrote the scenario.
var (
	theRepositoryNamed = regexp.MustCompile(`(?im)^This job works in ([^\s,]+)`)
	aPullRequestNamed  = regexp.MustCompile(`https?://[^\s]+/pull/[0-9]+`)
)

// whichRequirement finds the requirement a task was handed, which the ask states on a line of its
// own.
var whichRequirement = regexp.MustCompile(`(?im)^Requirement:?[ \t]+(\d+)`)

// BuildAsk is the phrase a task carries when it asks a session to build one vertical against its
// failing tests, and BuildMarker opens the line of the report that names a test passing now. Both are
// spelled here for the reason the six above are: internal/job imports this package.
const (
	BuildAsk = "make the failing tests for this vertical pass, and change no test"
	// The line that names a passing test, because it is the one line every build report has: a report
	// that names none says nothing was turned green.
	BuildMarker = "Passing 1:"
)

// FakeBuildReport is what this double says when it is asked to build one vertical.
//
// It names no vertical on a report line, because the stage no longer asks for one: it reads that
// number off the run. It does read the vertical and the failing tests out of the task, because the
// stage refuses a report that does not name the tests that were failing, and a double that always
// named the same tests would be refused for every worker but the first.
func FakeBuildReport(asked string) string {
	vertical := 1
	if found := whichVertical.FindStringSubmatch(asked); found != nil {
		vertical, _ = strconv.Atoi(found[1])
	}
	var passing []string
	for at, found := range failingTest.FindAllStringSubmatch(asked, -1) {
		passing = append(passing, fmt.Sprintf("Passing %d: %s", at+1, strings.TrimSpace(found[1])))
	}
	if len(passing) == 0 {
		passing = []string{fmt.Sprintf("Passing 1: TestVertical%dPasses", vertical)}
	}
	return namingWhereTheWorkWent(fmt.Sprintf(
		"I built vertical %d and ran the suite.\n\nRan: 14\nRed: 0\n%s\n"+
			"Changed 1: internal/vertical%d.go\n%s",
		vertical, strings.Join(passing, "\n"), vertical, fakeEvidence(asked, vertical)),
		asked, vertical)
}

// fakeEvidence is the evidence this double shows for the vertical it built, in the kind the ask asked
// for.
//
// It reads the ask for the reason the vertical is read out of it: a vertical asks to be shown with a
// picture, a recording or steps, and a double that always sent a picture would be refused by every
// vertical that asked for one of the other two. What it looks for is the report line the ask spells
// out, which is the same string the stage reads the answer back with.
func fakeEvidence(asked string, vertical int) string {
	switch {
	case strings.Contains(asked, "Recording: the name of a recording"):
		return fmt.Sprintf("Recording: vertical%d.webm\nTaken: the terminal under tmux, captured with "+
			"tmux capture-pane and joined with krewe record", vertical)
	case strings.Contains(asked, "Steps 1: the first thing a person runs"):
		return fmt.Sprintf("Steps 1: run krewe job show job-%d, and the row says it is built\n"+
			"Steps 2: press r, and the listing comes back with the job on it\n"+
			"Taken: the console against a running system, started with krewe console", vertical)
	default:
		return fmt.Sprintf("Picture: vertical%d.png\nTaken: the page at http://localhost:3000, drawn "+
			"with krewe render while the server was up", vertical)
	}
}

// whichVertical finds the vertical a task was handed, which the ask states on a line of its own.
var whichVertical = regexp.MustCompile(`(?im)^Vertical:?[ \t]+(\d+)`)

// failingTest finds the tests the ask says fail now, which it lists one to a line under a dash.
var failingTest = regexp.MustCompile(`(?m)^- (.+)$`)

// answer is what the double says, which follows the task it was handed the way a model does.
//
// A task that asks for an outcome gets one. Every job says so beside its brief, so a double that
// ignored it would be looser than the thing it stands in for: every job would stop, and every test
// about a job would be a test about that. A reply that already states an outcome is left alone, which
// is how a test says the word it means.
func (f *FakeRunner) answer(req Request) string {
	if f.Exact {
		return f.Reply
	}
	// A task that asks what the session understood gets an understanding rather than whatever this
	// double was going to say next, for the reason the outcome line exists: the double follows the
	// task it was handed.
	if strings.Contains(req.Text, DesignAsk) && !strings.Contains(f.Reply, DesignMarker) {
		return FakeDesign
	}
	if strings.Contains(req.Text, UnderstandingAsk) && !strings.Contains(f.Reply, UnderstandingMarker) {
		return FakeUnderstanding
	}
	if strings.Contains(req.Text, TestAsk) && !strings.Contains(f.Reply, TestMarker) {
		return statingTheOutcome(FakeTestReport(req.Text), req.Text)
	}
	if strings.Contains(req.Text, BuildAsk) && !strings.Contains(f.Reply, BuildMarker) {
		return statingTheOutcome(FakeBuildReport(req.Text), req.Text)
	}
	return statingTheOutcome(f.Reply, req.Text)
}

// statingTheOutcome ends an answer the way a session that read its task ends one: every job tells its
// session to state an outcome on a line of its own, and a job whose answer states none does not
// settle.
func statingTheOutcome(reply, asked string) string {
	if !strings.Contains(asked, OutcomeMarker) || strings.Contains(reply, OutcomeMarker) {
		return reply
	}
	return reply + "\n\n" + OutcomeMarker + " " + FakeOutcome
}
