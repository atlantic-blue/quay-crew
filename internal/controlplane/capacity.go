package controlplane

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/capacity"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Admit says whether the runtime can host one more sandbox, and takes the room for it when it can.
//
// The reservation is the part a reading cannot do on its own. A dispatch is detached, so the
// container appears seconds after the job that asked for it was admitted, and the system reads its
// runtime on a ten second timer. Nine jobs asking the same reading whether the machine is empty are
// all told yes. So the answer and the reservation are one movement, and the tenth job counts the
// nine before it. Kubernetes does the same and calls it assuming the pod onto the node.
//
// The caller releases what it took on every road that does not end in a sandbox. A reservation
// nobody released expires on its own, which is the backstop for a controller that died mid start
// and never the mechanism.
func (s *Server) Admit(ctx context.Context, key string, want capacity.Request) capacity.Verdict {
	node := s.node()
	verdict := capacity.Fits(node, s.placed.Placed(), want)
	if !verdict.OK {
		slog.InfoContext(ctx, "there is no room on this machine for another sandbox",
			"wanted", want.String(), "placed", s.placed.Placed().String(),
			"sandboxes", s.placed.Count(), "allocatable", node.Allocatable().String(),
			"resource", verdict.Resource)
		return verdict
	}
	s.placed.Reserve(key, want)
	return verdict
}

// Release gives back room that was taken for a sandbox that will not be made.
func (s *Server) Release(key string) { s.placed.Release(key) }

// node is the runtime as the system last read it, with what the system keeps back for itself.
//
// It reads the last sample and never the daemon. Admission runs on every job, and reading the daemon
// takes as long as the daemon takes: `docker stats` waits for it to sample every container. That is
// rule one of issue 405 and it holds harder here, because this is on the path of every job the system
// starts rather than of a header nobody is waiting for.
func (s *Server) node() capacity.Node {
	return nodeFrom(s.headroom.Latest(), s.reserve)
}

// requestFor is what one sandbox in this workspace asks the machine for: what the workspace declared,
// or the system's own measured request where it declared nothing.
func (s *Server) requestFor(ctx context.Context, workspace string) capacity.Request {
	standard := capacity.DefaultRequest()
	limits, err := s.store.WorkspaceLimits(ctx, workspace)
	if err != nil {
		return standard
	}
	return limits.Request(standard)
}

// placeSandbox takes the room one session's container needs, at the moment the system is about to make
// it, and refuses when the runtime has none.
//
// This is the second half of the kubernetes shape. The controller is the scheduler: it decides what
// to admit and holds what does not fit. This is the kubelet: whatever reaches the machine is checked
// against the machine one last time, so a dispatch nobody scheduled cannot take a runtime down. A
// job that came through the controller is already holding its room under the same key, and taking it
// twice is what the key prevents.
func (s *Server) placeSandbox(ctx context.Context, session *quaycrewv1.Session) error {
	key := capacity.KeyFor(session.GetProject(), session.GetHandle())
	want := s.requestFor(ctx, session.GetWorkspace())
	if verdict := capacity.Fits(s.node(), s.placed.Without(key), want); !verdict.OK {
		slog.WarnContext(ctx, "a sandbox was refused because the runtime is full",
			"session", session.GetId(), "resource", verdict.Resource)
		return status.Error(codes.ResourceExhausted, verdict.Reason)
	}
	s.placed.Place(key, session.GetId(), want)
	return nil
}

// unplaceSandbox gives back the room a container held, when the container goes.
func (s *Server) unplaceSandbox(sessionID string) { s.placed.ReleaseSession(sessionID) }

// SeedCapacity counts the sandboxes that were already running when this system started.
//
// The containers outlive the process that made them. A system that started counting from zero would
// admit a whole machine's worth of work onto a machine that is already full, which is the same
// failure as counting jobs and arrives on every restart.
func (s *Server) SeedCapacity(ctx context.Context) {
	running, err := s.provider.Stranded(ctx)
	if err != nil {
		slog.WarnContext(ctx, "the system could not read which sandboxes were already running, "+
			"so it is admitting work as though the machine were empty", "error", err)
		return
	}
	if len(running) == 0 {
		return
	}
	s.placed.Seed(running, capacity.DefaultRequest())
	slog.InfoContext(ctx, "the system counted the sandboxes that were already running",
		"sandboxes", len(running), "placed", s.placed.Placed().String())
}

// SystemReserve is what the system holds back for itself, from the system's configuration.
//
// This is the one thing that is not kubernetes. A kubelet runs on the node, outside the pods it
// manages, so what it reserves is somebody else's memory. The system's control plane, database and
// event log are containers inside the same runtime the work fills, so a system that reserves nothing
// goes down with its own workload. On 30 August 2026 it did.
//
// The figure here is a floor. What actually binds is measured on every sample: everything the
// runtime holds, less what the sandboxes hold, is the system's own containers.
func SystemReserve(memory, processor string, logger *slog.Logger) capacity.Request {
	reserve := capacity.DefaultReserve()
	if mebibytes, ok := wholeNumber(memory, logger, "QC_SYSTEM_RESERVE_MEMORY"); ok {
		reserve.Memory = int64(mebibytes) << 20
	}
	if percent, ok := wholeNumber(processor, logger, "QC_SYSTEM_RESERVE_PROCESSOR"); ok {
		reserve.Processor = percent
	}
	return reserve
}

// wholeNumber reads one of the two reserve settings. A setting that is not a number leaves the
// measured floor and says so: a system that will not start over a misspelled number is worse than a
// system running the number it was already running.
func wholeNumber(value string, logger *slog.Logger, named string) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	number, err := strconv.Atoi(trimmed)
	if err != nil || number < 0 {
		if logger != nil {
			logger.Warn("this setting is not a whole number, so the system's own floor is used instead",
				"setting", named, "configured", value)
		}
		return 0, false
	}
	return number, true
}

// jobRoom is the controller's view of this server, which is the same two calls anything else would
// make. It is here rather than on the controller so the arithmetic has one home.
var _ job.Room = (*Server)(nil)

// EnvReserve reads the two reserve settings from the environment, which is how the compose stack and
// the operator's own configuration file set them.
func EnvReserve(logger *slog.Logger) capacity.Request {
	return SystemReserve(
		setting("QC_SYSTEM_RESERVE_MEMORY", "QC_CREW_RESERVE_MEMORY", logger),
		setting("QC_SYSTEM_RESERVE_PROCESSOR", "QC_CREW_RESERVE_PROCESSOR", logger),
		logger)
}

// setting reads one reserve setting, and reads the name it had before this level was called the
// system when that is the only one set.
//
// A configuration file is written once and kept for a year. Dropping the old name would leave the
// operator's floor silently back at the default, on the one setting whose whole job is to stop the
// machine going down under its own work, so the old name still counts and says out loud that it
// moved. The new name wins where both are set, because that is the one that was typed on purpose.
func setting(named, was string, logger *slog.Logger) string {
	if value := strings.TrimSpace(os.Getenv(named)); value != "" {
		return value
	}
	value := strings.TrimSpace(os.Getenv(was))
	if value == "" {
		return ""
	}
	if logger != nil {
		logger.Warn("this setting has been renamed, and is still read under the name it had. Set the new one",
			"setting", was, "now", named)
	}
	return value
}

// NodeFrom is the runtime as the system last read it: what it has, and what its own containers are
// taking out of that.
//
// It reads the runtime and never the host. On 30 August 2026 the host had 36 gibibytes free while
// the runtime it holds had 7.65 and was full, so a system that admitted against the host would have
// called that machine empty while it died.
//
// A sample nothing has taken, or one whose figures the daemon would not give up, leaves the node
// unknown. An unknown node admits work rather than refusing it: the local sandbox provider has no
// runtime to read at all, and a system that stopped dead there would be worse than the system that
// counted. The caller says out loud that it is admitting unmeasured.
func nodeFrom(sample headroom.Sample, floor capacity.Request) capacity.Node {
	node := capacity.Node{Reserve: floor}
	if !sample.Taken() || !sample.Limit.Known() || !sample.Processors.Known() {
		return node
	}
	node.Known = true
	node.Capacity = capacity.Request{
		Memory: sample.Limit.Bytes(),
		// Down, because a processor the system rounded into existence is a processor it would hand out.
		Processor: int(sample.Processors.Percent()),
	}
	node.Reserve = larger(floor, measuredReserve(sample))
	return node
}

// MeasuredReserve is what the system's own containers are taking: everything the runtime holds, less
// what the sandboxes hold.
//
// This is the number kubernetes does not need. A kubelet runs on the node, outside the pods it
// manages, so what it holds back is a flag somebody sets. The system's control plane, database and
// event log are containers inside the same runtime the work fills, so what it has to hold back is
// whatever they are actually using, and that is measurable rather than declared.
//
// A stray container nobody can account for lands on this side of the line, which is the safe side:
// it is real memory the system must not hand out twice.
func measuredReserve(sample headroom.Sample) capacity.Request {
	reserve := capacity.Request{}
	if sample.Used.Known() {
		held := sample.Used.Bytes()
		for _, box := range sample.Sandboxes {
			if box.Held.Known() {
				held -= box.Held.Bytes()
			}
		}
		reserve.Memory = atLeastZero(held)
	}
	if sample.Held.Known() {
		busy := sample.Held.Percent()
		for _, box := range sample.Sandboxes {
			if box.Processor.Known() {
				busy -= box.Processor.Percent()
			}
		}
		// Up, because a reserve rounded down is capacity the system hands out and then needs back.
		reserve.Processor = int(atLeastZero(int64(busy + 0.999)))
	}
	return reserve
}

// larger is the bigger of two requests on each axis, taken separately: a system whose own containers
// are using more memory than the floor and fewer processors than it must still keep both.
func larger(one, other capacity.Request) capacity.Request {
	answer := one
	if other.Memory > answer.Memory {
		answer.Memory = other.Memory
	}
	if other.Processor > answer.Processor {
		answer.Processor = other.Processor
	}
	return answer
}

// atLeastZero keeps a subtraction on the sensible side of nothing: a system whose sandboxes report
// more than the runtime does has read two commands a moment apart, not a machine holding less than
// nothing.
func atLeastZero(figure int64) int64 {
	if figure < 0 {
		return 0
	}
	return figure
}
