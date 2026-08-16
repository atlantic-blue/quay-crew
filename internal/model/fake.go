package model

import (
	"context"
	"sync"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// FakeRunner is a Runner for tests. It records the last request and returns a canned response.
type FakeRunner struct {
	Reply     string
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
	sessionID := f.SessionID
	if sessionID == "" {
		sessionID = "fake-session"
	}
	return Response{Reply: f.Reply, ModelSessionID: sessionID}, nil
}
