package headroom

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/room"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// A Source takes one sample of the machine. It is an interface so the control plane can be built
// with no source at all, which is what a system running the local sandbox provider does: it then
// reports unknown rather than shelling out to a daemon that is not there.
type Source interface {
	Sample(ctx context.Context) (Sample, error)
}

// The separator between the fields of one line the daemon prints. A tab, because a container name
// and a memory figure can both hold a space and neither holds a tab.
const field = "\t"

// Daemon is a Source over the Docker command line tool the control plane already carries and
// already uses to make every sandbox.
//
// It shells out rather than dialling the daemon's own interface for the reason the sandbox provider
// gives: the tool is in the image, it is the same tool the operator runs by hand, and a figure the
// system reports can be checked against the command that produced it.
type Daemon struct {
	// Run runs one command and returns its standard output. A test replaces it. Nil takes the real
	// one.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// Root is the machine's own accounting, which the control plane reads from inside its own
	// container. Nil takes the real root.
	Root fs.FS
}

var _ Source = Daemon{}

// Sample reads the daemon twice and the machine once.
//
// It returns what it could read even when part of it failed. A sample with a limit and no sandboxes
// still answers the question the header asks, and the alternative is a header that goes blank
// because one of three reads did not answer.
func (d Daemon) Sample(ctx context.Context) (Sample, error) {
	sample := Sample{
		TakenAt: time.Now(), Used: Unknown(), Limit: Unknown(),
		Held: UnknownShare(), Processors: UnknownShare(),
	}
	var refused []string

	info, err := d.run(ctx, "docker", "info", "--format",
		"{{.MemTotal}}"+field+"{{.NCPU}}"+field+"{{.OperatingSystem}}")
	if err != nil {
		refused = append(refused, "the daemon did not say how much memory it may hold: "+err.Error())
	} else {
		sample.Limit, sample.Processors, sample.Machine.Name = parseInfo(info)
	}

	stats, err := d.run(ctx, "docker", "stats", "--no-stream", "--format",
		"{{.Name}}"+field+"{{.MemUsage}}"+field+"{{.CPUPerc}}")
	if err != nil {
		refused = append(refused, "the daemon did not say what its containers hold: "+err.Error())
	} else {
		sample.Used, sample.Held, sample.Sandboxes = parseStats(stats)
	}

	machine, err := d.machine()
	if err != nil {
		refused = append(refused, "the machine keeps no memory accounting here: "+err.Error())
	}
	machine.Name = sample.Machine.Name
	sample.Machine = machine

	if len(refused) > 0 {
		sample.Failed = strings.Join(refused, "; ")
	}
	return sample, nil
}

// machine reads the memory pressure on the machine the daemon runs on, through the same accounting
// `krewe room` reads inside a sandbox. Where that accounting is not there, every figure is unknown
// and nothing is guessed.
func (d Daemon) machine() (Machine, error) {
	root := d.Root
	if root == nil {
		root = os.DirFS("/")
	}
	reading, err := room.Read(root)
	if err != nil {
		return Machine{Total: Unknown(), Available: Unknown(), SwapTotal: Unknown(), SwapUsed: Unknown()}, err
	}
	machine := Machine{
		Total:     Measured(reading.Total),
		Available: Measured(reading.Available),
		SwapTotal: Unknown(),
		SwapUsed:  Unknown(),
	}
	// Swap is the pressure signal, and a machine with none reports a total of zero rather than
	// nothing. Zero total is a real reading, so it is measured and the used figure is measured with
	// it.
	if reading.SwapKnown {
		machine.SwapTotal = Measured(reading.SwapTotal)
		machine.SwapUsed = Measured(reading.SwapTotal - reading.SwapFree)
	}
	return machine, nil
}

func (d Daemon) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if d.Run != nil {
		return d.Run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

// parseInfo reads the two ceilings the daemon works under, and what it calls the machine it runs on.
//
// The memory is the limit that binds. On a Mac it is the Docker virtual machine's cap rather than
// the Mac's memory, which is the whole reason this figure is read from the daemon rather than from
// the machine: a Mac with 36 gigabytes and a 7.8 gigabyte cap is full at 7.8.
//
// The processors are the second ceiling. On 29 August 2026 eight jobs held 7,488 megabytes of a
// 7,653 megabyte cap and 913 per cent of a processor at the same moment, and what stopped answering
// was the daemon rather than the memory. A system that read memory alone would have admitted a ninth.
func parseInfo(out []byte) (Figure, Share, string) {
	line := strings.TrimSpace(string(out))
	if line == "" {
		return Unknown(), UnknownShare(), ""
	}
	parts := strings.Split(line, field)
	limit := Unknown()
	if total, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64); err == nil && total > 0 {
		limit = Measured(total)
	}
	return limit, processors(parts), name(parts)
}

// processors is what every container on this daemon may hold together, as a share of one processor:
// a machine with fourteen of them may hold 1400 per cent. It is the unit the daemon already prints
// for one container, so the ceiling and the figures under it are read in one language.
func processors(parts []string) Share {
	if len(parts) < 2 {
		return UnknownShare()
	}
	count, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || count <= 0 {
		return UnknownShare()
	}
	return MeasuredShare(float64(count) * 100)
}

func name(parts []string) string {
	if len(parts) < 3 {
		return ""
	}
	return strings.TrimSpace(parts[2])
}

// parseStats adds up what every container holds, on both axes, and pulls the sandboxes out of the
// list.
//
// Every container rather than the system's own, because the cap binds all of them: the question the
// figure answers is whether another sandbox will start, and a container belonging to something else
// takes the same memory and the same processors.
//
// A container the daemon could not read is skipped rather than counted as zero. A total that
// silently leaves one out is worse than a total that is missing, so a line the system cannot read
// leaves the total unknown.
func parseStats(out []byte) (Figure, Share, []Sandbox) {
	body := strings.TrimSpace(string(out))
	if body == "" {
		// The daemon answered and it is holding nothing. Zero is measured here, and it is not the
		// same answer as a daemon that would not say.
		return Measured(0), MeasuredShare(0), nil
	}

	var total int64
	var busy float64
	var boxes []Sandbox
	readable, readableShare := true, true
	for _, line := range strings.Split(body, "\n") {
		parts := strings.Split(strings.TrimSpace(line), field)
		if len(parts) < 3 {
			readable, readableShare = false, false
			continue
		}
		held, ok := parseSize(parts[1])
		if !ok {
			readable = false
		} else {
			total += held.Bytes()
		}
		share := parsePercent(parts[2])
		if !share.Known() {
			readableShare = false
		} else {
			busy += share.Percent()
		}
		session, isSandbox := sessionOf(strings.TrimSpace(parts[0]))
		if !isSandbox {
			continue
		}
		boxes = append(boxes, Sandbox{
			Session:   session,
			Held:      held,
			Processor: share,
		})
	}
	// The two axes are reported apart, because one unreadable figure must not take the other down
	// with it: a daemon that gave every memory figure and one bad processor figure has still said
	// what the machine holds.
	used, running := Measured(total), MeasuredShare(busy)
	if !readable {
		used = Unknown()
	}
	if !readableShare {
		running = UnknownShare()
	}
	return used, running, boxes
}

// sessionOf is the session a container belongs to, and false for a container that is not a sandbox.
// The system's own services are containers too, and they hold memory, but nobody stops one of them to
// make room.
//
// The sandbox package answers this, under either of the names a sandbox can carry, so a container
// started before the rename is still charged to the session that holds it rather than read as
// somebody else's.
func sessionOf(container string) (string, bool) { return sandbox.SessionOf(container) }

// parseSize reads the left of "1.201GiB / 7.653GiB", which is what the daemon prints for the memory
// one container holds. It states a unit rather than bytes, so this is the only place in the system
// that turns one back into a number.
func parseSize(text string) (Figure, bool) {
	held, _, _ := strings.Cut(text, "/")
	held = strings.TrimSpace(held)
	if held == "" || strings.HasPrefix(held, "-") {
		// The daemon prints "-- / --" for a container it cannot read.
		return Unknown(), false
	}
	digits := strings.TrimRight(held, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	unit := strings.ToLower(strings.TrimSpace(held[len(digits):]))
	value, err := strconv.ParseFloat(strings.TrimSpace(digits), 64)
	if err != nil || value < 0 {
		return Unknown(), false
	}
	scale, known := scaleOf(unit)
	if !known {
		return Unknown(), false
	}
	return Measured(int64(value * scale)), true
}

// scaleOf is what one of the daemon's units is worth in bytes. Both spellings are here because the
// daemon prints the binary units by default and the decimal ones where it was built to.
func scaleOf(unit string) (float64, bool) {
	switch unit {
	case "b":
		return 1, true
	case "kib":
		return 1 << 10, true
	case "mib":
		return 1 << 20, true
	case "gib":
		return 1 << 30, true
	case "tib":
		return 1 << 40, true
	case "kb":
		return 1e3, true
	case "mb":
		return 1e6, true
	case "gb":
		return 1e9, true
	case "tb":
		return 1e12, true
	default:
		return 0, false
	}
}

// parsePercent reads "0.15%", which is what the daemon prints for a container's share of one
// processor. Anything else is unknown rather than zero: a session reported at no processor share is
// a session an operator reads as idle.
func parsePercent(text string) Share {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "%"))
	if trimmed == "" {
		return UnknownShare()
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value < 0 {
		return UnknownShare()
	}
	return MeasuredShare(value)
}

// Describe is what `krewe room` prints when it answers from the system rather than from a sandbox.
func Describe(sample Sample) string {
	var out strings.Builder
	if !sample.Taken() {
		out.WriteString("the system has not read the machine yet, so it says nothing about it.\n")
		if sample.Failed != "" {
			fmt.Fprintf(&out, "\n  %s\n", sample.Failed)
		}
		return out.String()
	}

	fmt.Fprintf(&out, "the machine is %s: %s of %s is held.\n\n",
		sample.State(), sample.Used, sample.Limit)
	fmt.Fprintf(&out, "  every container holds  %10s\n", sample.Used)
	fmt.Fprintf(&out, "  the daemon may hold    %10s   the limit that binds\n", sample.Limit)
	fmt.Fprintf(&out, "  so there is room for   %10s\n", sample.Free())

	where := sample.Machine.Name
	if where == "" {
		where = "the machine it runs on"
	}
	fmt.Fprintf(&out, "\n%s, which is a different question:\n", where)
	fmt.Fprintf(&out, "  it has                 %10s\n", sample.Machine.Total)
	fmt.Fprintf(&out, "  free right now         %10s\n", sample.Machine.Available)
	if fraction, known := sample.Machine.SwapFraction(); known {
		fmt.Fprintf(&out, "  swap in use            %10s   %.0f per cent of %s\n",
			sample.Machine.SwapUsed, fraction*100, sample.Machine.SwapTotal)
	} else {
		fmt.Fprintf(&out, "  swap in use            %10s\n", sample.Machine.SwapUsed)
	}

	if len(sample.Sandboxes) == 0 {
		out.WriteString("\nNo session is holding a sandbox.\n")
	} else {
		fmt.Fprintf(&out, "\n%d sandboxes, largest first:\n", len(sample.Sandboxes))
		for _, box := range Sorted(sample.Sandboxes) {
			fmt.Fprintf(&out, "  %s  %10s  %8s\n", box.Session, box.Held, box.Processor)
		}
	}

	if sample.Failed != "" {
		fmt.Fprintf(&out, "\nSome of this could not be read: %s\n", sample.Failed)
	}
	fmt.Fprintf(&out, "\nRead at %s.\n", sample.TakenAt.Format(time.RFC3339))
	return out.String()
}

// Sorted orders sandboxes by what they hold, largest first, which is the order an operator deciding
// what to stop reads them in. A sandbox the system could not measure sorts last rather than first,
// because an unknown figure is not a large one.
func Sorted(boxes []Sandbox) []Sandbox {
	ordered := make([]Sandbox, len(boxes))
	copy(ordered, boxes)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && after(ordered[j-1], ordered[j]); j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}
	return ordered
}

// after says the first sandbox belongs below the second.
func after(a, b Sandbox) bool {
	if a.Held.Known() != b.Held.Known() {
		return !a.Held.Known()
	}
	if a.Held.Bytes() != b.Held.Bytes() {
		return a.Held.Bytes() < b.Held.Bytes()
	}
	return a.Session > b.Session
}
