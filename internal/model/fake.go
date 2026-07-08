package model

import (
	"context"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// FakeRunner is a Runner for tests. It records the last request and returns a canned response.
type FakeRunner struct {
	Reply     string
	SessionID string
	Err       error
	LastReq   Request
}

// compile time check.
var _ Runner = (*FakeRunner)(nil)

// Run records the request and returns the canned response (or Err). The sandbox is ignored.
func (f *FakeRunner) Run(_ context.Context, _ sandbox.Sandbox, req Request) (Response, error) {
	f.LastReq = req
	if f.Err != nil {
		return Response{}, f.Err
	}
	sessionID := f.SessionID
	if sessionID == "" {
		sessionID = "fake-session"
	}
	return Response{Reply: f.Reply, ModelSessionID: sessionID}, nil
}
