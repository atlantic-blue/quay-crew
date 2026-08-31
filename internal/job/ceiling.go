package job

import (
	"fmt"
	"strings"
	"time"

	"github.com/atlantic-blue/krewe/internal/display"
)

// A session used to run until its context window was full.
//
// The system printed the share and did nothing with it, so a session at eighty per cent kept taking
// tasks, and the last task of a long job is the one that opens the pull request and writes the
// answer. The work that matters most was done at the point where the model is worst.
//
// So the share becomes a gate rather than a column. Past the workspace's ceiling the system gives
// that session no new task on the job it is doing. It asks it for one thing instead, a handoff, and
// then carries the job on in a fresh session that starts from what the handoff says.
//
// Three properties this has to keep, and each of them is a way of getting it wrong:
//
//   - A window the system cannot measure never refuses. Nothing tells the system how big a model's
//     context window is until a session in that workspace writes the size down, and a gate that read
//     silence as full would stop every job on a system that has not been told yet.
//   - A handoff that says nothing is refused rather than written. A fresh session started from an
//     empty handoff is a fresh session started from nothing, which costs more than the session at
//     eighty per cent it replaced.
//   - The handoff ask is the last task that session gets, not a task on top of the work. A gate that
//     asked for a handoff and then sent the work anyway would have spent a task to change nothing.

// DefaultContextCeiling is how full a session's context window may be before the system gives it no
// new task, where the workspace has not said.
//
// **This number is provisional and it is not measured.** It is taken from the standard quay-crew#539
// names, which says quality falls off between 50 and 70 per cent of a window and is poor past 70. No
// run of this crew produced it. What would replace it is a measurement of this crew's own answers
// against how full the window was when each was written, and nobody has taken one.
const DefaultContextCeiling = 70

// ContextCeiling is how full a session's context window may be in this workspace, or the system's own
// where the workspace says nothing.
//
// Zero takes the system's own rather than meaning off, which is the opposite of what reclaim and
// archive do on the same row, and it is deliberate: those two ship with no number because none has
// been measured, and this one ships with a number the standard names. A workspace that wants no gate
// sets 100, which refuses nothing until the window is actually full.
func (l Limits) ContextCeiling() int {
	if l.ContextCeilingPercent > 0 {
		return l.ContextCeilingPercent
	}
	return DefaultContextCeiling
}

// PastTheCeiling says whether a conversation carrying `used` of a `size` window is at or past the
// ceiling, and so takes no new task.
//
// This is the gate. Saying which sessions are near one is a listing's job and lives in
// internal/display beside the share it renders, because that is the only place it is read.
//
// A window of no size is not past anything. The size is what the model runtime last told a session in
// that workspace, so a system nothing has told holds no opinion, and holding one would mean refusing
// work over a share the system made up.
func PastTheCeiling(used, size int64, ceiling int) bool {
	if size <= 0 || used <= 0 || ceiling <= 0 {
		return false
	}
	return display.Share(used, size) >= int64(ceiling)
}

// ShareOf is how full a window is, as a whole number out of a hundred.
//
// Worked out where the console works it out, so a refusal, the line under an operator's prompt and a
// listing can never disagree about the same conversation.
func ShareOf(used, size int64) int { return int(display.Share(used, size)) }

// HandoffLimit is how long each half of a handoff may be.
//
// It is a quarter of a brief. The whole handoff goes in front of the fresh session beside the job's
// brief, so two halves at this ceiling plus a brief is still something a model reads to the end,
// which is the reason the brief has a ceiling at all.
const HandoffLimit = BriefLimit / 4

// Handoff is the state one session left behind on a job, on the record.
//
// It is what the session said rather than anything the system watched, the way a step and an answer
// are. That is also what makes it the only thing that survives: a controller cannot see inside a
// container, and a session that stops taking work takes everything it did not write down with it.
type Handoff struct {
	Job  string
	Seq  int
	Left string
	// Tried is what this session tried that did not work. Empty is a real answer and not a missing
	// one: a session that hit no dead end has none to report.
	Tried string
	// Session is the conversation that wrote it. It is what tells a handoff waiting to be taken up
	// from one a fresh session already holds, so the system does not hand the same words over twice.
	Session   string
	WrittenAt time.Time
}

// TidyHandoff is a handoff as the system keeps it, and the refusal where it could not be kept.
//
// What is left is refused when it says nothing, and that refusal is the whole point of the record:
// a fresh session given an empty handoff starts from nothing, which is more expensive than the
// session at the ceiling it replaced. What was tried is not refused for being empty, because a
// session that hit no dead end has nothing to say and inventing one would be worse than silence.
func TidyHandoff(left, tried string) (string, string, error) {
	left, tried = strings.TrimSpace(left), strings.TrimSpace(tried)
	switch {
	case left == "":
		return "", "", fmt.Errorf("a handoff says what is left to do: the next session starts from this and " +
			"from nothing else, so write what you would tell somebody taking over")
	case len(left) > HandoffLimit:
		return "", "", fmt.Errorf("what is left is %d bytes and a handoff may be %d: it goes in front of the "+
			"next session beside the brief, so say what is left and leave the working out of it",
			len(left), HandoffLimit)
	case len(tried) > HandoffLimit:
		return "", "", fmt.Errorf("what you tried is %d bytes and a handoff may be %d: name the dead ends, "+
			"not the route to them", len(tried), HandoffLimit)
	}
	return left, tried, nil
}

// Latest is the newest handoff on a job, and false where the job has none.
func Latest(handoffs []Handoff) (Handoff, bool) {
	if len(handoffs) == 0 {
		return Handoff{}, false
	}
	return handoffs[len(handoffs)-1], true
}

// HandingOver says whether the next task of this job carries a handoff into a session that has not
// seen the job.
//
// Read off the record rather than counted. The newest handoff names the conversation that wrote it,
// and the system clears the job's session when it hands over, so from that moment the two differ and
// every task the job is then given is one the session before it never saw. A count would have to be
// kept somewhere, and a controller that died between writing it and using it would read a count
// nobody finished.
func HandingOver(one *Job) bool {
	latest, written := Latest(one.Handoffs)
	return written && latest.Session != one.Session
}

// SessionAfter is the conversation a job's next attempt runs in.
//
// A job's session is named after the job, so a dispatch made twice lands in the conversation the job
// has been in all along. Handing over is the one thing that must not: the point of it is a window
// that is empty, and the same handle would be the same full conversation. So each handoff moves the
// name on, and the name is derived from the record rather than stored beside it.
func SessionAfter(id string, handoffs int) string {
	if handoffs <= 0 {
		return SessionFor(id)
	}
	return fmt.Sprintf("%s-%d", SessionFor(id), handoffs+1)
}

// theHandoffAsk is the sentence the ask below is recognised by. It is a constant because the ask and
// the reading of it must not drift: a controller that stopped recognising its own ask would send it
// on every tick, and every ask is a task somebody pays for.
const theHandoffAsk = "this session takes no new work on this job"

// AskedForAHandoff is what the system sends a session that has reached the ceiling. It is the last
// task that session gets for this job.
//
// It asks for the branch by name, because a fresh session gets a fresh working directory. Nothing
// clones a repository once per workspace yet, which is quay-crew#255, so what is on disk in this
// container is not on disk in the next one and a handoff that names no branch hands over nothing but
// prose.
func AskedForAHandoff(one *Job, ceiling int) string {
	said := []string{
		fmt.Sprintf("Your context window is at or past the %d per cent this workspace allows, so %s. "+
			"Do not carry on with the work, and do not open anything new.",
			ceiling, theHandoffAsk),
		"Write the handoff instead, with: krewe job handoff \"<what is left>\" \"<what you tried that " +
			"did not work>\". A fresh session is given those words and nothing else you can see, so " +
			"write what you would tell somebody taking the work over.",
	}
	if one.Repository != "" {
		said = append(said, fmt.Sprintf("The next session starts in an empty working directory, so push "+
			"what you have to %s first and name the branch in what is left. Work nobody can fetch is work "+
			"that is done again.", one.Repository))
	}
	said = append(said, "Then answer with the same two things in a sentence, and stop.")
	return strings.Join(said, "\n\n")
}

// AskingForAHandoff says whether a task the system sent was that ask.
func AskingForAHandoff(prompt string) bool { return strings.Contains(prompt, theHandoffAsk) }

// HandedOver is what the system sends the fresh session that carries the job on.
//
// It is the brief and the record together, which is where it differs from a job being continued after
// a failure. A resume goes back into the conversation that did the work, so the brief would be the
// job asked for a second time. This conversation has never seen the job, so it gets the brief, what
// is already finished, and what the session before it left behind.
func HandedOver(one *Job) string {
	latest, written := Latest(one.Handoffs)
	said := []string{}
	if one.Product != "" {
		said = append(said, ServesAPerson(one.Product))
	}
	said = append(said,
		"This job was started by another session, which reached the end of what its context window "+
			"could hold and handed the rest to you. It is not a new job and it does not start again.",
		one.Brief,
		finishedAlready(one.Steps))
	if written {
		said = append(said, "What is left, in the words of the session that stopped:\n  "+latest.Left)
		said = append(said, whatWasTried(latest.Tried))
	}
	if one.PullRequest != "" {
		said = append(said, "The work so far is at "+one.PullRequest+". Read it before you write anything.")
	}
	if one.Repository != "" {
		said = append(said, "Your working directory is empty, so clone "+one.Repository+" and check out the "+
			"branch named above before you start. Nothing carried over from the other session's disk.")
		said = append(said, EndsInAPullRequest(one.Repository))
	}
	said = append(said, RecordEachStep())
	return strings.Join(said, "\n\n")
}

// whatWasTried is the dead ends the last session named, and the sentence for a session that named
// none. Silence is said out loud, because a heading with nothing under it reads as a lost record.
func whatWasTried(tried string) string {
	if tried == "" {
		return "The session before you recorded nothing it tried that did not work."
	}
	return "Tried already, and it did not work. Do not do it again:\n  " + tried
}

// NothingHandedOver is why a job whose session would not write a handoff stops.
//
// Stopped rather than handed over anyway. A fresh session given an empty handoff pays for every
// discovery the last one made, which is more than it costs to leave the work where a person can find
// it, and a job that carried on from nothing would read afterwards like one that handed over well.
func NothingHandedOver(one *Job, ceiling int) string {
	where := "what it did is in its conversation"
	if one.PullRequest != "" {
		where = "what it did is in its conversation and at " + one.PullRequest
	}
	return fmt.Sprintf("this job reached the %d per cent context ceiling and the session doing it was asked "+
		"for a handoff and wrote none, so there is nothing for a fresh session to start from: %s, and read "+
		"that before this job is declared again", ceiling, where)
}

// HandedOverAt is the line the record carries when the system moves a job to a fresh session, so a
// reader learns from the history why the conversation changed rather than inferring it.
func HandedOverAt(share int, ceiling int) string {
	return fmt.Sprintf("the session was at %d per cent of its context window, over this workspace's "+
		"ceiling of %d, so the rest of this job goes to a fresh session", share, ceiling)
}
