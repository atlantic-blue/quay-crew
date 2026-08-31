package display

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/contextspend"
	"github.com/atlantic-blue/krewe/internal/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionColumns names what a listing of sessions says, in order.
//
// One list, because the console and the command line used to answer the same question differently:
// the console had ten columns and the command line four, so a session's cost, its mode and how long
// ago it was touched were visible in one place and invisible in the other. Whichever surface an
// operator learns first should teach them the other.
// The first column is headed "session" rather than "id" because it is the value every command takes.
// Under "id" it read as bookkeeping, and the operator reached for the name cell instead, which held
// the other identifier.
func SessionColumns() []string {
	return []string{"session", "workspace", "project", "name", "status", "mode", "ctx", "spent on",
		"in", "out", "cache", "age"}
}

// SessionCells is one session as a listing shows it, matching SessionColumns.
func SessionCells(one *quaycrewv1.Session, workspaceName, projectName string) []string {
	return []string{
		ShortID(one.GetId()),
		Name(workspaceName, one.GetWorkspace()),
		Name(projectName, one.GetProject()),
		SessionLabel(one),
		StatusLabel(one),
		PermissionLabel(one.GetPermissionMode()),
		ContextLabel(one),
		Spend(one).Cell(),
		Tokens(one.GetUsage().GetInput()),
		Tokens(one.GetUsage().GetOutput()),
		Tokens(one.GetUsage().GetCacheRead()),
		// How long ago the session moved, which is the same stamp the listing is ordered by. Both the
		// column and the order read it from the session package, so the two cannot come apart.
		Age(session.LastMoved(one)),
	}
}

// ContextLabel is how full the model's context window is: a share where the system knows how big the
// window is, and the count on its own where nothing has told it yet.
//
// The count rather than a guessed share, because a share is what an operator acts on and a wrong one
// is worse than none. The system learns the size from the model runtime the first time anybody attaches
// to a session in that workspace.
//
// Blank for a conversation nobody has spoken in, the way the token columns are: a session with no
// conversation behind it has not filled anything.
//
// A share on its own says nothing about whether it is a problem. The column used to print one and an
// operator had to hold the workspace's ceiling in their head to read it, so the word beside the number
// says what the system is about to do: over means this session is given no new work on its job and
// hands the rest over, and near means it is inside the band below that.
func ContextLabel(session *quaycrewv1.Session) string {
	window := session.GetContextWindow()
	if window.GetUsed() <= 0 {
		return ""
	}
	if window.GetSize() <= 0 {
		return Tokens(window.GetUsed())
	}
	share := Share(window.GetUsed(), window.GetSize())
	return strconv.FormatInt(share, 10) + "%" + againstTheCeiling(share, window.GetCeiling())
}

// NearBand is how far below the ceiling a session starts reading as near it, in points of the share.
//
// The band rather than a second setting, because the standard the ceiling comes from names a band
// rather than a point: quality falls off between 50 and 70 per cent of a window and is poor past 70.
// So a ceiling of 70 marks a session from 50, and a ceiling an operator moves takes its band with it.
// It is as provisional as the ceiling internal/job refuses work at, and it lives here because saying
// which sessions are near one is a listing's job: the gate itself reads no band at all.
const NearBand = 20

// againstTheCeiling is the word beside the share, and nothing where the system stated no ceiling. A
// mark against a number nobody answered with would be the console inventing a limit.
func againstTheCeiling(share int64, ceiling int32) string {
	switch {
	case ceiling <= 0:
		return ""
	case share >= int64(ceiling):
		return " over"
	case share >= int64(ceiling)-NearBand:
		return " near"
	default:
		return ""
	}
}

// Spend is where a session's context went, in the form the accounting works in.
//
// The listing next to it says how full the window is, and a share on its own moves nothing. This is
// the column that says what to look at: whether the session filled up on the code it had to read, on
// tool output it read once, or on its own repeated attempts.
//
// The zero value for a conversation nobody has spoken in, which prints as an empty cell the way the
// token columns beside it do.
func Spend(session *quaycrewv1.Session) contextspend.Spend {
	where := session.GetContextSpend()
	return contextspend.Spend{
		Reads: where.GetReads(), Tools: where.GetTools(),
		Turns: where.GetTurns(), Told: where.GetTold(),
	}
}

// Share is what part of the window is used, as a whole number out of a hundred.
//
// It stops at a hundred: a window reported as more than full is a conversation the runtime is about
// to compact, and a hundred and four per cent reads as a defect in the system. Nothing is multiplied
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

// The words a listing shows for a session whose row says idle, once the system has read what is
// actually inside its sandbox.
//
// They exist because idle used to cover all four. The row's status only ever said whether a
// dispatched task was open, so a conversation somebody opened by hand and left answering read the
// same as an empty container, and idle is the word that invites a restart, a drain or a reclaim.
//
// Why these words:
//
//   - Awake, not thinking or busy. The system reads a runtime process, which is up both while it
//     answers and while it waits at a prompt, so thinking claims more than was measured. Busy is
//     what running already means to an operator, and this is not a task.
//   - Attached, because it is what the operator typed to get there: `krewe attach`.
//   - Unknown, because the system asked and was not told. It is not idle, and it must never read as
//     idle: a listing that guesses empty here is the defect this set of words was written for.
//   - Idle keeps its word and finally earns it: nothing is running and nobody is in there.
const (
	StatusIdle     = "idle"
	StatusAwake    = "awake"
	StatusAttached = "attached"
	StatusUnknown  = "unknown"
)

// SessionStatus is the one word a listing shows for where a session is.
//
// It is the row's own status, except where that status is idle and the system has read the sandbox. A
// row that says anything else is left alone: running, failed, stopped and reclaimed each carry
// something this cannot say, and overwriting failed with awake would lose that the last task did not
// land.
//
// A session nobody asked about reads exactly as it did before, which is what a caller that has not
// asked for presence should get.
func SessionStatus(session *quaycrewv1.Session) string {
	if session.GetStatus() != StatusIdle {
		return session.GetStatus()
	}
	switch session.GetPresence() {
	case quaycrewv1.SessionPresence_SESSION_PRESENCE_ATTACHED:
		return StatusAttached
	case quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE:
		return StatusAwake
	case quaycrewv1.SessionPresence_SESSION_PRESENCE_UNTOLD:
		return StatusUnknown
	default:
		return StatusIdle
	}
}

// StatusLabel is the status cell, carrying the stale mark when the session's live sandbox was born
// before the workspace's current skills: the cue that stopping and restarting it gets a sandbox born
// current.
func StatusLabel(session *quaycrewv1.Session) string {
	if session.GetStale() {
		return SessionStatus(session) + " stale"
	}
	return SessionStatus(session)
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
// cost zero, it has no cost. A column of zeroes would read as a system that is free.
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

// SessionName is what to call a session where one word has to stand for it: the name it is called,
// then the identifier a listing prints.
//
// The identifier is last, and it is the id rather than the handle: the id is the value the session
// column carries, so a breadcrumb falling back to it falls back to something the operator can type.
func SessionName(session *quaycrewv1.Session) string {
	if named := SessionLabel(session); named != "" {
		return named
	}
	return ShortID(session.GetId())
}

// SessionLabel is what a session is called, and nothing else. Empty until somebody names it.
//
// Three names, in the order of how much a reader should trust them: the label the operator typed
// about this conversation, the title it was dispatched with, then the line the system wrote about it.
//
// The label is first because it is the last word of the person who has seen the session, and it is
// the only one of the three they can change. The title comes next because a person typed it too, at
// declaration, about the job this session was made for. The description is last because a model
// wrote it.
//
// The title is what fills the cell while the work is happening. A label needs an operator who has
// already looked, and a description is written behind a task that has landed, so a job, which is one
// long task, ran to the end with a blank name cell: four running jobs and no way to tell which was
// which.
//
// The name cell used to fall back to the handle, which put a raw identifier under the heading "name"
// and took it off the screen again the moment the session was labelled. Two identifiers were on the
// screen and neither was in a column that said so.
func SessionLabel(session *quaycrewv1.Session) string {
	if label := strings.TrimSpace(session.GetLabel()); label != "" {
		return label
	}
	if title := strings.TrimSpace(session.GetTitle()); title != "" {
		return title
	}
	return strings.TrimSpace(session.GetDescription())
}
