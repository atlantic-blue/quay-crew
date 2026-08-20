package room

import (
	"fmt"
	"strings"
)

// Say writes a reading out for whoever asked, which is a session about to run a gate.
//
// It says three things, in this order: what this sandbox actually has, what an out of memory killer
// has already taken in it, and what to do about a gate that does not fit. The last part is here
// rather than in each session's memory, so the answer is the same every time instead of being
// invented once per session.
func Say(reading Reading) string {
	var out strings.Builder

	if reading.Limited() {
		fmt.Fprintf(&out, "this sandbox may take %s, and holds %s of it.\n\n",
			megabytes(reading.Limit), megabytes(reading.Held))
		fmt.Fprintf(&out, "  its limit             %10s   what node, go, jest and webpack read\n",
			megabytes(reading.Limit))
	} else {
		out.WriteString("this sandbox has no memory limit of its own. So everything in it sizes " +
			"itself against\nthe whole machine, and the machine is shared.\n\n")
		fmt.Fprintf(&out, "  it advertises         %10s   what node, go, jest and webpack budget against\n",
			megabytes(reading.Total))
	}
	fmt.Fprintf(&out, "  free right now        %10s   what the kernel kills against\n",
		megabytes(reading.Free()))
	fmt.Fprintf(&out, "  it holds              %10s\n", megabytes(reading.Held))
	if reading.Peak > 0 {
		fmt.Fprintf(&out, "  the most it has held  %10s\n", megabytes(reading.Peak))
	}

	if killed := kills(reading); killed != "" {
		out.WriteString("\n" + killed + "\n")
	}

	out.WriteString(`
A gate that grows past what is free is killed with signal 9. A shell reports that as exit 137 and
says nothing else, so it reads as a hang.

Before you run a gate that does not fit:
  cap the heap under what is free, with NODE_OPTIONS=--max-old-space-size=<megabytes>
  cap Go the same way, with GOMEMLIMIT=<megabytes>MiB
  take one worker instead of one for each processor: jest --maxWorkers=1, go test -p 1
  run the gate over part of the tree, and name the part you ran

If it still does not fit, say what you could not run. Then ask the operator for more memory. Never
report a partial check as a check.
`)
	return out.String()
}

// kills says what an out of memory killer has already taken in this sandbox, and which memory ran
// out. A kill by the machine's own killer raises the kill count and leaves the limit count at zero,
// because a sandbox with no limit never reaches one. That pair is the only thing inside a container
// that tells the two apart, because the kernel log is not readable in here.
func kills(reading Reading) string {
	if reading.Kills == 0 {
		return ""
	}
	taken := fmt.Sprintf("An out of memory killer took %d processes in this sandbox.", reading.Kills)
	if reading.Kills == 1 {
		taken = "An out of memory killer took a process in this sandbox."
	}
	if reading.AtLimit == 0 {
		return taken + " This sandbox never reached a limit of its\nown, so the machine ran out rather than this session."
	}
	return taken + " This sandbox reached its own limit, so this\nsession ran out."
}

// megabytes states a byte count the way the tools in a sandbox take one, so a number read here can be
// typed straight into a heap cap.
func megabytes(value int64) string {
	return fmt.Sprintf("%d MiB", value/(1<<20))
}
