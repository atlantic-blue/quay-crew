package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/job"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The flags a history takes. Three, and each one narrows: two ends of a window and how much of it to
// print. Nothing here says where, because where is an address.
const (
	flagSince = "--since"
	flagUntil = "--until"
	flagLimit = "--limit"
)

// historyFlagsTaken is what `krewe history` accepts, for the refusal every other flag gets.
func historyFlagsTaken() map[string]bool {
	return map[string]bool{flagSince: true, flagUntil: true, flagLimit: true}
}

// runHistory says what the crew did over a window: what ran, what it cost, and what failed and why.
//
// This is the read a session makes instead of being told. Before it, a session could read the
// repository it stood in and nothing else, so every fact about the crew's own work was typed into a
// brief by hand and the operator was the memory.
func runHistory(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: krewe history [<workspace>/<project>|system] [%s <date>] [%s <date>] [%s <n>]",
			flagSince, flagUntil, flagLimit)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}

	request := &quaycrewv1.GetHistoryRequest{}
	if request.Since, err = readMoment(values.first(flagSince), flagSince, dayStarts); err != nil {
		return err
	}
	if request.Until, err = readMoment(values.first(flagUntil), flagUntil, dayEnds); err != nil {
		return err
	}
	if limit := values.first(flagLimit); limit != "" {
		count, err := strconv.Atoi(limit)
		if err != nil || count < 1 {
			return fmt.Errorf("%s takes a count of jobs, and %q is not one", flagLimit, limit)
		}
		request.Limit = int32(count)
	}

	where := systemWide("jobs")
	if !readsTheSystem(typed) {
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		request.Workspace, request.Project = located.WorkspaceID, located.ProjectID
		where = narrowedTo("jobs", located.Path.String(), "krewe history system reads every project")
	}

	resp, err := client.GetHistory(ctx, request)
	if err != nil {
		return err
	}
	writeHistory(out, resp, where)
	return nil
}

// dayStarts and dayEnds are what a bare date means at each end of a window. A person who writes
// --since 2026-08-28 --until 2026-08-30 means three whole days, so the near end opens at midnight and
// the far end closes at the end of that day. Reading both as midnight would silently drop the last
// day, which is the kind of wrong nobody checks.
const (
	dayStarts = false
	dayEnds   = true
)

// readMoment reads a date or a whole moment off the command line, and refuses anything else by name.
//
// Two spellings, because a person asking what happened last week writes a date and a machine passing
// a window along writes the moment it already holds.
func readMoment(typed, flag string, toTheEnd bool) (*timestamppb.Timestamp, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return nil, nil
	}
	if at, err := time.Parse(time.RFC3339, typed); err == nil {
		return timestamppb.New(at.UTC()), nil
	}
	at, err := time.Parse(time.DateOnly, typed)
	if err != nil {
		return nil, fmt.Errorf("%s takes a date written as 2026-08-28, or a moment written as "+
			"2026-08-28T15:04:05Z, and %q is neither", flag, typed)
	}
	if toTheEnd {
		at = at.AddDate(0, 0, 1)
	}
	return timestamppb.New(at.UTC()), nil
}

// writeHistory draws the answer: the window, the total, then one line for each job.
//
// The total goes above the rows rather than under them, unlike every listing in this tool. A reader
// who only wants to know what the week cost stops after two lines, and a reader who wants the jobs
// reads on. Under the rows it would be the thing you scroll to find.
func writeHistory(out io.Writer, resp *quaycrewv1.GetHistoryResponse, where scope) {
	total := resp.GetTotal()
	fmt.Fprintf(out, "%s to %s%s\n", onDay(resp.GetSince()), onDay(resp.GetUntil()), inWhere(where))
	if total.GetJobs() == 0 {
		fmt.Fprintln(out, "\nno jobs were declared in that window")
		fmt.Fprintf(out, "widen it with %s and %s, or read further back\n", flagSince, flagUntil)
		return
	}

	fmt.Fprintf(out, "\n%s: %s\n", plural(int(total.GetJobs()), "jobs"), endings(total))
	fmt.Fprintf(out, "%s, %s\n", cost(total.GetSpentTokens()), worked(total.GetWorkingSeconds()))
	fmt.Fprintf(out, "%s, %s\n",
		plural(int(total.GetPullRequests()), "pull requests"), job.Steers(int(total.GetSteers())))

	fmt.Fprintln(out)
	for _, one := range resp.GetJobs() {
		fmt.Fprintf(out, "%-10s %-13s %-12s %-8s %7s %5s  %s\n",
			display.ShortID(one.GetId()), onDayAndTime(one.GetCreatedAt()), ranAs(one), one.GetPhase(),
			display.Tokens(one.GetSpentTokens()), took(one), truncateLine(one.GetTitle()))
		// Why it ended, under the job it ended. "What failed and why" is one question, and a reader
		// who had to ask again for every failure is back where they started.
		if reason := one.GetReason(); reason != "" {
			fmt.Fprintf(out, "%-10s   %s\n", "", truncateLine(reason))
		}
		if pull := one.GetPullRequest(); pull != "" {
			fmt.Fprintf(out, "%-10s   %s\n", "", pull)
		}
	}

	// What the limit cut off, said out loud. A cap nobody is told about reads as complete coverage,
	// which is the one way a bounded read can lie to the reader it exists to serve.
	if left := resp.GetLeftOut(); left > 0 {
		fmt.Fprintf(out, "\n%s not shown: raise %s to read them\n", plural(int(left), "jobs"), flagLimit)
	}
}

// ranAs is the role a job ran as, and a dash for a job that ran as nobody in particular, so the column
// stays a column.
func ranAs(one *quaycrewv1.JobDigest) string {
	if one.GetRole() == "" {
		return "-"
	}
	return one.GetRole()
}

// took is how long a job ran, and nothing for one that has not finished. Nothing rather than a zero,
// because a job still running has not taken no time.
func took(one *quaycrewv1.JobDigest) string {
	started, finished := one.GetStartedAt(), one.GetFinishedAt()
	if started == nil || finished == nil || !finished.AsTime().After(started.AsTime()) {
		return ""
	}
	return compact(finished.AsTime().Sub(started.AsTime()))
}

// endings says how the window's jobs ended, leaving out the words that count nothing. A line reading
// "0 failed" on a clean week is noise, and it makes the one that says "3 failed" harder to see.
func endings(total *quaycrewv1.HistoryTotals) string {
	said := []string{}
	for _, one := range []struct {
		count int32
		word  string
	}{
		{total.GetDone(), "done"}, {total.GetFailed(), "failed"},
		{total.GetStopped(), "stopped"}, {total.GetUnfinished(), "still going"},
	} {
		if one.count > 0 {
			said = append(said, fmt.Sprintf("%d %s", one.count, one.word))
		}
	}
	return strings.Join(said, ", ")
}

// compact renders a duration in the units a person reads it in: 4m, 2h18m, 3d4h.
func compact(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(elapsed.Hours())/24, int(elapsed.Hours())%24)
	}
}

// onDay and onDayAndTime are how a history writes a moment: the day for the window, and the day with
// the time for a row, because two jobs on one day are told apart by the time and not by the date.
func onDay(stamp *timestamppb.Timestamp) string {
	if stamp == nil {
		return "-"
	}
	return stamp.AsTime().UTC().Format("2 January 2006")
}

func onDayAndTime(stamp *timestamppb.Timestamp) string {
	if stamp == nil {
		return "-"
	}
	return stamp.AsTime().UTC().Format("2 Jan 15:04")
}

// inWhere names the address a history read, or says nothing for one that read every project. The
// scope under a listing says this too, and a history says it at the top because the total is the
// first thing read and a total from one project must not be taken for the crew's.
func inWhere(where scope) string {
	if where.where == "" {
		return ", every project"
	}
	return ", " + where.where
}

// cost is what the window spent. A total of zero says so in words, because display.Tokens renders
// nothing at all for zero: a column of zeroes down a listing would read as a system that is free,
// and the same blank in a summary line reads as a missing number.
func cost(tokens int64) string {
	if tokens <= 0 {
		return "nothing spent yet"
	}
	return display.Tokens(tokens) + " tokens"
}

// worked is how long the window's jobs ran, added up over the ones that both started and finished.
func worked(seconds int64) string {
	if seconds <= 0 {
		return "no time measured yet"
	}
	return compact(time.Duration(seconds)*time.Second) + " working"
}
