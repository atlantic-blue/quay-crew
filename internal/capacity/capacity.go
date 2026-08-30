// Package capacity is the arithmetic that decides whether another sandbox fits.
//
// The system used to admit work by counting it. A workspace said `max running 8`, nine jobs were
// declared, and the ninth was admitted because eight is not nine. Sandboxes are not the same size:
// on the machine that broke, ten of them held 4.3, 201.7, 306.2, 325.1, 359.4, 364.7, 400.4, 478.6,
// 498.4 and 764.5 megabytes, two orders of magnitude between the smallest and the largest. A count
// says they are the same. The ninth job waited two minutes for a container, was failed, and the
// container runtime then stopped answering for four minutes and exited, taking the control plane,
// the database and eight running jobs with it. See issue 466.
//
// So this is the kubernetes model rather than a count. Every sandbox declares a Request for memory
// and processor. The system reads what the runtime has, holds back a Reserve for itself, and admits a
// sandbox only where the requests already placed plus this one still fit in what is left. A job that
// does not fit stays pending, for as long as it takes, and says which resource ran out. Nothing is
// admitted and then killed.
//
// Two things are deliberately not here. A Request is not a limit: it is what the system reserves, and
// what stops one runaway sandbox taking the machine is a limit, which is issue 477. And nothing here
// evicts: when a machine goes under anyway, something has to stop the least valuable sandbox, which
// is issue 478.
//
// The one thing that differs from kubernetes is the Reserve. A kubelet runs on the node, outside the
// pods it manages. The system's control plane, database and event log are containers inside the same
// runtime the work fills, so the system has to hold capacity back for itself or it goes down with its
// own workload. On 30 August 2026 it did exactly that.
package capacity

import "fmt"

// A Request is what one sandbox asks for, and what the system reserves on its behalf.
//
// Memory is bytes and Processor is per cent of one processor, which are the units the room view
// already prints: "764 MiB" and "913.0%". One vocabulary, so an operator sets a number and reads it
// back in the same words.
type Request struct {
	Memory    int64
	Processor int
}

// The whole processor, in the units Processor is counted in. A machine with fourteen processors has
// 1400 of these to give out.
const OneProcessor = 100

// Plus adds two requests, which is what placing a sandbox does to what is already placed.
func (r Request) Plus(other Request) Request {
	return Request{Memory: r.Memory + other.Memory, Processor: r.Processor + other.Processor}
}

// Minus takes one request off another, and never goes below zero on either side: a reserve larger
// than the machine leaves nothing to give out rather than a negative amount to give out.
func (r Request) Minus(other Request) Request {
	return Request{
		Memory:    atLeastZero(r.Memory - other.Memory),
		Processor: int(atLeastZero(int64(r.Processor - other.Processor))),
	}
}

// Empty says whether this request asks for nothing at all, which is what an unset one does.
func (r Request) Empty() bool { return r.Memory <= 0 && r.Processor <= 0 }

// Or is this request where it asks for something, and the other where it does not. It is how a
// workspace's own request stands in front of the system's default without either having to know about
// the other.
func (r Request) Or(other Request) Request {
	answer := r
	if answer.Memory <= 0 {
		answer.Memory = other.Memory
	}
	if answer.Processor <= 0 {
		answer.Processor = other.Processor
	}
	return answer
}

// String is the request in the units the room view prints, so a refusal and a listing read alike.
func (r Request) String() string {
	return fmt.Sprintf("%s, %s", Memory(r.Memory), Processor(r.Processor))
}

// Memory writes a byte count as whole mebibytes, which is how the room view and every memory limit
// spell one.
func Memory(bytes int64) string { return fmt.Sprintf("%d MiB", bytes/(1<<20)) }

// Processor writes a processor share as per cent of one processor, which is how the daemon reports
// one and how the room view prints it.
func Processor(percent int) string { return fmt.Sprintf("%d%%", percent) }

// A Node is what the runtime has and what the system keeps back from it.
//
// Capacity is read from the container runtime and never from the host. On 30 August 2026 the host
// had 36 gibibytes free while the runtime it holds had 7.65 and was full, so a system that read the
// host would have called that machine empty while it died.
type Node struct {
	Capacity Request
	Reserve  Request
	// Known says whether anything measured the capacity. A system whose runtime cannot be read admits
	// work unmeasured rather than refusing everything: the local sandbox provider has no runtime to
	// read at all, and a system that stopped dead there would be worse than the system that counted.
	Known bool
}

// Allocatable is what sandboxes may have between them: what the runtime has, less what the system
// holds back for itself.
func (n Node) Allocatable() Request { return n.Capacity.Minus(n.Reserve) }

// A Verdict is the answer to whether one more sandbox fits, and why not when it does not.
type Verdict struct {
	// OK says the sandbox may be placed.
	OK bool
	// Resource is the one that ran out, empty where nothing did. It is a word rather than a number
	// so a reader learns which of the two to go and free.
	Resource string
	// Reason is the whole sentence, naming the resource, the request and what was left.
	Reason string
	// Unmeasured says the system could not read its runtime, so this admission is arithmetic nobody
	// checked. It is true only alongside OK, and a caller that logs it is saying the system is running
	// blind rather than that it is running well.
	Unmeasured bool
}

// The resources a sandbox can run out of. They are words rather than numbers because the operator's
// next move is different for each: stop a session to free memory, wait for a build to end to free
// processors.
const (
	ResourceMemory    = "memory"
	ResourceProcessor = "processor"
)

// Fits says whether one more request may be placed on a node that already holds these.
//
// The arithmetic is the whole mechanism: placed plus wanted against allocatable, on both resources,
// and the first one that does not fit is the one named. Memory is checked first because it is the
// one an operator can free immediately by stopping a session.
func Fits(node Node, placed, want Request) Verdict {
	if !node.Known {
		return Verdict{OK: true, Unmeasured: true}
	}
	allocatable := node.Allocatable()
	after := placed.Plus(want)
	if after.Memory > allocatable.Memory {
		return Verdict{
			OK: false, Resource: ResourceMemory,
			Reason: shortOf(ResourceMemory, Memory(want.Memory),
				Memory(allocatable.Memory-placed.Memory), Memory(allocatable.Memory)),
		}
	}
	if after.Processor > allocatable.Processor {
		return Verdict{
			OK: false, Resource: ResourceProcessor,
			Reason: shortOf(ResourceProcessor, Processor(want.Processor),
				Processor(allocatable.Processor-placed.Processor), Processor(allocatable.Processor)),
		}
	}
	return Verdict{OK: true}
}

// shortOf is what a held job says about itself.
//
// It names the resource first, because that is the word an operator acts on, and then the three
// figures that make the arithmetic checkable: what this sandbox asked for, what was left, and what
// there is in total. A refusal nobody can check against the room view is a refusal nobody believes.
func shortOf(resource, want, left, allocatable string) string {
	return fmt.Sprintf("there is not enough %s for this job's sandbox: it asks for %s, "+
		"%s of %s is unallocated", resource, want, left, allocatable)
}

// atLeastZero keeps a subtraction on the sensible side of nothing. A reserve larger than the machine
// leaves nothing to give out, which is a system that admits nothing, and never a negative amount to
// give out, which is a system that admits everything.
func atLeastZero(figure int64) int64 {
	if figure < 0 {
		return 0
	}
	return figure
}

// KeyFor is what a sandbox is admitted under, and it is the same string on both sides of the
// dispatch: the controller reserves room before it starts a job, and the system places the container
// under it seconds later. Anything else would count one sandbox twice.
//
// The project and the handle, because a handle is only unique inside its project: two projects each
// holding a session called "review" are two sandboxes, and a key that could not tell them apart
// would hand one machine's room out twice.
func KeyFor(project, handle string) string { return project + "/" + handle }
