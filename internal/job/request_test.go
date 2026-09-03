package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The brief that is faithful to its request is the first test, and the one that matters most.
//
// A check that fires on every brief passes every test about finding drift and is worth nothing: it
// puts a person in front of every job, which is the cost the whole system exists to remove. So the
// silence is asserted before the speech.
func TestAFaithfulBriefIsNotReportedAsDrifted(t *testing.T) {
	request := "paste a youtube link and get the text back"
	brief := "Build the page a reader pastes a YouTube link into. It fetches the transcript for that " +
		"link and renders the text back on the same page. Where there is no transcript, say so."
	if drifted := job.Drifted(request, brief); drifted != "" {
		t.Errorf("a brief that carries what was asked for was reported as drifted: %s", drifted)
	}
	covered, missing := job.Covered(request, brief)
	if covered < job.DriftThreshold {
		t.Errorf("a faithful brief covers %.3f of its request, want at least %.3f (missing %v)",
			covered, job.DriftThreshold, missing)
	}
}

// The measured incident: the design the crew was given took a video identifier, and the request was
// to paste a link. Every job was faithful to the design and the product was not the one wanted.
func TestABriefThatDropsWhatWasAskedForIsReported(t *testing.T) {
	request := "paste a youtube link and get the text back"
	brief := "Build a page that serves a transcript archive. The address reads /videos?id=<video id>, " +
		"and the video identifier is the key the store is read by. A reader supplies the identifier and " +
		"the page renders the stored transcript for it."
	drifted := job.Drifted(request, brief)
	if drifted == "" {
		t.Fatal("the brief that cost two days was not reported as drifted")
	}
	// The words are the whole product of the line. A reader told a number goes looking for what it
	// means; a reader told which words are missing has already read the finding.
	for _, word := range []string{"paste", "link", "text"} {
		if !strings.Contains(drifted, word) {
			t.Errorf("the report does not name %q, which the brief never says: %s", word, drifted)
		}
	}
}

// The other measured incident. A request for an article about what had been built became a brief for
// a diary of throughput, and the product sentence agreed with both of them.
func TestTheArticleThatBecameADiaryOfThroughput(t *testing.T) {
	request := "write an article about what we built this week"
	brief := "Write a 1500 word post for the engineering blog. Open with the number of pull requests " +
		"merged this week, then a section per day covering how many jobs ran, the token spend, and the " +
		"sessions that failed. Close with the throughput trend."
	if job.Drifted(request, brief) == "" {
		t.Error("the article brief was not reported as drifted")
	}
	// Product cannot catch this, which is why the request is a field of its own rather than a longer
	// product sentence.
	if job.Drifted("a reader opens the post", brief) != "" {
		t.Error("the product sentence caught it, so the request field is not needed; check the premise")
	}
}

// A job that states no request behaves exactly as it did before this shipped.
func TestNoRequestSaysNothing(t *testing.T) {
	if drifted := job.Drifted("", "a brief about anything at all"); drifted != "" {
		t.Errorf("a job that stated no request was reported as drifted: %s", drifted)
	}
	if asked := job.AskedInTheseWords("", "a brief"); asked != "" {
		t.Errorf("a job that stated no request put a line in front of its session: %s", asked)
	}
	if covered, _ := job.Covered("", "a brief"); covered != 1 {
		t.Errorf("a request nobody stated covers %.3f, want 1", covered)
	}
}

// Two words that are one word in two shapes are one word. A request saying pasting and a brief saying
// paste are saying the same thing, and reporting that as a dropped word is a line nobody trusts.
func TestOneWordInTwoShapesIsOneWord(t *testing.T) {
	for _, one := range []struct{ request, brief string }{
		{"pasting a link", "the reader pastes a link"},
		{"a reader pastes links", "one reader, paste one link"},
		{"rendering the transcripts", "it renders a transcript"},
	} {
		if _, missing := job.Covered(one.request, one.brief); len(missing) > 0 {
			t.Errorf("%q held against %q reported %v as missing", one.request, one.brief, missing)
		}
	}
}

// A brief that repeats the request word for word drops nothing.
func TestABriefThatRepeatsTheRequestDropsNothing(t *testing.T) {
	request := "fix the login button on the settings page"
	covered, missing := job.Covered(request, request)
	if covered != 1 || len(missing) > 0 {
		t.Errorf("a brief repeating its request covers %.3f and misses %v, want 1 and nothing", covered, missing)
	}
}

// Said plainly in the file and said again here: a brief that keeps every word and inverts the meaning
// is not caught. The test exists so nobody reads the check as more than it is.
func TestAnInvertedBriefIsNotCaught(t *testing.T) {
	request := "delete the archived transcripts every night"
	brief := "Never delete the archived transcripts. Run every night and keep them."
	if drifted := job.Drifted(request, brief); drifted != "" {
		t.Errorf("this measure is words, not meaning, and it reported %s", drifted)
	}
}

// The session is given the request whole, because a summary of what was said is the compression that
// caused the fault.
func TestTheSessionIsGivenTheRequestWordForWord(t *testing.T) {
	request := "paste a youtube link and get the text back"
	faithful := "Build the page a reader pastes a YouTube link into, fetch the transcript for that link, " +
		"and render the text back."
	said := job.AskedInTheseWords(request, faithful)
	if !strings.Contains(said, request) {
		t.Fatalf("the session was not given the request as it was said: %s", said)
	}
	if strings.Contains(said, "the brief says nothing about") {
		t.Errorf("a faithful brief put a drift line in front of the session: %s", said)
	}
	drifted := job.AskedInTheseWords(request, "Serve the archive by video identifier.")
	if !strings.Contains(drifted, "the brief says nothing about") {
		t.Errorf("a drifted brief did not tell the session which words are missing: %s", drifted)
	}
	if !strings.Contains(drifted, "this wins") {
		t.Errorf("the session was not told the request wins over the brief: %s", drifted)
	}
}

// The task a session is handed carries the request above the brief, which is the half of this that
// works with nobody watching.
func TestTheTaskCarriesTheRequestAboveTheBrief(t *testing.T) {
	one := &job.Job{
		Brief:   "Serve the archive by video identifier.",
		Request: "paste a youtube link and get the text back",
	}
	task := job.Asked(one)
	at, brief := strings.Index(task, "paste a youtube link"), strings.Index(task, "Serve the archive")
	if at < 0 {
		t.Fatalf("the task never carried the request: %s", task)
	}
	if at > brief {
		t.Errorf("the request came after the brief rather than above it: %s", task)
	}
}

// The ceiling is the brief's, not the title's. A request held to one line would make somebody shorten
// what was said, which is the compression this exists to catch.
func TestTheRequestIsHeldToTheBriefsCeiling(t *testing.T) {
	declaration := job.Declaration{
		Workspace: "acme", Project: "acme/one", Title: "one", Brief: "do it",
		Request: strings.Repeat("a", job.RequestLimit),
	}
	if err := declaration.Validate(); err != nil {
		t.Fatalf("a request at the ceiling was refused: %v", err)
	}
	declaration.Request = strings.Repeat("a", job.RequestLimit+1)
	err := declaration.Validate()
	if err == nil {
		t.Fatal("a request over the ceiling was declared")
	}
	if !strings.Contains(err.Error(), "asked for in the words it was asked in") {
		t.Errorf("the refusal does not say why the ceiling is the brief's: %v", err)
	}
	if job.RequestLimit != job.BriefLimit {
		t.Errorf("the request ceiling is %d and the brief's is %d; they are one ceiling",
			job.RequestLimit, job.BriefLimit)
	}
}

// A one word request is the shortest thing a person says, and the measure has to survive it rather
// than dividing by nothing.
func TestAOneWordRequest(t *testing.T) {
	if covered, _ := job.Covered("transcripts", "render the transcript"); covered != 1 {
		t.Errorf("a one word request the brief says covers %.3f, want 1", covered)
	}
	if job.Drifted("dashboard", "render the transcript") == "" {
		t.Error("a one word request the brief never says was not reported")
	}
	// A request of nothing but the words every brief holds has no subject to measure, so it is silent
	// rather than dividing by nothing.
	if drifted := job.Drifted("do it for us", "anything at all"); drifted != "" {
		t.Errorf("a request with no content words was reported as drifted: %s", drifted)
	}
}
