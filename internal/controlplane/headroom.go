package controlplane

import (
	"context"
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// unmeasured is what a byte count carries when nothing measured it. It is negative rather than zero
// because zero is a real reading, and a caller that compares the two has to be able to tell a
// machine holding nothing from a machine nobody read.
const unmeasured = int64(-1)

// GetHeadroom is the crew's last reading of the machine it runs on.
//
// It answers from the last sample and never from the daemon. The header asks this every second, and
// reading the daemon takes as long as the daemon takes: `docker stats` waits for it to sample every
// container. So the sampler has its own timer and this call is a read of a field. That is rule one
// of issue 405, and `internal/controlplane/headroom_test.go` proves the daemon is not called here.
func (s *Server) GetHeadroom(ctx context.Context, _ *quaycrewv1.GetHeadroomRequest) (*quaycrewv1.GetHeadroomResponse, error) {
	sample := s.headroom.Latest()

	resp := &quaycrewv1.GetHeadroomResponse{
		Used:             sample.Used.String(),
		Limit:            sample.Limit.String(),
		Free:             sample.Free().String(),
		State:            sample.State(),
		MachineName:      sample.Machine.Name,
		MachineTotal:     sample.Machine.Total.String(),
		MachineAvailable: sample.Machine.Available.String(),
		SwapTotal:        sample.Machine.SwapTotal.String(),
		SwapUsed:         sample.Machine.SwapUsed.String(),
		Failed:           sample.Failed,
		UsedBytes:        measured(sample.Used),
		LimitBytes:       measured(sample.Limit),
	}
	if sample.Taken() {
		resp.TakenAt = timestamppb.New(sample.TakenAt)
	}
	// Largest first, because the question this view answers is which session to stop.
	for _, box := range headroom.Sorted(sample.Sandboxes) {
		resp.Sandboxes = append(resp.Sandboxes, s.headroomSandbox(ctx, box))
	}
	return resp, nil
}

// headroomSandbox describes one container, and asks the store what the session beside it is doing.
//
// The daemon knows what a container holds and nothing about what it is for. A session row is what
// says whether the largest sandbox on the machine is the one doing the work, which is the difference
// between stopping something idle and stopping something mid task.
func (s *Server) headroomSandbox(ctx context.Context, box headroom.Sandbox) *quaycrewv1.HeadroomSandbox {
	answer := &quaycrewv1.HeadroomSandbox{
		Session:   box.Session,
		Held:      box.Held.String(),
		Processor: box.Processor.String(),
		HeldBytes: measured(box.Held),
	}
	session, err := s.store.GetSession(ctx, box.Session)
	if err != nil {
		// A container the crew holds no session for. It is a stray rather than a session, and saying
		// nothing about it is right: the crew does not know what it is.
		return answer
	}
	answer.Status = session.GetStatus()
	if since := session.GetUpdatedAt(); since != nil {
		answer.Idle = shortDuration(time.Since(since.AsTime()))
	}
	return answer
}

// measured is a byte count for a caller that compares numbers, and unmeasured where nothing read it.
func measured(figure headroom.Figure) int64 {
	if !figure.Known() {
		return unmeasured
	}
	return figure.Bytes()
}

// shortDuration writes an age the way a listing does: the largest unit that says something, and
// never more than two characters of number. A listing is read at a glance and "3h" is read faster
// than "3h14m22s".
func shortDuration(age time.Duration) string {
	switch {
	case age < 0:
		return ""
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours())/24)
	}
}

// SampleHeadroom reads the machine once, now. The sampler's own timer calls this too; a caller that
// wants a reading without waiting for a tick calls it directly.
func (s *Server) SampleHeadroom(ctx context.Context) { s.headroom.Once(ctx) }

// RunHeadroom reads the machine on its own timer until the context ends. Whoever owns the process
// starts it, the way the flow poller is started, because a goroutine hidden inside a constructor is
// a lifetime nobody can see.
func (s *Server) RunHeadroom(ctx context.Context) { s.headroom.Run(ctx) }
