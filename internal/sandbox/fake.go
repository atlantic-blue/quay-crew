package sandbox

import (
	"context"
	"io"
	"strings"
	"sync"
)

// FakeProvider hands out a FakeSandbox and records what it was asked to create. For tests.
type FakeProvider struct {
	Output string
	mu     sync.Mutex
	// Created records the configuration of each sandbox actually made, in order, so a test can assert
	// which session, project and workspace it belongs to and what environment it carries. Adopting an
	// existing one is not a creation and does not appear here, which is what lets a scenario say a
	// second turn made no second sandbox.
	Created []Config
	// Calls records every request, adopted or not, for a test that cares how often it was asked.
	Calls []Config
	Boxes []*FakeSandbox
	// live is the sandbox each session currently has, so creating twice for one session adopts it.
	live map[string]*FakeSandbox
}

var _ Provider = (*FakeProvider)(nil)

// Create records the configuration and returns a FakeSandbox streaming the canned Output.
//
// A session it has already created, and which has not been closed, gets that same sandbox back. The
// real Docker provider adopts the container already carrying that name, and a double that is looser
// than the thing it stands in for manufactures green: this one used to hand out two sandboxes for one
// session, which is why the suite passed while the daemon refused the duplicate name.
func (f *FakeProvider) Create(_ context.Context, cfg Config) (Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, cfg)
	if box, live := f.live[cfg.ID]; live && !box.Closed {
		return box, nil
	}
	f.Created = append(f.Created, cfg)
	box := &FakeSandbox{Output: f.Output}
	if f.live == nil {
		f.live = make(map[string]*FakeSandbox)
	}
	f.live[cfg.ID] = box
	f.Boxes = append(f.Boxes, box)
	return box, nil
}

// FakeSandbox is a Sandbox for tests. It records exec specs, streams a canned output, and tracks
// whether it was closed.
type FakeSandbox struct {
	Output   string
	Err      error
	LastSpec Spec
	Closed   bool
}

var _ Sandbox = (*FakeSandbox)(nil)

type readerProcess struct{ r io.Reader }

func (p readerProcess) Stdout() io.Reader { return p.r }
func (p readerProcess) Wait() error       { return nil }

// Exec records the spec and returns a process streaming the canned Output.
func (f *FakeSandbox) Exec(_ context.Context, spec Spec) (Process, error) {
	f.LastSpec = spec
	if f.Err != nil {
		return nil, f.Err
	}
	return readerProcess{r: strings.NewReader(f.Output)}, nil
}

// Close marks the sandbox closed.
func (f *FakeSandbox) Close(context.Context) error {
	f.Closed = true
	return nil
}
