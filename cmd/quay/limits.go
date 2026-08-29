package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// The flags a ceiling is set with. Each is its own number, so setting one and leaving the rest is a
// read of the row and a write of it back.
const (
	flagMaxDepth   = "--max-depth"
	flagMaxRunning = "--max-running"
	flagLease      = "--lease"
	// The two times a session's life is measured by. Both ship unset, and unset means the crew takes
	// no container back and files nothing away. No number is written for them anywhere in this
	// repository: three measurements decide them, section 11 of docs/ORCHESTRATION.md names each and
	// the command that would take it, and none has been run.
	flagReclaim = "--reclaim"
	flagArchive = "--archive"
)

// runLimits reads and sets what a workspace lets its sessions declare.
//
// A role says what a session may do and this says how much of it, and the two are deliberately in
// different places: a role is a file somebody reviews, and a limit is tenancy. The effective
// capability is the intersection.
func runLimits(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: quay limits [<workspace>] [%s <n>] [%s <n>] [%s <n>] "+
			"[%s <duration>] [%s <duration>] [%s <duration>]",
			flagMaxDepth, flagMaxRunning, flagBudgetTokens, flagLease, flagReclaim, flagArchive)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if located.WorkspaceID == "" {
		return fmt.Errorf("a ceiling belongs to a workspace: quay limits <workspace>")
	}

	held, err := client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: located.WorkspaceID,
	})
	if err != nil {
		return err
	}
	asked := held.GetLimits()

	setting := false
	for _, given := range []struct {
		flag string
		set  func(int64)
	}{
		{flagMaxDepth, func(n int64) { asked.MaxDepth = int32(n) }},
		{flagMaxRunning, func(n int64) { asked.MaxRunning = int32(n) }},
		{flagBudgetTokens, func(n int64) { asked.BudgetTokens = n }},
	} {
		if !values.has(given.flag) {
			continue
		}
		number, err := strconv.ParseInt(values.first(given.flag), 10, 64)
		if err != nil {
			return fmt.Errorf("%s takes a number, and %q is not one", given.flag, values.first(given.flag))
		}
		given.set(number)
		setting = true
	}
	for _, given := range []struct {
		flag string
		set  func(int32)
	}{
		{flagLease, func(seconds int32) { asked.LeaseSeconds = seconds }},
		{flagReclaim, func(seconds int32) { asked.ReclaimSeconds = seconds }},
		{flagArchive, func(seconds int32) { asked.ArchiveSeconds = seconds }},
	} {
		if !values.has(given.flag) {
			continue
		}
		length, err := time.ParseDuration(values.first(given.flag))
		if err != nil {
			return fmt.Errorf("%s takes a length of time such as 60s, and %q is not one",
				given.flag, values.first(given.flag))
		}
		given.set(int32(length.Seconds()))
		setting = true
	}

	if setting {
		written, err := client.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{Limits: asked})
		if err != nil {
			return err
		}
		asked = written.GetLimits()
	}

	fmt.Fprintf(out, "%s\n", located.Path)
	fmt.Fprintf(out, "max depth      %d%s\n", asked.GetMaxDepth(), depthMeans(asked.GetMaxDepth()))
	fmt.Fprintf(out, "max running    %s\n", unsetOr(int64(asked.GetMaxRunning())))
	fmt.Fprintf(out, "budget tokens  %s\n", unsetOr(asked.GetBudgetTokens()))
	fmt.Fprintf(out, "lease          %s\n", leaseOr(asked.GetLeaseSeconds()))
	fmt.Fprintf(out, "reclaim        %s%s\n", lengthOr(asked.GetReclaimSeconds()),
		timeMeans(asked.GetReclaimSeconds(), "no session here gives its container back"))
	fmt.Fprintf(out, "archive        %s%s\n", lengthOr(asked.GetArchiveSeconds()),
		timeMeans(asked.GetArchiveSeconds(), "nothing here is filed away on its own"))
	if !setting {
		fmt.Fprintf(out, "\nraise one with quay limits %s %s <n>\n", located.Path, flagMaxDepth)
	}
	return nil
}

// depthMeans says out loud what the number does, because zero reads as "no limit" to everybody who
// has met one before and here it means the opposite.
func depthMeans(depth int32) string {
	if depth == 0 {
		return "  (no session here may declare a job)"
	}
	return ""
}

// timeMeans says out loud what unset does, because a reader who has met a timeout before reads a
// missing number as a default rather than as off.
func timeMeans(seconds int32, means string) string {
	if seconds == 0 {
		return "  (" + means + ")"
	}
	return ""
}

// lengthOr reads a number of seconds as a length of time, and zero as unset.
func lengthOr(seconds int32) string {
	if seconds == 0 {
		return "unset"
	}
	return (time.Duration(seconds) * time.Second).String()
}

func unsetOr(value int64) string {
	if value == 0 {
		return "unset"
	}
	return strconv.FormatInt(value, 10)
}

func leaseOr(seconds int32) string {
	if seconds == 0 {
		return "the crew's own"
	}
	return (time.Duration(seconds) * time.Second).String()
}

// limitsFlagsTaken is every flag quay limits takes, which is what keeps the tool's refusal of flags
// from refusing these.
func limitsFlagsTaken() map[string]bool {
	return map[string]bool{
		flagMaxDepth: true, flagMaxRunning: true, flagBudgetTokens: true, flagLease: true,
		flagReclaim: true, flagArchive: true,
	}
}
