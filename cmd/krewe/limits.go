package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The flags a ceiling is set with. Each is its own number, so setting one and leaving the rest is a
// read of the row and a write of it back.
const (
	flagMaxDeclared = "--max-declared"
	// The flag the ceiling used to be typed as, kept so it is refused by name with the flag to type
	// instead: it is in scripts and in notes, and a flag quietly ignored leaves a ceiling unchanged.
	flagMaxDepth   = "--max-depth"
	flagMaxRunning = "--max-running"
	// What one sandbox in this workspace asks the machine for, in the units the room view prints:
	// mebibytes, and per cent of one processor. The system adds these up and starts a job only where
	// its runtime still has that much unallocated, so a workspace whose jobs compile says so here
	// rather than being counted the same as one whose jobs read a mailbox.
	flagRequestMemory    = "--request-memory"
	flagRequestProcessor = "--request-processor"
	flagLease            = "--lease"
	// The two times a session's life is measured by. Both ship unset, and unset means the system takes
	// no container back and files nothing away. No number is written for them anywhere in this
	// repository: three measurements decide them, section 11 of docs/ORCHESTRATION.md names each and
	// the command that would take it, and none has been run.
	flagReclaim = "--reclaim"
	flagArchive = "--archive"
	// How full a session's context window may be here before the system gives it no new task on the
	// job it is doing. It is the one number on this row that ships set rather than unset, and where it
	// came from is printed beside it.
	flagContextCeiling = "--context-ceiling"
	// How long a job may wait for a person here before the telling names the age beside it. It ships
	// at fifteen minutes, which is a guess: job.DefaultWaiting names the measurement that replaces it.
	flagWaiting = "--waiting"
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
	// The flag the ceiling used to be typed as. It is refused by name rather than ignored: a flag
	// that is quietly dropped leaves the operator believing a ceiling moved that did not.
	if values.has(flagMaxDepth) {
		return fmt.Errorf("%s is gone: a job cannot be under another job, so there is no depth to "+
			"bound. The ceiling is how many jobs one session may declare: krewe limits <workspace> %s <n>",
			flagMaxDepth, flagMaxDeclared)
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: krewe limits [<workspace>] [%s <n>] [%s <n>] [%s <n>] "+
			"[%s <duration>] [%s <duration>] [%s <duration>] [%s <duration>] [%s <mebibytes>] "+
			"[%s <per cent>] [%s <per cent>]",
			flagMaxDeclared, flagMaxRunning, flagBudgetTokens, flagLease, flagReclaim, flagArchive,
			flagWaiting, flagRequestMemory, flagRequestProcessor, flagContextCeiling)
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
		return fmt.Errorf("a ceiling belongs to a workspace: krewe limits <workspace>")
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
		{flagMaxDeclared, func(n int64) { asked.MaxDeclared = int32(n) }},
		{flagMaxRunning, func(n int64) { asked.MaxRunning = int32(n) }},
		{flagBudgetTokens, func(n int64) { asked.BudgetTokens = n }},
		{flagRequestMemory, func(n int64) { asked.RequestMemoryMib = int32(n) }},
		{flagRequestProcessor, func(n int64) { asked.RequestProcessorPercent = int32(n) }},
		{flagContextCeiling, func(n int64) { asked.ContextCeilingPercent = int32(n) }},
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
		{flagWaiting, func(seconds int32) { asked.WaitingSeconds = seconds }},
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
	fmt.Fprintf(out, "max declared   %d%s\n", asked.GetMaxDeclared(), declaredMeans(asked.GetMaxDeclared()))
	fmt.Fprintf(out, "max running    %s\n", unsetOr(int64(asked.GetMaxRunning())))
	fmt.Fprintf(out, "request        %s%s\n", requestOf(asked), systemsOwn(asked))
	fmt.Fprintf(out, "budget tokens  %s\n", unsetOr(asked.GetBudgetTokens()))
	fmt.Fprintf(out, "lease          %s%s\n", leaseOr(asked.GetLeaseSeconds()), leaseMeans)
	fmt.Fprintf(out, "reclaim        %s%s\n", lengthOr(asked.GetReclaimSeconds()),
		timeMeans(asked.GetReclaimSeconds(), "no session here gives its container back"))
	fmt.Fprintf(out, "archive        %s%s\n", lengthOr(asked.GetArchiveSeconds()),
		timeMeans(asked.GetArchiveSeconds(), "nothing here is filed away on its own"))
	fmt.Fprintf(out, "ctx ceiling    %d%%%s\n", ceilingOf(asked), ceilingMeans(asked))
	fmt.Fprintf(out, "waiting        %s%s\n", waitingOf(asked), waitingMeans(asked))
	if !setting {
		fmt.Fprintf(out, "\nraise one with krewe limits %s %s <n>\n", located.Path, flagMaxDeclared)
	}
	return nil
}

// requestOf is what one sandbox here asks the machine for, in the units the room view prints. A
// workspace that has set neither reads the system's own, so an operator sees the number that is
// actually being used rather than two zeroes.
func requestOf(limits *quaycrewv1.WorkspaceLimits) string {
	want := capacity.Request{
		Memory:    int64(limits.GetRequestMemoryMib()) << 20,
		Processor: int(limits.GetRequestProcessorPercent()),
	}
	return want.Or(capacity.DefaultRequest()).String()
}

// systemsOwn says when the figures beside it are the system's rather than this workspace's, because a
// number with no source reads as a decision somebody made about this workspace.
func systemsOwn(limits *quaycrewv1.WorkspaceLimits) string {
	if limits.GetRequestMemoryMib() > 0 && limits.GetRequestProcessorPercent() > 0 {
		return ""
	}
	return "  (the system's own, until this workspace sets its own)"
}

// declaredMeans says out loud what the number does, because zero reads as "no limit" to everybody
// who has met one before and here it means the opposite.
func declaredMeans(declared int32) string {
	if declared == 0 {
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

// leaseMeans says what the lease is not, next to the number an operator sets.
//
// It reads as the length of a job and it is not one: it is the system's hold on a job, renewed on
// every tick for as long as the job runs. The credential a session runs under is a different
// lifetime and this setting does not reach it. An operator who read the two as one number set the
// lease to fifteen minutes to cover their work and got no change to the credential at all.
const leaseMeans = "  (the system's hold on a job, not the life of a session's credential)"

func leaseOr(seconds int32) string {
	if seconds == 0 {
		return "the system's own"
	}
	return (time.Duration(seconds) * time.Second).String()
}

// limitsFlagsTaken is every flag krewe limits takes, which is what keeps the tool's refusal of flags
// from refusing these.
func limitsFlagsTaken() map[string]bool {
	return map[string]bool{
		flagMaxDeclared: true, flagMaxDepth: true, flagMaxRunning: true, flagBudgetTokens: true, flagLease: true,
		flagReclaim: true, flagArchive: true, flagWaiting: true,
		flagRequestMemory: true, flagRequestProcessor: true,
		flagContextCeiling: true,
	}
}

// ceilingOf is the share of its context window a session here may fill before the system gives it no
// new task, which is the workspace's where it set one and the system's where it did not.
func ceilingOf(limits *quaycrewv1.WorkspaceLimits) int32 {
	if set := limits.GetContextCeilingPercent(); set > 0 {
		return set
	}
	return job.DefaultContextCeiling
}

// ceilingMeans says where the number came from, because this one is not measured and reads exactly
// like the ones that are. It also says what happens at it, since a limit that only prints a share
// tells an operator nothing about what the system does when a session reaches it.
func ceilingMeans(limits *quaycrewv1.WorkspaceLimits) string {
	if limits.GetContextCeilingPercent() > 0 {
		return "  (a session past this hands the rest of its job to a fresh one)"
	}
	return "  (the system's own, from a standard rather than from any measurement of this system)"
}

// waitingOf is how long a job may wait for a person here before the telling names the age beside it,
// which is the workspace's where it set one and the system's where it did not.
func waitingOf(limits *quaycrewv1.WorkspaceLimits) string {
	if set := limits.GetWaitingSeconds(); set > 0 {
		return (time.Duration(set) * time.Second).String()
	}
	return job.DefaultWaiting.String()
}

// waitingMeans says where the number came from, because this one is a guess and reads exactly like
// the ones that are measured. It also says what happens at it: a limit that only prints a length
// tells an operator nothing about what the system does when a wait passes it.
func waitingMeans(limits *quaycrewv1.WorkspaceLimits) string {
	if limits.GetWaitingSeconds() > 0 {
		return "  (past this the telling names how long the job has waited)"
	}
	return "  (the system's own, a guess: the median gap from a wait starting to its telling replaces it)"
}
