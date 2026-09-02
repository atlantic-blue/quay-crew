package controlplane

import (
	"context"
	"log/slog"
	"sort"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetWaiting answers what waits for a person, in one read, so every surface tells them the same
// thing.
//
// The calculation is the one the briefing has always made. It is here rather than in the page
// because the page is a thing a person opens, and the whole point of this read is that nobody has to
// open anything: the console, the command line and the status line all ask for it.
//
// Longest wait first, so the job somebody has been not knowing about for an hour is above the one
// that stopped a moment ago. A surface that can draw only one line draws that one.
//
// It records the telling as it answers. The first surface to name a waiting job writes job.raised
// carrying its own name, once for each wait rather than once for each poll, and the gap from
// job.asked is what this work is judged on. A request that names no surface records nothing: a
// caller that will not say who it is has told nobody.
func (s *Server) GetWaiting(ctx context.Context, req *quaycrewv1.GetWaitingRequest) (
	*quaycrewv1.GetWaitingResponse, error) {
	found, err := s.store.ListJobs(ctx, job.Filter{Workspace: req.GetWorkspace()})
	if err != nil {
		return nil, storeError(err, "jobs")
	}

	now := time.Now().UTC()
	limits := map[string]time.Duration{}
	answer := &quaycrewv1.GetWaitingResponse{}
	for _, one := range found {
		why, want, waiting := job.Waits(one)
		if !waiting {
			continue
		}
		since := job.WaitingSince(one)
		waited := now.Sub(since)
		if waited < 0 {
			waited = 0
		}
		answer.Waiting = append(answer.Waiting, &quaycrewv1.Waiting{
			Job: one.ID, Workspace: one.Workspace, Project: one.Project, Title: one.Title,
			Why: why,
			// Through the same redactor a record goes through. A question can quote whatever the
			// session had in front of it, and this is drawn on a screen and printed above a command.
			Want:          s.RedactFor(ctx, one.Workspace, want),
			Since:         timestamppb.New(since),
			WaitedSeconds: int64(waited.Seconds()),
			OverLimit:     waited >= s.waitingLimit(ctx, one.Workspace, limits),
			PullRequest:   one.PullRequest,
		})
		s.raise(ctx, one, req.GetSurface())
	}
	sort.SliceStable(answer.Waiting, func(i, j int) bool {
		return answer.Waiting[i].GetWaitedSeconds() > answer.Waiting[j].GetWaitedSeconds()
	})
	return answer, nil
}

// waitingLimit is how long a wait lasts in a workspace before its age is named, read once for each
// workspace in the answer rather than once for each job. A read that failed takes the system's own,
// because a limit nobody could read is not a reason to say nothing about a job that is waiting.
func (s *Server) waitingLimit(ctx context.Context, workspace string, read map[string]time.Duration) time.Duration {
	if held, known := read[workspace]; known {
		return held
	}
	limits, err := s.store.WorkspaceLimits(ctx, workspace)
	if err != nil {
		slog.WarnContext(ctx, "the workspace's limits could not be read, so the wait is measured "+
			"against the system's own", "workspace", workspace, "error", err)
		read[workspace] = job.DefaultWaiting
		return job.DefaultWaiting
	}
	read[workspace] = limits.Waiting()
	return limits.Waiting()
}

// raise writes that a surface named this job as waiting, once for each wait.
//
// It never fails the read. A telling that could not be recorded is still a telling, and a person
// looking at a screen that refused to draw because it could not write a record is worse off than one
// whose gap nobody measured.
func (s *Server) raise(ctx context.Context, one *job.Job, surface string) {
	if surface == "" || one.RaisedAt != nil {
		return
	}
	record := s.jobEvent(ctx, one, job.EventRaised, surface)
	written, err := s.store.RaiseJob(ctx, one.ID, record)
	if err != nil {
		slog.WarnContext(ctx, "a waiting job was named and the telling was not recorded",
			"job", one.ID, "surface", surface, "error", err)
		return
	}
	if !written {
		return
	}
	// After the write has landed, the way every other record is offered: the store is the truth and
	// the log is the copy.
	s.ExportJob(ctx, record)
}
