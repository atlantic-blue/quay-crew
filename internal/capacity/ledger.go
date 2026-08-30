package capacity

import (
	"sort"
	"sync"
	"time"
)

// ReservedFor is how long a reservation lasts when no sandbox ever appears under it.
//
// A reservation is taken before the job is claimed and released by the controller on every road out
// of a start, so this is only the backstop for a controller that died between the two. It is longer
// than the system's whole dispatch budget on purpose: the incident this exists for had a sandbox take
// two minutes and seven seconds to be made, and a reservation that expired underneath it would let
// the next job in against capacity that is already spoken for.
const ReservedFor = 10 * time.Minute

// A Ledger is what the system has placed and what it has promised to place.
//
// The reservation is the part a count cannot do. A dispatch is detached, so a container appears
// seconds after the job that asked for it was admitted, and the system reads its runtime on a timer
// ten seconds wide. Nine jobs admitted inside one of those windows all measure the same empty
// machine and all fit, which is how nine went onto a machine with room for eight. So admission
// writes down what it has just promised, in the same movement as the decision, and the next job
// counts it. Kubernetes does the same thing and calls it assuming the pod onto the node.
type Ledger struct {
	// now is the clock, so a test can hold one still rather than wait a reservation out.
	now func() time.Time

	mu    sync.Mutex
	holds map[string]hold
}

// A hold is one sandbox's claim on the machine: reserved, or placed and running.
type hold struct {
	request Request
	// session is the sandbox this became, empty while it is still a promise.
	session string
	// until is when a promise nobody kept runs out. The zero time is a placed sandbox, which runs
	// out when its container goes and never on a clock.
	until time.Time
}

// NewLedger builds an empty one.
func NewLedger() *Ledger { return &Ledger{now: time.Now, holds: map[string]hold{}} }

// Clocked returns a ledger reading this clock, for a test that has to age a reservation without
// waiting ten minutes for one.
func (l *Ledger) Clocked(now func() time.Time) *Ledger {
	if now != nil {
		l.now = now
	}
	return l
}

// Reserve promises room for a sandbox that does not exist yet, under a key its placement will use
// again. Reserving twice under one key leaves one reservation, so a controller that ticks over the
// same job twice does not charge the machine for it twice.
func (l *Ledger) Reserve(key string, request Request) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if held, found := l.holds[key]; found && held.session != "" {
		// The sandbox is already there. A reservation on top of it would count it twice.
		return
	}
	l.holds[key] = hold{request: request, until: l.now().Add(ReservedFor)}
}

// Place records a sandbox that exists, under the key its reservation was taken with.
//
// Anything else holding this key or this session is dropped first, so a sandbox the system reserved
// for, adopted after a restart, and then placed is one entry throughout rather than three.
func (l *Ledger) Place(key, session string, request Request) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.forgetSession(session)
	l.holds[key] = hold{request: request, session: session}
}

// Release drops what one key holds, whether it was a promise or a container.
func (l *Ledger) Release(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.holds, key)
}

// ReleaseSession drops what a session holds, which is how a container going away gives its room
// back. The system closes a sandbox by session and not by the key it was admitted under.
func (l *Ledger) ReleaseSession(session string) {
	if l == nil || session == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.forgetSession(session)
}

// Seed records the sandboxes already running that this system never admitted, which is every one of
// them after a restart: the containers outlive the process, and a system that started counting from
// zero would admit a second machine's worth of work onto a full machine.
//
// A sandbox seeded this way is counted at the system's standard request, because what it was admitted
// under went with the process that admitted it.
func (l *Ledger) Seed(sessions []string, request Request) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, session := range sessions {
		if session == "" {
			continue
		}
		l.forgetSession(session)
		l.holds[session] = hold{request: request, session: session}
	}
}

// Placed is everything spoken for: the containers that exist and the promises still standing.
// Promises nobody kept are dropped as they are read, which is the only thing that expires them.
func (l *Ledger) Placed() Request {
	if l == nil {
		return Request{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire()
	total := Request{}
	for _, held := range l.holds {
		total = total.Plus(held.request)
	}
	return total
}

// Count is how many sandboxes are spoken for, which is what a refusal says out loud: an operator
// reading that the machine is full wants to know how many things are holding it.
func (l *Ledger) Count() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire()
	return len(l.holds)
}

// Keys is what the ledger holds, sorted, for a test and for a system saying what it is holding.
func (l *Ledger) Keys() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire()
	keys := make([]string, 0, len(l.holds))
	for key := range l.holds {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// expire drops the promises nobody kept. Called under the lock by every read.
func (l *Ledger) expire() {
	now := l.now()
	for key, held := range l.holds {
		if held.session == "" && !held.until.IsZero() && now.After(held.until) {
			delete(l.holds, key)
		}
	}
}

// forgetSession drops whatever holds this session, under whichever key it was written. Called under
// the lock.
func (l *Ledger) forgetSession(session string) {
	if session == "" {
		return
	}
	for key, held := range l.holds {
		if held.session == session {
			delete(l.holds, key)
		}
	}
}

// Without is everything spoken for except what one key holds. It is what a placement asks: the
// sandbox about to be made is already holding its reservation under this key, and counting that
// against itself would refuse every job the system has correctly admitted.
func (l *Ledger) Without(key string) Request {
	if l == nil {
		return Request{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire()
	total := Request{}
	for held := range l.holds {
		if held == key {
			continue
		}
		total = total.Plus(l.holds[held].request)
	}
	return total
}
