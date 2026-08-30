package job

import (
	"fmt"
	"strings"
	"time"
)

// A job that failed is continued rather than started again.
//
// The failure this answers is on the record of an acceptance run: the container runtime went down
// and took six jobs with it, a credential ran out sixty seconds in, and each time the only remedy was
// to declare the same brief a second time. The second attempt reads the same issue, cuts the same
// worktree and makes the same discoveries, so the work is paid for twice and two branches carry one
// slice under two names.
//
// Three pieces make continuing possible, and all three are records rather than memory:
//
//   - A session says what it finished, one line per step, as it finishes it. The lines are rows, so
//     they outlive the container, the controller and the night in between.
//   - A resume puts the failed job back to pending, keeping its session, so the controller starts it
//     the way it starts anything else and the task lands in the conversation the job has been in all
//     along. The session's worktree, its branch and its pull request are where it left them.
//   - What it failed with moves off the reason and onto the row as the failure this attempt is
//     continuing past, so the next task can say what went wrong without a pending job reading as one
//     the machine is holding back.
//
// The guard is the operator's. A job that failed because the work was wrong is refused, which ends
// it as stopped, and a stopped job is never continued: stopping is the deliberate end.
const (
	// StepLimit is how long one step may be. It is the title's ceiling, because both are one line a
	// person reads in a listing, and a step that needs a paragraph is a job.
	StepLimit = TitleLimit
	// StepCount is how many steps one job may record.
	//
	// The ceiling is what the next task can carry. Every finished step goes in front of the session
	// that continues the job, beside its brief, so the record has to stay a list somebody reads rather
	// than a transcript. Forty lines of two hundred bytes is half a brief. A job with forty steps in it
	// is more than one job.
	StepCount = 40
)

// Step is one thing a session finished, on the record.
//
// It is what the session said it finished rather than anything the system watched, the way an answer
// is. What that buys is the only record that survives the attempt: a controller cannot see inside a
// container, and a session that dies takes everything it did not write down with it.
type Step struct {
	Job     string
	Seq     int
	Summary string
	// FinishedAt is when the step was recorded, which is when the session said it was done.
	FinishedAt time.Time
}

// TidyStep is a step as the system keeps it, and the refusal where it could not be kept.
func TidyStep(summary string) (string, error) {
	tidy := TidySentence(summary)
	switch {
	case tidy == "":
		return "", fmt.Errorf("a step says what you finished: write it in a few words, so whoever continues " +
			"this job knows not to do it again")
	case len(tidy) > StepLimit:
		return "", fmt.Errorf("this step is %d bytes and a step may be %d: it is one line beside the others, "+
			"so say what you finished and leave the working out of it", len(tidy), StepLimit)
	}
	return tidy, nil
}

// Recorded says whether this job has already recorded a step in these words.
//
// The record is the set of what is finished, so the same words twice are one step. A session that
// continues a job and says again what it said before must not push the earlier steps down a list, and
// a step recorded twice is the record claiming work that happened once.
func Recorded(steps []Step, summary string) bool {
	for _, one := range steps {
		if one.Summary == summary {
			return true
		}
	}
	return false
}

// RoomForAStep is the refusal where a job has recorded as many steps as it may, and nil where there
// is room for another.
func RoomForAStep(steps []Step) error {
	if len(steps) < StepCount {
		return nil
	}
	return fmt.Errorf("this job has recorded %d steps and a job may record %d: every one of them goes in "+
		"front of the session that continues it, so declare the rest of this as its own job",
		len(steps), StepCount)
}

// Resumable says whether a job in this phase can be continued, which is a job that failed and no
// other.
//
// A job that is done has nothing to continue. A job that is stopped was ended on purpose, by a
// person, by a limit or by a claim that did not hold, and continuing it would walk back somebody's
// decision. Everything else has not stopped yet.
func Resumable(phase string) bool { return phase == PhaseFailed }

// NotResumable is what the system says to a resume it will not do. It names the phase the job is in
// and what to do instead, because a refusal a caller cannot act on sends them looking.
func NotResumable(id, phase string) string {
	switch phase {
	case PhaseDone:
		return fmt.Sprintf("job %s is done, so there is nothing left of it to continue: read what came back "+
			"with krewe job show %s, and declare a new job for whatever is still missing", id, id)
	case PhaseStopped:
		return fmt.Sprintf("job %s was stopped, which is somebody ending it on purpose rather than it "+
			"breaking, so it is not continued: declare a new job for the work that is left", id)
	default:
		return fmt.Sprintf("job %s is %s, so it has not stopped: a job is continued after it fails, and "+
			"this one is still the system's to move", id, phase)
	}
}

// Refusable says whether a job in this phase can be refused, which is the same job a resume applies
// to. Refusing is the other answer to a failure: this one was wrong, do not offer to continue it.
func Refusable(phase string) bool { return Resumable(phase) }

// NotRefusable is what the system says to a refusal it will not apply.
func NotRefusable(id, phase string) string {
	return fmt.Sprintf("job %s is %s, and refusing is the answer to a job that failed: it ends a failure "+
		"on purpose so nobody continues it. Stop a job that has not ended with krewe job stop %s", id, phase, id)
}

// Refused is why a job an operator refused ended, carrying the failure it is refusing so a reader is
// not left comparing two records to find out what happened.
func Refused(reason, failure string) string {
	said := strings.TrimSpace(reason)
	if said == "" {
		said = "refused by the operator"
	}
	if failure = strings.TrimSpace(failure); failure != "" {
		return fmt.Sprintf("refused rather than continued: %s. It failed with: %s", said, failure)
	}
	return "refused rather than continued: " + said
}

// Continued is what the system sends a session whose job is being carried on after a failure.
//
// The steps go with it, because the session is being asked not to do them again and a model reads
// what it is handed rather than what it remembers. So does the failure, so it knows what it is
// walking back into, and so does the moment it stopped, because what it has to do first is find out
// what moved under it since then.
//
// The base is the part the system cannot check. Nothing here runs git, so the session is told to
// fetch and say what moved, the same way a job that names a repository is told to open a pull
// request: the system states the expectation and reads the answer against it.
func Continued(one *Job) string {
	said := []string{
		"This job stopped part way and is being continued rather than started again. Do not do it over.",
		finishedAlready(one.Steps),
		fmt.Sprintf("It stopped%s with: %s", theMomentItStopped(one.FinishedAt), one.Resuming),
		"Your working directory, your branch and anything you pushed are where you left them. Before you " +
			"touch any of it, fetch the branch this work is based on and say in your answer what moved " +
			"under it while you were stopped, because it may have moved. Then carry on from the first step " +
			"that is not on the list above, and record each step as you finish it.",
	}
	if one.Repository != "" {
		// Said again, because this task is the one that ends the job and a model reads what it is handed.
		// It also keeps the bound honest: the system asks a session once more for an address its answer
		// did not carry, and counts the tasks the session has run to decide, so a continued attempt that
		// was never told how the job ends would be stopped for missing something nobody asked it for.
		said = append(said, EndsInAPullRequest(one.Repository))
	}
	if one.Product != "" {
		said = append([]string{ServesAPerson(one.Product)}, said...)
	}
	return strings.Join(said, "\n\n")
}

// finishedAlready is the list of steps in front of the session that continues the job, and the
// sentence for a job that recorded none.
//
// A job with no steps is the honest case rather than an error. The session died before it wrote
// anything down, or it was running a build that never recorded any, so what it is told is to look
// before it repeats itself.
func finishedAlready(steps []Step) string {
	if len(steps) == 0 {
		return "Nothing was recorded as finished, so before you do anything twice, read what is already " +
			"in your working directory and on your branch."
	}
	lines := make([]string, 0, len(steps)+1)
	lines = append(lines, "These steps are finished and on the record. Do not do them again:")
	for _, one := range steps {
		lines = append(lines, fmt.Sprintf("  %d. %s", one.Seq, one.Summary))
	}
	return strings.Join(lines, "\n")
}

// theMomentItStopped is when the attempt being continued ended, and nothing where the row does not
// say. A moment invented here would be a moment the session measures its base against.
func theMomentItStopped(at *time.Time) string {
	if at == nil {
		return ""
	}
	return " at " + at.UTC().Format(time.RFC3339)
}

// RecordEachStep is the line every session doing a job is given beside its brief.
//
// It is added by the system rather than left to whoever wrote the brief, for the reason the pull
// request line is: a brief that forgets to ask for it produces a job that can only be started again
// from nothing, and every brief forgets eventually.
func RecordEachStep() string {
	return "Record each step as you finish it, in a few words: krewe job step \"read the issue\". If this " +
		"job dies part way, what is on that record is where it carries on from, and what is not on it " +
		"is done a second time."
}
