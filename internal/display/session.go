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
func SessionColumns() []string {
	return []string{"id", "workspace", "project", "name", "status", "mode", "ctx", "in", "out", "cache", "age"}
}

// SessionCells is one session as a listing shows it, matching SessionColumns.
func SessionCells(session *quaycrewv1.Session, workspaceName, projectName string) []string {
	return []string{
		ShortID(session.GetId()),
		Name(workspaceName, session.GetWorkspace()),
		Name(projectName, session.GetProject()),
		SessionName(session),
		StatusLabel(session),
		PermissionLabel(session.GetPermissionMode()),
		ContextLabel(session),
		Tokens(session.GetUsage().GetInput()),
		Tokens(session.GetUsage().GetOutput()),
		Tokens(session.GetUsage().GetCacheRead()),
		// How long ago it was put away where it was, and how long ago it was touched otherwise. A
		// live session has no archived stamp, so one rule covers both.
		Age(LastMoved(session)),
	}
}

// ContextLabel is how full the model's context window is: a share where the crew knows how big the
// window is, and the count on its own where nothing has told it yet.
//
// The count rather than a guessed share, because a share is what an operator acts on and a wrong one
// is worse than none. The crew learns the size from the model runtime the first time anybody attaches
// to a session in that workspace.
//
// Blank for a conversation nobody has spoken in, the way the token columns are: a session with no
// conversation behind it has not filled anything.
func ContextLabel(session *quaycrewv1.Session) string {
	window := session.GetContextWindow()
	if window.GetUsed() <= 0 {
		return ""
	}
	if window.GetSize() <= 0 {
		return Tokens(window.GetUsed())
	}
	return strconv.FormatInt(Share(window.GetUsed(), window.GetSize()), 10) + "%"
}

// Share is what part of the window is used, as a whole number out of a hundred.
//
// It stops at a hundred: a window reported as more than full is a conversation the runtime is about
// to compact, and a hundred and four per cent reads as a defect in the crew. Nothing is multiplied
// above that ceiling, so a nonsense count cannot overflow into a nonsense share.
func Share(used, size int64) int64 {
	switch {
	case used <= 0 || size <= 0:
		return 0
	case used >= size:
		return 100
	default:
		return (used*100 + size/2) / size
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
		return "-"
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

// SessionName is what to call a session in a listing: the name the operator gave it, then the one the
// crew wrote for itself, then the identifier.
//
// The operator's name wins because a name somebody picked beats a name a machine wrote. The
// identifier is last because it is the thing nobody remembers, and it is still in the id column
// beside this one, so nothing is hidden by preferring a name.
func SessionName(session *quaycrewv1.Session) string {
	if label := strings.TrimSpace(session.GetLabel()); label != "" {
		return label
	}
	if described := strings.TrimSpace(session.GetDescription()); described != "" {
		return described
	}
	return ShortID(session.GetHandle())
}
