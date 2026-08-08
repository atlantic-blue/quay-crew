package sandbox

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
)

// FakeProvider hands out a FakeSandbox and records what it was asked to create. For tests.
type FakeProvider struct {
	Output string
	// Missing are the binaries the image behind this provider does not carry, so a scenario can be a
	// crew whose skill needs something the sandbox cannot run.
	Missing []string
	// Stderr and ExitErr are handed to every sandbox this makes, so a scenario can say the command
	// inside failed and what it said about it.
	Stderr  string
	ExitErr error
	mu      sync.Mutex
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
	box := &FakeSandbox{Output: f.Output, Stderr: f.Stderr, ExitErr: f.ExitErr}
	box.Without(f.Missing...)
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
	Output string
	Err    error
	// Stderr is what the command inside writes about going wrong, and ExitErr is it failing. They
	// are separate because a command can fail silently, which is the case worth being able to write
	// a scenario for.
	Stderr   string
	ExitErr  error
	LastSpec Spec
	// Ran is every command this sandbox was asked to run, so a scenario can say a skill's setup was
	// run, or was not run twice.
	Ran    []Spec
	Closed bool
	// without are the binaries this sandbox does not have, so a scenario can be a session whose image
	// is missing what a skill needs.
	without map[string]bool
}

// Without makes this sandbox one whose image does not carry these commands.
func (f *FakeSandbox) Without(binaries ...string) {
	if f.without == nil {
		f.without = map[string]bool{}
	}
	for _, binary := range binaries {
		f.without[binary] = true
	}
}

var _ Sandbox = (*FakeSandbox)(nil)

type readerProcess struct {
	r      io.Reader
	stderr string
	err    error
}

func (p readerProcess) Stdout() io.Reader { return p.r }
func (p readerProcess) Wait() error       { return p.err }
func (p readerProcess) Stderr() string    { return p.stderr }

// Exec records the spec and returns a process streaming the canned Output.
func (f *FakeSandbox) Exec(_ context.Context, spec Spec) (Process, error) {
	f.LastSpec = spec
	f.Ran = append(f.Ran, spec)
	if f.Err != nil {
		return nil, f.Err
	}
	// Looking for a command answers like the real thing, because a double that says yes to every
	// binary makes a crew look ready for a skill the image cannot run. The real shell exits non zero
	// when `command -v` finds nothing.
	if binary, asking := wantedBinary(spec); asking {
		if f.without[binary] {
			return readerProcess{r: strings.NewReader(""), err: errNotFound}, nil
		}
		return readerProcess{r: strings.NewReader("/usr/bin/" + binary)}, nil
	}
	return readerProcess{r: strings.NewReader(f.Output), stderr: f.Stderr, err: f.ExitErr}, nil
}

// errNotFound is what a shell does when `command -v` finds nothing: it says nothing and exits non
// zero.
var errNotFound = errors.New("exit status 1")

// wantedBinary reads a `command -v <name>` out of a spec, which is how the crew asks a sandbox what
// it has.
func wantedBinary(spec Spec) (string, bool) {
	if len(spec.Argv) != 3 || spec.Argv[0] != "sh" || spec.Argv[1] != "-c" {
		return "", false
	}
	after, found := strings.CutPrefix(spec.Argv[2], "command -v ")
	if !found {
		return "", false
	}
	return strings.TrimSpace(after), true
}

// Close marks the sandbox closed.
func (f *FakeSandbox) Close(context.Context) error {
	f.Closed = true
	return nil
}
