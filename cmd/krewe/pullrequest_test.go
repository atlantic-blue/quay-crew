package main

import (
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// What krewe job show prints under the address of a pull request.
//
// The line is read by somebody deciding what to pick up, so the one that matters is the unknown: a
// pull request nothing could read must not print anything a person reads as fine.

func TestWhatIsPrintedUnderAPullRequest(t *testing.T) {
	read := timestamppb.New(time.Now().Add(-3 * time.Minute))
	for _, one := range []struct {
		name  string
		job   *quaycrewv1.Job
		says  string
		never string
	}{
		{
			name:  "nothing has read it",
			job:   &quaycrewv1.Job{PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/1"},
			says:  "nothing has read it yet",
			never: forge.ChecksGreen,
		},
		{
			name: "the reading failed",
			job: &quaycrewv1.Job{
				PullRequestStatus: forge.StatusUnknown, PullRequestChecks: forge.ChecksUnknown,
				PullRequestReview: forge.ReviewUnknown, PullRequestReadAt: read,
				PullRequestFailed: "the rate limit is spent",
			},
			says:  "the rate limit is spent",
			never: forge.ChecksGreen,
		},
		{
			name: "it merged",
			job: &quaycrewv1.Job{
				PullRequestStatus: forge.StatusMerged, PullRequestChecks: forge.ChecksGreen,
				PullRequestReview: forge.ReviewApproved, PullRequestReadAt: read,
			},
			says: "merged, checks green, read 3m ago",
		},
		{
			name: "a check went red",
			job: &quaycrewv1.Job{
				PullRequestStatus: forge.StatusOpen, PullRequestChecks: forge.ChecksRed,
				PullRequestCheck: "integration", PullRequestReview: forge.ReviewNone,
				PullRequestReadAt: read,
			},
			says: "open, checks red: integration",
		},
		{
			name: "a review asked for changes",
			job: &quaycrewv1.Job{
				PullRequestStatus: forge.StatusOpen, PullRequestChecks: forge.ChecksGreen,
				PullRequestReview: forge.ReviewChangesRequested, PullRequestReadAt: read,
			},
			says: "a review asked for changes",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			printed := pullRequestState(one.job)
			if !strings.Contains(printed, one.says) {
				t.Fatalf("it prints %q, and never says %q", printed, one.says)
			}
			if one.never != "" && strings.Contains(printed, one.never) {
				t.Fatalf("it prints %q, which reads as %q", printed, one.never)
			}
		})
	}
}
