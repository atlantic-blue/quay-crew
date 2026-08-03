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
	// Created records each sandbox's configuration in order, so a test can assert which session,
	// project and workspace it belongs to and what environment it carries.
	Created []Config
	Boxes   []*FakeSandbox
}

var _ Provider = (*FakeProvider)(nil)

// Create records the configuration and returns a new FakeSandbox streaming the canned Output.
func (f *FakeProvider) Create(_ context.Context, cfg Config) (Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Created = append(f.Created, cfg)
	box := &FakeSandbox{Output: f.Output}
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
