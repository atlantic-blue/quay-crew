package model

import (
	"context"
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

// answer is what the double says, which follows the task it was handed the way a model does.
//
// A task that asks for an outcome gets one. Every job says so beside its brief, so a double that
// ignored it would be looser than the thing it stands in for: every job would stop, and every test
// about a job would be a test about that. A reply that already states an outcome is left alone, which
// is how a test says the word it means.
func (f *FakeRunner) answer(req Request) string {
	if f.Exact || !strings.Contains(req.Text, OutcomeMarker) || strings.Contains(f.Reply, OutcomeMarker) {
		return f.Reply
	}
	return f.Reply + "\n\n" + OutcomeMarker + " " + FakeOutcome
}
