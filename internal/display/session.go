package display

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionColumns names what a listing of sessions says, in order.
//
// One list, because the console and the command line used to answer the same question differently:
// the console had ten columns and the command line four, so a session's cost, its mode and how long
// ago it was touched were visible in one place and invisible in the other. Whichever surface an
// operator learns first should teach them the other.
//
// The first column is the handle, which is the identifier an address takes. The session's own id
// used to be there, and no command would accept it: `quay dispatch me/website/a4db600a` came back
// with "this crew has no session a4db600a. it has: 5ae35d77", naming a value that was nowhere on the
// screen. What is printed has to be typeable back, so the column prints what the address wants.
func SessionColumns() []string {
	return []string{"session", "workspace", "project", "name", "status", "mode", "in", "out", "cache", "age"}
}

// SessionCells is one session as a listing shows it, matching SessionColumns.
func SessionCells(session *quaycrewv1.Session, workspaceName, projectName string) []string {
	return []string{
		ShortID(session.GetHandle()),
		Name(workspaceName, session.GetWorkspace()),
		Name(projectName, session.GetProject()),
		SessionLabel(session),
		StatusLabel(session),
		PermissionLabel(session.GetPermissionMode()),
		Tokens(session.GetUsage().GetInput()),
		Tokens(session.GetUsage().GetOutput()),
		Tokens(session.GetUsage().GetCacheRead()),
		// How long ago it was put away where it was, and how long ago it was touched otherwise. A
		// live session has no archived stamp, so one rule covers both.
		Age(LastMoved(session)),
	}
}

// StatusLabel is the status cell, carrying the stale mark when the session's live sandbox was born
// before the workspace's current skills: the cue that stopping and restarting it gets a sandbox born
// current.
func StatusLabel(session *quaycrewv1.Session) string {
	if session.GetStale() {
		return session.GetStatus() + " stale"
	}
	return session.GetStatus()
}

// PermissionLabel never leaves the cell blank: a session from before the mode existed runs
// acceptEdits, and an empty cell would read as "asks first", the opposite. bypassPermissions becomes
// "dangerous", the only one of the three worth spotting from across a list.
func PermissionLabel(mode string) string {
	switch mode {
	case "bypassPermissions":
		return "dangerous"
	case "plan":
		return "plan"
	default:
		return "edits"
	}
}

// Tokens is a count in a column seven characters wide: 52, 6.9k, 1.7M.
//
// Nothing at all for a session that has spent nothing, because a conversation nobody has had has not
// cost zero, it has no cost. A column of zeroes would read as a crew that is free.
func Tokens(count int64) string {
	switch {
	case count <= 0:
		return ""
	case count < 1000:
		return strconv.FormatInt(count, 10)
	case count < 1_000_000:
		return trimZero(float64(count)/1000) + "k"
	case count < 1_000_000_000:
		return trimZero(float64(count)/1_000_000) + "M"
	default:
		return trimZero(float64(count)/1_000_000_000) + "B"
	}
}

// trimZero renders one decimal place, and drops it when it says nothing: 1.7 and 12 rather than 1.7
// and 12.0, so the column stays narrow where it can.
func trimZero(value float64) string {
	rendered := strconv.FormatFloat(value, 'f', 1, 64)
	return strings.TrimSuffix(rendered, ".0")
}

// LastMoved is when the session last went anywhere: when it was put away if it was, and when it was
// last touched otherwise.
func LastMoved(session *quaycrewv1.Session) *timestamppb.Timestamp {
	if session.GetArchivedAt() != nil {
		return session.GetArchivedAt()
	}
	return session.GetUpdatedAt()
}

// Age renders how long ago a timestamp was, compactly. An unset timestamp shows a dash rather than
// fifty years, which is what the zero value would otherwise read as.
func Age(stamp *timestamppb.Timestamp) string {
	if stamp == nil || !stamp.IsValid() || stamp.AsTime().IsZero() {
		return emptyCell
	}
	return compactDuration(time.Since(stamp.AsTime()))
}

func compactDuration(elapsed time.Duration) string {
	switch {
	case elapsed < 0:
		return "0s"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours())/24)
	}
}

// Rows renders a listing as aligned columns with a header, for a surface with no table to draw into.
// Each column is as wide as its widest cell, so nothing is cut: the command line has the whole
// terminal and cutting a value there helps nobody.
func Rows(columns []string, rows [][]string) string {
	widths := make([]int, len(columns))
	for index, title := range columns {
		widths[index] = len(title)
	}
	for _, row := range rows {
		for index, cell := range row {
			if index < len(widths) && len(cell) > widths[index] {
				widths[index] = len(cell)
			}
		}
	}
	var out strings.Builder
	writeRow(&out, widths, columns)
	for _, row := range rows {
		writeRow(&out, widths, row)
	}
	return out.String()
}

func writeRow(out *strings.Builder, widths []int, cells []string) {
	for index, cell := range cells {
		if index > 0 {
			out.WriteString("  ")
		}
		out.WriteString(cell)
		// Not the last one: trailing spaces on the end of a line are invisible and get copied.
		if index < len(cells)-1 && index < len(widths) {
			out.WriteString(strings.Repeat(" ", widths[index]-len(cell)))
		}
	}
	out.WriteString("\n")
}

// SessionLabel is the name cell of a listing: what the operator called this session, then what the
// crew called it, then nothing at all.
//
// Nothing at all, rather than the identifier, because the identifier is already the first column of
// the same row. Repeating it there made a name look like an identifier that had lost its label, and
// it is the reason the handle went missing from the screen the moment a session was named.
func SessionLabel(session *quaycrewv1.Session) string {
	if label := strings.TrimSpace(session.GetLabel()); label != "" {
		return label
	}
	if described := strings.TrimSpace(session.GetDescription()); described != "" {
		return described
	}
	return emptyCell
}

// SessionName is what to call a session anywhere one name is all there is room for: the name the
// operator gave it, then the one the crew wrote for itself, then the identifier.
//
// The operator's name wins because a name somebody picked beats a name a machine wrote. The
// identifier is last because it is the thing nobody remembers, and it is never blank: a breadcrumb
// or a page title with a gap in it reads as a bug.
func SessionName(session *quaycrewv1.Session) string {
	if named := SessionLabel(session); named != emptyCell {
		return named
	}
	return ShortID(session.GetHandle())
}
