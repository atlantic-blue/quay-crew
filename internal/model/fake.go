package model

import (
	"context"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// FakeRunner is a Runner for tests. It records the last request and returns a canned response.
type FakeRunner struct {
	Reply     string
	SessionID string
	Err       error
	LastReq   Request
	// Takes is how long a turn pretends to take. Zero is instant, which is right for almost every
	// test and wrong for any test about something happening while a turn is under way: with an
	// instant model a whole automation finishes before a second command can be typed, and a test
	// of stopping one would be racing rather than testing.
	Takes time.Duration
}

// compile time check.
var _ Runner = (*FakeRunner)(nil)

// Run records the request and returns the canned response (or Err). The sandbox is ignored.
func (f *FakeRunner) Run(ctx context.Context, _ sandbox.Sandbox, req Request) (Response, error) {
	f.LastReq = req
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
