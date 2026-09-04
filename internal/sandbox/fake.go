package sandbox

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
)

// FakeProvider hands out a FakeSandbox and records what it was asked to create. For tests.
type FakeProvider struct {
	Output string
	// Missing are the binaries the image behind this provider does not carry, so a scenario can be a
	// system whose skill needs something the sandbox cannot run.
	Missing []string
	// Stderr and ExitErr are handed to every sandbox this makes, so a scenario can say the command
	// inside failed and what it said about it.
	Stderr  string
	ExitErr error
	mu      sync.Mutex
	// Created records the configuration of each sandbox actually made, in order, so a test can assert
	// which session, project and workspace it belongs to and what environment it carries. Adopting an
	// existing one is not a creation and does not appear here, which is what lets a scenario say a
	// second exec made no second sandbox.
	Created []Config
	// Calls records every request, adopted or not, for a test that cares how often it was asked.
	Calls []Config
	// Removed records every session torn down by name rather than through a handle, which is what
	// the control plane does when its map is empty after a restart.
	Removed []string
	// Hold makes every creation wait until it is closed, so a test can be a daemon that has taken
	// the request and not answered. Nil creates straight away.
	Hold  chan struct{}
	Boxes []*FakeSandbox
	// Watched are the sessions somebody has the conversation open in, so a scenario can be an
	// operator typing into a container the system is about to take back.
	Watched map[string]bool
	// watchAll answers yes for every session, whether or not this provider has seen it yet. See
	// WatchEverything.
	watchAll bool
	// AttachErr is the daemon refusing to answer whether anybody is attached, which is not the same
	// answer as nobody: a system that cannot tell must leave the container alone.
	AttachErr error
	// Running are the sessions whose sandbox holds a model runtime with nobody watching it, so a
	// scenario can be a conversation answering while the listing decides what to call it.
	Running map[string]bool
	// RuntimeErr is the daemon refusing to say what a container is running, which is not the same
	// answer as nothing: a system that cannot tell must not report the session as idle.
	RuntimeErr error
	// ExistingErr is the daemon refusing to say whether this session still has a container, which is
	// not the same answer as none: a system that cannot tell must not report the work as unreachable.
	ExistingErr error
	// Replies are what particular commands answer, handed to every sandbox this makes. A scenario
	// about what the system does with a session's git needs its commands to answer differently from
	// each other, and a double with one canned answer for everything cannot say anything about that.
	Replies []Reply
	// questions counts what has been asked about the inside of a sandbox, attachment and runtime
	// alike, so a test can hold how much one listing costs.
	questions int
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
func (f *FakeProvider) Create(ctx context.Context, cfg Config) (Sandbox, error) {
	// The request is recorded before the hold, so a test can tell that the provider was reached from
	// one that has not been, and the context is honoured while waiting: the real provider runs the
	// daemon through it and gives up when it is done. A double that creates whatever the caller's
	// budget says makes a suite green over a system that waits without end.
	f.mu.Lock()
	f.Calls = append(f.Calls, cfg)
	hold := f.Hold
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if box, live := f.live[cfg.ID]; live && !box.Closed {
		return box, nil
	}
	f.Created = append(f.Created, cfg)
	box := &FakeSandbox{Output: f.Output, Stderr: f.Stderr, ExitErr: f.ExitErr, Replies: f.Replies}
	box.Without(f.Missing...)
	if f.live == nil {
		f.live = make(map[string]*FakeSandbox)
	}
	f.live[cfg.ID] = box
	f.Boxes = append(f.Boxes, box)
	return box, nil
}

// Asked is how many creations this provider has been asked for, adopted or not, read under its own
// lock for a test watching from another goroutine.
func (f *FakeProvider) Asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// Questions is how many times a sandbox has been asked what is inside it, counting both the
// attachment question and the runtime one. A test holds the cost rule with it: a listing asks once
// per row that would otherwise read idle, and nothing at all for any other row.
func (f *FakeProvider) Questions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.questions
}

// Remove closes and forgets the session's sandbox, held or not, the way the Docker provider removes
// a container by its deterministic name. Absent is success there, so it is success here.
func (f *FakeProvider) Remove(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Removed = append(f.Removed, sessionID)
	if box, live := f.live[sessionID]; live {
		box.Closed = true
		delete(f.live, sessionID)
	}
	return nil
}

// Configurations is what this provider was asked to make, in order.
//
// The same list as Created, read under the provider's own lock, for a test watching from another
// goroutine: a scenario about what a session's own directory holds runs while a turn is in flight,
// and reading the slice directly there is a race.
func (f *FakeProvider) Configurations() []Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Config(nil), f.Created...)
}

// Stranded lists the sessions whose sandboxes are still open, the way the Docker provider lists the
// containers the daemon holds.
func (f *FakeProvider) Stranded(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.live))
	for id, box := range f.live {
		if !box.Closed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Attached says whether somebody has this session's conversation open, the way the Docker provider
// asks tmux inside the container.
//
// It answers false for a session this provider holds no sandbox for, because the real one asks a
// container that is not there and gets a non zero exit, which means nobody rather than an error. A
// double looser than that would let a scenario reclaim a container the real system would leave alone.
func (f *FakeProvider) Attached(_ context.Context, sessionID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questions++
	if f.AttachErr != nil {
		return false, f.AttachErr
	}
	if box, live := f.live[sessionID]; !live || box.Closed {
		return false, nil
	}
	return f.watchAll || f.Watched[sessionID], nil
}

// Watch makes this provider one where somebody has that session's conversation open.
func (f *FakeProvider) Watch(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Watched == nil {
		f.Watched = map[string]bool{}
	}
	f.Watched[sessionID] = true
}

// RuntimeRunning says whether a model runtime is up in this session's sandbox, the way the Docker
// provider reads the container's own process table.
//
// False for a session this provider holds no sandbox for, because the real one asks a container that
// is not there and gets a non zero exit, which means nothing is running rather than an error.
func (f *FakeProvider) RuntimeRunning(_ context.Context, sessionID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questions++
	if f.RuntimeErr != nil {
		return false, f.RuntimeErr
	}
	if box, live := f.live[sessionID]; !live || box.Closed {
		return false, nil
	}
	return f.Running[sessionID], nil
}

// Wake makes this provider one where that session's sandbox holds a running model runtime, which is
// a conversation answering with nobody watching it.
func (f *FakeProvider) Wake(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Running == nil {
		f.Running = map[string]bool{}
	}
	f.Running[sessionID] = true
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
	// Replies are answers to particular commands, tried in order before Output. The first whose Match
	// is in the command line wins.
	Replies []Reply
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
	// binary makes a system look ready for a skill the image cannot run. The real shell exits non zero
	// when `command -v` finds nothing.
	if binary, asking := wantedBinary(spec); asking {
		if f.without[binary] {
			return readerProcess{r: strings.NewReader(""), err: errNotFound}, nil
		}
		return readerProcess{r: strings.NewReader("/usr/bin/" + binary)}, nil
	}
	if reply, set := replyTo(f.Replies, spec); set {
		return readerProcess{r: strings.NewReader(reply.Out), stderr: reply.Stderr, err: reply.Err}, nil
	}
	return readerProcess{r: strings.NewReader(f.Output), stderr: f.Stderr, err: f.ExitErr}, nil
}

// Reply is what one command answers, matched on a fragment of the command line rather than on the
// whole of it: the paths in a command are made by the system and a scenario should not have to know
// them to say what git said.
type Reply struct {
	// Match is the fragment. An empty one matches nothing, so a half written scenario answers with
	// the sandbox's own Output rather than with every command at once.
	Match  string
	Out    string
	Stderr string
	Err    error
}

// replyTo is the canned answer for this command, and false where none was set for it.
func replyTo(replies []Reply, spec Spec) (Reply, bool) {
	line := strings.Join(spec.Argv, " ")
	for _, reply := range replies {
		if reply.Match != "" && strings.Contains(line, reply.Match) {
			return reply, true
		}
	}
	return Reply{}, false
}

// errNotFound is what a shell does when `command -v` finds nothing: it says nothing and exits non
// zero.
var errNotFound = errors.New("exit status 1")

// wantedBinary reads a `command -v <name>` out of a spec, which is how the system asks a sandbox what
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

// Existing is the sandbox this session already has, and false where it has none, the way the Docker
// provider finds a container by name and finds nothing.
//
// It never creates one, which is the property worth holding a double to: a system that made a
// container to look inside a finished session would cost a machine a sandbox and read an empty
// directory, and a double that quietly created one would leave that untested.
func (f *FakeProvider) Existing(_ context.Context, sessionID string) (Sandbox, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ExistingErr != nil {
		return nil, false, f.ExistingErr
	}
	box, live := f.live[sessionID]
	if !live || box.Closed {
		return nil, false, nil
	}
	return box, true, nil
}

// WatchEverything makes this provider one where somebody has every session's conversation open,
// which is the one state in which a system may not take a container back however stopped its queue
// is. It is set rather than listed because a caller that watched each session as it appeared would
// be racing the tick that is about to reclaim one.
func (f *FakeProvider) WatchEverything() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchAll = true
}
