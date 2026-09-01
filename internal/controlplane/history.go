package controlplane

import (
	"context"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetHistory says what the crew did over a window: what ran, what it cost, and what failed and why.
//
// It exists because a session could read the repository it stands in and nothing else. Everything a
// job needed to know about the crew's own work had to be typed into its brief by hand, so the
// operator was the memory, and the brief for one writing job came to 1,109 words of facts the crew
// already held. See issue 543.
//
// The shape is a total and then a page. The total is taken over every job in the window and the page
// is cut afterwards, so a reader who takes ten rows still gets a summary of the whole window rather
// than a summary of the ten. That order is the whole correctness of this call.
func (s *Server) GetHistory(ctx context.Context, req *quaycrewv1.GetHistoryRequest) (*quaycrewv1.GetHistoryResponse, error) {
	window, err := job.Window{Since: at(req.GetSince()), Until: at(req.GetUntil())}.Resolve(time.Now().UTC())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	history, err := s.store.JobHistory(ctx, job.HistoryQuery{
		Workspace: req.GetWorkspace(), Project: req.GetProject(), Window: window,
	})
	if err != nil {
		return nil, storeError(err, "job history")
	}

	// Added up before it is cut down. The other order gives a reader a summary of the page, which is a
	// number that looks right and is not.
	total := job.Summarise(history)
	page, leftOut := job.Page(history, int(req.GetLimit()))

	digests := make([]*quaycrewv1.JobDigest, 0, len(page))
	for _, one := range page {
		digests = append(digests, asDigest(one))
	}
	return &quaycrewv1.GetHistoryResponse{
		Total:   asTotals(total),
		Jobs:    digests,
		LeftOut: int32(leftOut),
		Since:   timestamppb.New(window.Since),
		Until:   timestamppb.New(window.Until),
	}, nil
}

// at reads a moment off the wire, leaving an unset one as the zero time so the window can tell "not
// given" from a moment somebody meant.
func at(stamp *timestamppb.Timestamp) time.Time {
	if stamp == nil {
		return time.Time{}
	}
	return stamp.AsTime().UTC()
}

// stamp is the other direction, and leaves a moment a job does not have yet off the wire rather than
// sending the zero time, which a reader would draw as the first of January year one.
func stamp(at time.Time) *timestamppb.Timestamp {
	if at.IsZero() {
		return nil
	}
	return timestamppb.New(at)
}

// asDigest puts one job's digest on the wire.
func asDigest(one *job.Digest) *quaycrewv1.JobDigest {
	return &quaycrewv1.JobDigest{
		Id: one.ID, Project: one.Project, Title: one.Title, Role: one.Role, Phase: one.Phase,
		SpentTokens: one.SpentToken, PullRequest: one.PullRequest, Reason: one.Reason,
		Steers:    int32(one.Steers),
		CreatedAt: stamp(one.CreatedAt), StartedAt: stamp(one.StartedAt), FinishedAt: stamp(one.FinishedAt),
	}
}

// asTotals puts the window's arithmetic on the wire.
func asTotals(total job.Totals) *quaycrewv1.HistoryTotals {
	return &quaycrewv1.HistoryTotals{
		Jobs: int32(total.Jobs), Done: int32(total.Done), Failed: int32(total.Failed),
		Stopped: int32(total.Stopped), Unfinished: int32(total.Unfinished),
		SpentTokens: total.SpentToken, PullRequests: int32(total.PullRequests),
		Steers: int32(total.Steers), WorkingSeconds: int64(total.Working / time.Second),
	}
}
