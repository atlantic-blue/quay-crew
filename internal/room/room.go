// Package room says how much memory a sandbox actually has.
//
// It exists because the number a sandbox advertises is not the number it is killed against. A
// container with no limit of its own reports the whole machine in /proc/meminfo, so node sizes its
// heap from it, Go sizes its garbage collector from it, and jest and webpack start one worker per
// processor. What is really there is whatever the rest of the machine has not taken. A session
// budgets against the first number and is killed against the second.
//
// The kill arrives as signal 9 and nothing else. A shell reports exit 137, the kernel log is not
// readable from inside a container, and the session is left reporting a partial check.
//
// So this reads the machine's own accounting and says what is true: what the sandbox advertises,
// what is free, whether there is a limit at all, and whether anything in here has already been
// killed for memory. A session that reads it before a gate can choose a smaller command instead of
// being killed by one.
package room

import (
	"bufio"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// Where the machine keeps its memory accounting, relative to the root this reads from.
const (
	meminfoPath = "proc/meminfo"
	cgroupDir   = "sys/fs/cgroup"
)

// A Reading is one look at a sandbox's memory, in bytes.
type Reading struct {
	// Total is MemTotal: what every tool in this sandbox sizes itself against.
	Total int64
	// Available is MemAvailable: what the machine can still give out without swapping.
	Available int64
	// Limit is the sandbox's own memory limit, and zero when it has none. A sandbox with no limit
	// advertises the whole machine to everything running in it.
	Limit int64
	// Held is what this sandbox is using now.
	Held int64
	// Peak is the most it has used since it started, and zero where the kernel does not keep it.
	Peak int64
	// Kills is how many processes in this sandbox an out of memory killer has taken.
	Kills int64
	// AtLimit is how many times this sandbox was held at a limit of its own.
	//
	// The pair is what says who ran out. A kill by the machine's own out of memory killer raises
	// Kills and leaves this at zero, because the sandbox never reached a limit: there was not one.
	AtLimit int64
}

// Free is what this sandbox can still take: what the machine has, and never more than what is left
// of a limit.
func (r Reading) Free() int64 {
	if r.Limit > 0 && r.Limit-r.Held < r.Available {
		return r.Limit - r.Held
	}
	return r.Available
}

// Limited says whether this sandbox carries a memory limit of its own.
func (r Reading) Limited() bool { return r.Limit > 0 }

// Read takes one reading from a machine's own accounting, under root.
//
// Only /proc/meminfo is required. The control group files are read where they are there and left at
// zero where they are not, because a sandbox is not the only place this runs and a missing file is
// an answer rather than a failure.
func Read(root fs.FS) (Reading, error) {
	var reading Reading

	meminfo, err := fs.ReadFile(root, meminfoPath)
	if err != nil {
		return Reading{}, fmt.Errorf("room: this reads a linux sandbox's own memory accounting, "+
			"and there is none here: %w", err)
	}
	reading.Total = kilobytes(meminfo, "MemTotal")
	reading.Available = kilobytes(meminfo, "MemAvailable")
	if reading.Total == 0 {
		return Reading{}, fmt.Errorf("room: %s says no MemTotal, so there is nothing to report", meminfoPath)
	}

	reading.Limit = bytes(root, "memory.max")
	reading.Held = bytes(root, "memory.current")
	reading.Peak = bytes(root, "memory.peak")
	if events, err := fs.ReadFile(root, cgroupDir+"/memory.events"); err == nil {
		reading.Kills = counter(events, "oom_kill")
		reading.AtLimit = counter(events, "max")
	}
	return reading, nil
}

// bytes reads one control group file that holds a byte count. "max" is how the kernel spells no
// limit, and it reads as zero here so that "has a limit" is one comparison rather than two.
func bytes(root fs.FS, name string) int64 {
	body, err := fs.ReadFile(root, cgroupDir+"/"+name)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// kilobytes reads a named line of /proc/meminfo, which states kilobytes, and returns bytes.
func kilobytes(meminfo []byte, name string) int64 {
	for _, line := range lines(meminfo) {
		label, rest, found := strings.Cut(line, ":")
		if !found || label != name {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0
		}
		value, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return value * 1024
	}
	return 0
}

// counter reads a named line of memory.events, which is "<name> <count>" a line.
func counter(events []byte, name string) int64 {
	for _, line := range lines(events) {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != name {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return value
	}
	return 0
}

func lines(body []byte) []string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	return out
}
