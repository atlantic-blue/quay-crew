// Package headroom says how much room the machine has left for another session.
//
// The crew knew nothing about the machine it ran on. On 27 August 2026 the host ran out of memory
// and the kernel killed eighteen sandboxes, three monitors and a build in one event. Nothing in quay
// reported it before, during or after: the console kept drawing a healthy crew, and every number
// that mattered had to be read from outside quay with `docker stats`. See issue 405.
//
// So this reads the daemon the control plane already talks to and reports four things: what every
// container holds, the limit that binds, what each sandbox holds, and the memory pressure on the
// machine the daemon runs on. Two of those are different questions and the incident is why. Docker
// sat at less than half its cap the whole time while the machine it ran on was at 94 per cent of its
// swap, so a crew that reported only the daemon's own figure would have said there was room.
//
// Nothing here estimates. A figure is measured or it is unknown, and unknown is printed as the word
// rather than as a zero. An operator stops a session on these numbers, and a number the crew guessed
// is a session stopped for nothing.
package headroom

import (
	"fmt"
	"time"
)

// A Figure is a byte count the crew measured, or the absence of one. There is no third case on
// purpose: a crew that fills a gap with an estimate hands the operator a number to act on that
// nothing measured.
type Figure struct {
	bytes int64
	known bool
}

// Measured is a figure the crew read from the machine.
func Measured(bytes int64) Figure { return Figure{bytes: bytes, known: true} }

// Unknown is the answer when the crew could not read it. It is a value rather than an error,
// because one figure the daemon would not give up must never stop the rest being reported.
func Unknown() Figure { return Figure{} }

// Known says whether anything measured this.
func (f Figure) Known() bool { return f.known }

// Bytes is what was measured, and zero when nothing was. Read Known first: zero bytes and no reading
// at all are different states, and only one of them means the machine is empty.
func (f Figure) Bytes() int64 { return f.bytes }

// String states the figure the way a memory limit is typed: whole mebibytes, so a number read here
// can go straight into a heap cap. An unmeasured figure is the word, never a zero.
func (f Figure) String() string {
	if !f.known {
		return "unknown"
	}
	return fmt.Sprintf("%d MiB", f.bytes/(1<<20))
}

// A Share is a fraction of one processor, as the daemon reports it, or the absence of one.
type Share struct {
	percent float64
	known   bool
}

// MeasuredShare is a processor share the daemon reported, in per cent of one processor.
func MeasuredShare(percent float64) Share { return Share{percent: percent, known: true} }

// UnknownShare is the answer when the daemon did not report one.
func UnknownShare() Share { return Share{} }

// Known says whether anything measured this.
func (s Share) Known() bool { return s.known }

// Percent is what was measured, and zero when nothing was.
func (s Share) Percent() float64 { return s.percent }

func (s Share) String() string {
	if !s.known {
		return "unknown"
	}
	return fmt.Sprintf("%.1f%%", s.percent)
}

// The three words the header says about the binding limit. Full has to be readable without reading
// the number beside it, which is why it is a word and not a percentage.
const (
	// StateRoom means another session will start.
	StateRoom = "room"
	// StateTight means another session may start and may take the machine with it.
	StateTight = "tight"
	// StateFull means do not start another session.
	StateFull = "full"
	// StateUnknown means the crew could not read the machine. It is never "room": a crew that
	// reports room it did not measure is the crew that drew a healthy header through eighteen kills.
	StateUnknown = "unknown"
)

// Where one word stops and the next begins, as fractions of the limit that binds.
//
// Fractions rather than byte counts, because the limit is different on every machine: a Mac with a
// 7.8 gigabyte cap on its Docker virtual machine and a Linux host with 64 gigabytes cannot share a
// number. Both are provisional. The measurement that replaces them is the fraction of the binding
// limit at which a sandbox start first fails on a machine, over the first fifty starts. Until that
// run exists these two fractions are a judgement, and `docs/OBSERVABILITY.md` says so.
const (
	// TightFrom is three quarters of the binding limit.
	TightFrom = 0.75
	// FullFrom is nine tenths of it.
	FullFrom = 0.90
)

// A Machine is the memory pressure on the machine the daemon runs on, which is a different question
// from what the daemon may hold.
//
// On a Mac that machine is the Docker virtual machine and not the Mac, because nothing inside a
// Linux container can read what macOS is doing. The crew names the machine it actually read rather
// than claiming to know the one it did not.
type Machine struct {
	// Name is what the daemon calls the operating system it runs on, for example "Docker Desktop".
	// Empty means the daemon did not say.
	Name string
	// Total and Available are the machine's own memory accounting.
	Total     Figure
	Available Figure
	// SwapTotal and SwapUsed are the pressure signal. The kill on 27 August came from a machine at
	// 94 per cent of its swap while the daemon held less than half its cap.
	SwapTotal Figure
	SwapUsed  Figure
}

// SwapFraction is how much of the machine's swap is in use, and false when either figure is missing.
func (m Machine) SwapFraction() (float64, bool) {
	if !m.SwapTotal.Known() || !m.SwapUsed.Known() || m.SwapTotal.Bytes() <= 0 {
		return 0, false
	}
	return float64(m.SwapUsed.Bytes()) / float64(m.SwapTotal.Bytes()), true
}

// A Sandbox is one session's container, as the daemon reports it.
type Sandbox struct {
	// Session is the session identifier the container is named after.
	Session string
	// Held is the memory the container holds now.
	Held Figure
	// Processor is its share of one processor.
	Processor Share
	// Idle is how long since the session's last task landed, and false when the crew does not know.
	// The daemon cannot answer this: it comes from the session row beside the container.
	Idle      time.Duration
	IdleKnown bool
}

// A Sample is one look at the machine, taken on the sampler's own timer and read by everything else.
type Sample struct {
	// Used is what every container on the daemon holds, added up. Every container rather than the
	// crew's own, because the cap binds all of them and the question is whether another sandbox will
	// start.
	Used Figure
	// Limit is the memory the daemon may hold. On a Mac that is the Docker virtual machine's cap and
	// not the machine's memory. A machine with 36 gigabytes and a 7.8 gigabyte cap is full at 7.8.
	Limit Figure
	// Machine is the pressure on the machine the daemon runs on.
	Machine Machine
	// Sandboxes is one entry per session container, in the order the source returned them.
	Sandboxes []Sandbox
	// TakenAt is when this sample was taken. The zero time means no sample has ever landed, which is
	// what a crew reports before its first tick and after a daemon that never answered.
	TakenAt time.Time
	// Failed is what went wrong the last time the crew tried, empty when the last try worked. It is
	// carried rather than returned, because a collection that failed must never fail a command.
	Failed string
}

// Taken says whether this sample came from a real reading.
func (s Sample) Taken() bool { return !s.TakenAt.IsZero() }

// Free is what the daemon may still hand out, and unknown unless both sides were measured.
func (s Sample) Free() Figure {
	if !s.Used.Known() || !s.Limit.Known() {
		return Unknown()
	}
	if s.Limit.Bytes() < s.Used.Bytes() {
		return Measured(0)
	}
	return Measured(s.Limit.Bytes() - s.Used.Bytes())
}

// Fraction is how much of the binding limit is in use, and false unless both sides were measured.
func (s Sample) Fraction() (float64, bool) {
	if !s.Used.Known() || !s.Limit.Known() || s.Limit.Bytes() <= 0 {
		return 0, false
	}
	return float64(s.Used.Bytes()) / float64(s.Limit.Bytes()), true
}

// State is the one word the header carries. A sample the crew could not take is unknown rather than
// room, because the header that said everything was fine through eighteen kills is the reason this
// package exists.
func (s Sample) State() string {
	fraction, known := s.Fraction()
	if !known {
		return StateUnknown
	}
	switch {
	case fraction >= FullFrom:
		return StateFull
	case fraction >= TightFrom:
		return StateTight
	default:
		return StateRoom
	}
}

// Line is the header's whole contribution: one figure and one word. The header is one row and it
// redraws every second, so it gets this and the detail lives in a view.
func (s Sample) Line() string {
	return fmt.Sprintf("%s of %s   %s", s.Used, s.Limit, s.State())
}
