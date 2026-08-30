package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
)

// A session says what it is about, written by the system rather than by the operator.
//
// A listing is a column of hexadecimal, and a label fixes that only for the sessions somebody stopped
// to name. Naming things is job and nobody does it consistently, so the half that actually gets used
// is the one the system writes itself.
//
// It never touches the label. A name somebody picked is the one thing in a listing that is certainly
// right, and nothing automatic is allowed to overwrite it.

const (
	// describeEveryDefault is how many tasks past its description a conversation goes before it is
	// described again.
	//
	// Ten is a starting number, not a measured one. Nothing here has been running long enough to say
	// how far a conversation drifts per task, so this is set where it can be changed rather than
	// presented as derived: QC_DESCRIBE_EVERY in the system's configuration. What would replace it is a
	// count of how often a re-description actually differs from the one before it.
	describeEveryDefault = 10
	// descriptionLimit is how much of a description is kept. It shares a column with the operator's
	// own label, so it is capped at the same kind of length for the same reason: a listing gives the
	// name one column among ten.
	descriptionLimit = 60
	// describeTasks is how much of a conversation is read to describe it. The opening exchange says
	// what a conversation is for better than the middle of it does, and reading the whole thing would
	// cost more than the task being described.
	describeTasks = 6
)

// DescribeEvery reads how often a session is described from what the system was configured with.
//
// "off" and zero both task it off, because a system running automation makes a session per run and
// should be able to pay for none of this. Anything unreadable keeps the default rather than refusing:
// the system starting matters more than this setting being exactly right, which is the opposite of the
// permission mode, where being wrong changes what a session may do.
func DescribeEvery(configured string) int {
	value := strings.ToLower(strings.TrimSpace(configured))
	if value == "" {
		return describeEveryDefault
	}
	if value == "off" || value == "no" || value == "false" {
		return 0
	}
	every, err := strconv.Atoi(value)
	if err != nil || every < 0 {
		return describeEveryDefault
	}
	return every
}

// worthDescribing says whether a session's description has fallen behind its conversation.
//
// The first task is always worth describing, because until then the listing has nothing but an
// identifier. After that it is tasks since, not tasks total: a session described at task one is
// described again at eleven, not at ten.
func worthDescribing(tasks, describedAtTask, every int) bool {
	if every <= 0 || tasks == 0 {
		return false
	}
	if describedAtTask == 0 {
		return true
	}
	return tasks-describedAtTask >= every
}

// tidyDescription is what the model said, as a listing can hold it: the first line, unquoted,
// trimmed and capped.
//
// The model is asked for one line and does not always give one. What comes back goes straight into a
// row, so a paragraph draws a row several rows tall and breaks the cursor, and a quoted answer puts
// quotation marks in the middle of a listing.
func tidyDescription(said string) string {
	line := strings.TrimSpace(said)
	if cut := strings.IndexAny(line, "\r\n"); cut >= 0 {
		line = strings.TrimSpace(line[:cut])
	}
	line = strings.TrimSpace(strings.Trim(line, `"'`))
	if runes := []rune(line); len(runes) > descriptionLimit {
		return strings.TrimSpace(string(runes[:descriptionLimit]))
	}
	return line
}

// describePrompt asks for the one line, from the conversation so far.
//
// It says what not to write as firmly as what to write. Asked without that, a model answers with a
// title in its own voice, and a listing of "an engaging exploration of agent tooling" is worse than a
// listing of hexadecimal because it takes longer to read and says less.
func describePrompt(tasks []*quaycrewv1.Task) string {
	var conversation strings.Builder
	for _, task := range tasks {
		fmt.Fprintf(&conversation, "asked: %s\n", oneLine(task.GetPrompt(), 300))
		if reply := task.GetReply(); reply != "" {
			fmt.Fprintf(&conversation, "answered: %s\n", oneLine(reply, 300))
		}
	}
	return "Here is the start of a conversation:\n\n" + conversation.String() +
		"\nIn one short line, say what this conversation is for, in plain words, as a person would " +
		"describe it to a colleague. For example \"blog post about the agentic harness\" or " +
		"\"fixing the payout job\". No title case, no quotation marks, no adjectives about how " +
		"interesting it is, and nothing but the line itself."
}

// oneLine flattens text to a single line and caps it, so a long task does not become most of the
// prompt that describes it.
func oneLine(text string, limit int) string {
	flat := strings.Join(strings.Fields(text), " ")
	if runes := []rune(flat); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return flat
}

// describeSession writes what a session is about, if it has fallen behind.
//
// It takes the session's id rather than the session, and reads everything it needs again, because it
// runs behind a task that has already been answered: anything handed to it would be a value somebody
// else is still reading. That is the same mistake that made `quay flow start` fail one run in six.
//
// Every failure is a log line and nothing else. A description is a convenience, and a task that
// worked must not be reported as failed because the system could not think of a name for it.
func (s *Server) describeSession(ctx context.Context, sessionID string) {
	if s.describeEvery <= 0 {
		return
	}
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		slog.DebugContext(ctx, "a session could not be described", "session", sessionID, "error", err)
		return
	}
	tasks, err := s.store.CountTasks(ctx, sessionID)
	if err != nil {
		slog.DebugContext(ctx, "a session could not be described", "session", sessionID, "error", err)
		return
	}
	if !worthDescribing(tasks, int(session.GetDescribedAtTask()), s.describeEvery) {
		return
	}

	history, err := s.store.ListTasks(ctx, sessionID, describeTasks)
	if err != nil || len(history) == 0 {
		slog.DebugContext(ctx, "a session could not be described", "session", sessionID, "error", err)
		return
	}
	box, err := s.sandboxFor(ctx, session)
	if err != nil {
		slog.DebugContext(ctx, "a session could not be described", "session", sessionID, "error", err)
		return
	}
	// Its own conversation, not the session's. Describing inside the session would put a request the
	// operator never made into their history, and would add its tokens to what the listing says the
	// conversation cost, so the cost column would stop describing the job.
	prompt := describePrompt(history)
	said, err := s.runner.Run(ctx, box, model.Request{
		Text:           prompt,
		PermissionMode: model.PermissionPlan,
		Env:            s.taskEnv(ctx, session, ""),
	})
	if err != nil {
		slog.DebugContext(ctx, "a session could not be described", "session", sessionID, "error", err)
		return
	}
	description := tidyDescription(said.Reply)
	if description == "" || isTheQuestionBack(prompt, description) {
		return
	}
	if err := s.store.SetDescription(ctx, sessionID, description, tasks); err != nil {
		slog.DebugContext(ctx, "a session's description could not be kept", "session", sessionID, "error", err)
	}
}

// isTheQuestionBack says whether what came back is the question rather than an answer to it.
//
// A backend that echoes is the obvious case, and continuous integration runs one, so without this the
// system names every session "Here is the start of a conversation:". It is worth guarding beyond that
// though: a model that answers badly enough to hand the instruction back would put the instruction in
// the listing, where it is worse than the identifier it replaced, and nothing else would notice.
//
// Whole lines, not any occurrence. The question carries examples of what a good answer looks like, so
// a model that produced exactly one of those examples would have its answer thrown away by a check
// that asked only whether the text appears somewhere in the prompt.
func isTheQuestionBack(prompt, description string) bool {
	if description == "" {
		return false
	}
	wanted := strings.ToLower(description)
	for _, line := range strings.Split(prompt, "\n") {
		if strings.ToLower(strings.TrimSpace(line)) == wanted {
			return true
		}
	}
	return false
}
