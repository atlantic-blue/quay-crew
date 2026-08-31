package job

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// A session that goes in circles, and the change the system makes when one does.
//
// The failure is on the record of the acceptance run of 30 August 2026. A session that could not get
// a check green tried the same shape of fix several times and gave the same reasoning each time. The
// operator was the loop detector, and only where he happened to read the transcript. Nothing here
// compared what a session produced against what it had just produced, so a session going in circles
// and a session working hard were the same picture from outside: one phase word, one growing bill.
//
// Three pieces, and the third is the one that matters:
//
//   - A measure. What an attempt said, held against what the earlier attempts at the same step said,
//     as one number between nothing alike and word for word the same.
//   - A record. Every attempt is a row carrying that number, so a loop is read off the record rather
//     than remembered by a controller, and so the number below can be replaced by a measurement.
//   - A change. Three attempts the system cannot tell apart stop the step, and the job escalates by
//     the route it declared. Detecting a loop and letting it run is worth nothing.
//
// The step is what makes this safe. A session that is getting somewhere records what it finished, so
// its attempts land on different steps and never stack up. A detector that fires on real progress is
// worse than none, because it stops work that was going to finish.

const (
	// LoopAttempts is how many attempts at one step are a loop. Three: the first is the work, the
	// second is a retry, and the third says the second changed nothing.
	LoopAttempts = 3
	// ShingleWords is how many words are in one shingle. Two attempts are compared on the runs of
	// three words they share rather than on the words themselves, because two answers about the same
	// repository share almost every word and share very few of the same runs.
	ShingleWords = 3
	// AttemptLimit is how much of what an attempt said the record keeps.
	//
	// The similarity is measured on what is kept, so anybody reading the record can work the number
	// out again. A whole answer can run to tens of kilobytes, and a table of those is a table nobody
	// reads and a store nobody prunes.
	AttemptLimit = 4096
)

// LoopThreshold is how alike two attempts have to be before the system reads them as one attempt.
//
// **Measured on the text this repository holds, and provisional until it is measured on attempts.**
// The corpus is every paragraph of CHANGELOG.md over sixty words, which is 304 pieces of technical
// prose about work that was really done, and the measurement is in loopcalibration_test.go so that it
// runs again on every build. Over the 46,056 pairs of different paragraphs the median is 0, the
// ninety ninth percentile is 0.024, and one pair reaches 0.546: two paragraphs that are one paragraph
// written twice with a noun changed, which is the thing this exists to find. Held against itself with
// every number in it changed, the way a session repeating an attempt reports a new measurement, the
// least any paragraph scores is 0.654.
//
// So half is an order of magnitude above anything different work produces, and below everything a
// repeat produces, on that corpus. **It is not a measurement of job attempts, because until this
// ships the system has recorded none.**
//
// **What it does not catch, said plainly.** An attempt reworded from scratch scores like different
// work: the same reasoning about a coverage check, written twice in different words, scores 0.057. So
// this finds a session saying the same thing again rather than a session thinking the same thing
// again. That is the deliberate direction of the error, because a detector that fires on real
// progress stops work that was going to finish.
//
// **What replaces the number.** Every attempt writes its similarity to the record now, whether or not
// it loops. Once fifty jobs have run, the measurement is this query:
//
//	select j.phase, a.step, a.seq, a.similarity
//	from job_attempts a join jobs j on j.id = a.job
//	order by a.job, a.seq;
//
// Read where an attempt that was followed by a finished step sits, and where an attempt on a job that
// ended failed or stopped sits, and put the threshold at the ninety fifth percentile of the first: it
// has to sit above nearly every attempt that went on to make progress. This is the shape the lease
// length in internal/job/controller.go already has, and it is replaced the same way.
const LoopThreshold = 0.5

// Attempt is what one attempt at one step produced, and how like the earlier attempts at that step it
// was.
//
// It is keyed on the task rather than counted, so recording it twice leaves one row. A controller
// that dies after the task lands and before the job moves is read again by the next controller, and
// an attempt counted twice there would manufacture a loop out of one piece of work.
type Attempt struct {
	Job  string
	Task string
	Seq  int
	// Step is which step this attempt was at, counting from one: the first attempt at a job is at step
	// 1, and the attempt after one finished step is at step 2. Attempts are compared only with attempts
	// at the same step, because a session that finished something is somewhere new.
	Step    int
	Session string
	// Said is what the attempt produced, capped at AttemptLimit: the answer where one landed, and the
	// failure where the task did not answer at all.
	Said string
	// Similarity is how like the closest earlier attempt at this step this one was, between 0 and 1.
	// Zero on the first attempt at a step, which has nothing to be like.
	Similarity float64
	OccurredAt time.Time
}

// TidyAttempt is what an attempt said, as the record keeps it: the space around it comes off and what
// is past the ceiling goes.
//
// The cap is not a redaction. What is kept is what the measure reads, so a reader holding the record
// holds everything the decision was made on.
func TidyAttempt(said string) string {
	tidy := strings.TrimSpace(said)
	if len(tidy) <= AttemptLimit {
		return tidy
	}
	return strings.TrimSpace(tidy[:AttemptLimit])
}

// Similarity is how alike two pieces of text are, between 0 for nothing in common and 1 for the same
// runs of words in both.
//
// It is the overlap of the sets of three word runs each one holds, over everything either of them
// holds. Word runs rather than words: two answers about one repository share nearly every word, and
// the question being asked is whether the same sentences are coming back rather than whether the same
// subject is being discussed.
//
// Nothing about the model is read here. The measure runs on text the system already has, so it costs
// no call, works with any backend, and can be worked out again by anybody holding the record.
func Similarity(one, other string) float64 {
	left, right := shingles(one), shingles(other)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	shared := 0
	for run := range left {
		if right[run] {
			shared++
		}
	}
	return float64(shared) / float64(len(left)+len(right)-shared)
}

// shingles is the set of three word runs in a piece of text, with the case and the punctuation taken
// off so two answers that differ by a full stop are not two answers.
//
// Text shorter than one run is one shingle of everything it has, so two short failures still compare
// rather than both scoring nothing and reading as different.
func shingles(text string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	runs := map[string]bool{}
	if len(words) == 0 {
		return runs
	}
	if len(words) < ShingleWords {
		runs[strings.Join(words, " ")] = true
		return runs
	}
	for at := 0; at+ShingleWords <= len(words); at++ {
		runs[strings.Join(words[at:at+ShingleWords], " ")] = true
	}
	return runs
}

// AtStep is the attempts at one step, in the order they were made.
func AtStep(attempts []Attempt, step int) []Attempt {
	at := make([]Attempt, 0, len(attempts))
	for _, one := range attempts {
		if one.Step == step {
			at = append(at, one)
		}
	}
	return at
}

// LikeTheOnesBefore is how like the closest earlier attempt at this step what an attempt said is, and
// zero where there is no earlier one.
//
// The closest rather than the last, because a session that alternates between two shapes of fix is
// going in circles as surely as one repeating a single shape, and comparing only against the attempt
// immediately before would score every one of those as new work.
func LikeTheOnesBefore(said string, earlier []Attempt) float64 {
	closest := 0.0
	for _, one := range earlier {
		if alike := Similarity(said, one.Said); alike > closest {
			closest = alike
		}
	}
	return closest
}

// Circling says whether these attempts at one step are a loop: LoopAttempts of them, of which the
// last two were each as alike as LoopThreshold to one that came before.
//
// The last two rather than all of them, because the first attempt at a step has nothing to be like.
// So the shape this reads is: something was tried, trying it again changed nothing, and trying it a
// third time changed nothing either.
func Circling(at []Attempt) bool {
	if len(at) < LoopAttempts {
		return false
	}
	for _, one := range at[len(at)-(LoopAttempts-1):] {
		if one.Similarity < LoopThreshold {
			return false
		}
	}
	return true
}

// The routes a job declares for the moment it goes in circles. Which one is a property of the job,
// declared while somebody is writing it, rather than a guess the system makes in the moment.
const (
	// RouteAsk stops and puts the question to the operator. It is what a job that declares nothing
	// gets, because it is the only route that needs nothing else to be true and cannot make the work
	// worse.
	RouteAsk = "ask"
	// RouteRole hands the job to another role, in a conversation of its own.
	RouteRole = "role"
	// RouteModel would run the same job on another model. It is refused: see ReadRoute.
	RouteModel = "model"
)

// Route is how a job escalates when it goes in circles: a word, and what that word names.
type Route struct {
	Word string
	To   string
}

// String is the route as it is declared and as it is stored, so what an operator types and what the
// record says are one spelling.
func (r Route) String() string {
	if r.To == "" {
		return r.Word
	}
	return r.Word + ":" + r.To
}

// Names says whether this route hands the job to the named role.
func (r Route) Names(role string) bool { return r.Word == RouteRole && r.To == role && role != "" }

// ReadRoute is the route a job declared, and the refusal where it declared something the system
// cannot carry out.
//
// Nothing declared is asking. A job whose author never thought about looping still gets a person
// rather than a budget spent in silence.
func ReadRoute(declared string) (Route, error) {
	tidy := strings.TrimSpace(declared)
	if tidy == "" {
		return Route{Word: RouteAsk}, nil
	}
	word, to, _ := strings.Cut(tidy, ":")
	word, to = strings.TrimSpace(strings.ToLower(word)), strings.TrimSpace(to)
	switch word {
	case RouteAsk:
		if to != "" {
			return Route{}, fmt.Errorf("this job escalates by %q, and asking puts the question to the "+
				"operator, so there is nobody to name after it: write ask", declared)
		}
		return Route{Word: RouteAsk}, nil
	case RouteRole:
		if to == "" {
			return Route{}, fmt.Errorf("this job escalates to a role and names none: write role:<name>, " +
				"for example role:architect, and the workspace has to hold that role")
		}
		return Route{Word: RouteRole, To: to}, nil
	case RouteModel:
		return Route{}, fmt.Errorf("this job escalates onto the model %q, and this build runs one model "+
			"for the whole system: a role declares a model and nothing reads it yet, so this would read as a "+
			"change and be none. Import a role that runs on that model and escalate to it with role:<name>, "+
			"or write ask and let the operator decide", or(to, "it does not name"))
	default:
		return Route{}, fmt.Errorf("this job escalates by %q, which is not a route: write ask to put the "+
			"question to the operator, or role:<name> to hand the job to another role", declared)
	}
}

// or is the value, or a stand in where there is none, so a refusal about a route that named nothing
// still reads as a sentence.
func or(value, missing string) string {
	if value == "" {
		return missing
	}
	return value
}

// Escalating is what the record says a looping job escalated to, in a person's words.
func Escalating(route Route) string {
	switch route.Word {
	case RouteRole:
		return "handed to the " + route.To + " role"
	default:
		return "put to the operator"
	}
}

// Alike is a similarity as a reader reads it.
//
// Zero is said in words rather than as a number. It is either the first attempt at a step, which has
// nothing to be like, or an attempt sharing no run of three words with any before it, and both read
// as the same thing to somebody deciding what to do next.
func Alike(similarity float64) string {
	if similarity == 0 {
		return "nothing before it that it is like"
	}
	return fmt.Sprintf("%.0f per cent alike", similarity*100)
}

// Looped is what the record says when a job goes in circles: how many attempts, at which step, and
// how alike they were.
func Looped(step int, similarity float64) string {
	return fmt.Sprintf("this job made %d attempts at step %d that the system could not tell apart, the "+
		"last of them %s, so the step was stopped rather than tried again",
		LoopAttempts, step, Alike(similarity))
}

// LoopedAgain is why a job that went in circles a second time is stopped rather than escalated again.
//
// A job escalates once. The second loop is the escalation itself failing, and escalating again would
// be the system going in circles about a session going in circles, which is the same bill with more
// steps in it. So it stops, and a person reads what two different attempts at the work produced.
func LoopedAgain(step int, escalated string) string {
	return fmt.Sprintf("this job went in circles again, at step %d, after it was already escalated (%s). "+
		"It is stopped rather than escalated a second time: read what its attempts said with krewe job "+
		"show, and declare the work again in smaller pieces or with what is missing in the brief",
		step, escalated)
}

// LoopQuestion is what the system asks a person about a job that is going in circles.
//
// It carries what the attempts said, because the decision the person is being asked to make cannot be
// made from a number. It is what an operator reading the transcript would have seen, put in front of
// them without their having to look.
func LoopQuestion(one *Job, step int, at []Attempt) string {
	said := []string{
		fmt.Sprintf("This job has made %d attempts at step %d and the system cannot tell them apart, so "+
			"it stopped rather than spending the rest of the budget on a fourth.", len(at), step),
		whatTheAttemptsSaid(at),
		"Tell it what to change, and it starts again from what you say: a different approach, the thing " +
			"it is missing, or a smaller piece of the work. If the work itself is wrong, stop the job with " +
			"krewe job stop instead.",
	}
	if len(one.Steps) > 0 {
		said = append([]string{fmt.Sprintf("It finished %d steps before this, and those are on the record.",
			len(one.Steps))}, said...)
	}
	return holdTo(strings.Join(said, "\n\n"), QuestionLimit)
}

// HandedOver is what the system sends the session a looping job has been handed to.
//
// It carries what the attempts said for the same reason the question does, and one instruction the
// question does not need: the attempts that have been made are what this session must not make again.
func HandedOver(one *Job, at []Attempt) string {
	said := []string{}
	if one.Product != "" {
		said = append(said, ServesAPerson(one.Product))
	}
	// A handed job starts in a conversation of its own, so the request has to be written out here too:
	// the session reading this never saw the one the job started in.
	if asked := AskedInTheseWords(one.Request, one.Brief); asked != "" {
		said = append(said, asked)
	}
	said = append(said,
		fmt.Sprintf("This job was handed to you because the session doing it went in circles: %d attempts "+
			"at one step that the system could not tell apart. You are starting in your own conversation, "+
			"and nothing it tried is in your history, so it is written out below.", len(at)),
		one.Brief,
		finishedAlready(one.Steps),
		whatTheAttemptsSaid(at),
		"Do not make those attempts again. Work out why they did not get there before you write anything, "+
			"and if the reason is that something is missing rather than that the approach was wrong, say so "+
			"in your answer rather than trying it a fourth time.")
	if one.Repository != "" {
		said = append(said, EndsInAPullRequest(one.Repository))
	}
	said = append(said, RecordEachStep())
	return holdTo(strings.Join(said, "\n\n"), BriefLimit)
}

// whatTheAttemptsSaid is the attempts written out for whoever has to decide what happens next, oldest
// first, each held to a few lines.
func whatTheAttemptsSaid(at []Attempt) string {
	if len(at) == 0 {
		return "Nothing it said was recorded."
	}
	lines := make([]string, 0, len(at)+1)
	lines = append(lines, "This is what each attempt said:")
	for _, one := range at {
		lines = append(lines, fmt.Sprintf("  attempt %d (%s): %s",
			one.Seq, Alike(one.Similarity), holdTo(oneLine(one.Said), attemptInAQuestion)))
	}
	return strings.Join(lines, "\n")
}

// attemptInAQuestion is how much of one attempt goes into a question or a brief. Every attempt has to
// fit beside the others and beside the instruction, and what is left out is a task list away in
// krewe job show.
const attemptInAQuestion = 600

// holdTo cuts text to a ceiling on a word, so nothing the system writes runs past a limit it is held
// to. It says it cut, because prose that stops mid sentence reads as a system that broke.
func holdTo(text string, ceiling int) string {
	if len(text) <= ceiling {
		return text
	}
	cut := text[:ceiling-len(theRestIsInTheRecord)]
	if at := strings.LastIndexAny(cut, " \n"); at > 0 {
		cut = cut[:at]
	}
	return cut + theRestIsInTheRecord
}

const theRestIsInTheRecord = " ... (the rest is on the record: krewe job show)"

// Loop is what the system writes when a job goes in circles: the record of the loop, and where the
// job goes next.
//
// Where it goes is what the job declared rather than a choice made here, which is the whole point of
// declaring a route: the moment a job is going nowhere is the worst moment to be deciding what to do
// about it.
type Loop struct {
	// Owner is the controller writing it. The write applies only where that controller still holds
	// the lease, in the same statement, so a controller that lost the row cannot move another's job.
	Owner string
	// Step is the step it went in circles on, and Similarity is how alike the attempt that closed the
	// loop was to the ones before it.
	Step       int
	Similarity float64
	// To is the route taken, in the shape it was declared. Empty where the job had already escalated
	// once and is being stopped instead.
	To string
	// Phase is where the job goes: asking where a person decides, pending where it is handed to
	// another role, stopped where it had escalated already.
	Phase string
	// Question is what an asking job waits to be told, and Reason is why a stopped one stopped. A job
	// going again carries neither: a pending job with a reason on it reads as one the machine is
	// holding back for want of room.
	Question string
	Reason   string
	// Handed says the job starts again in a conversation of its own, so the session it went in circles
	// in comes off the row. What that conversation was stays on the attempts it made.
	Handed bool
	// Attempt is the attempt that closed the loop, written in the same transaction as the loop.
	Attempt *Attempt
}

// RoleNow is the role doing this job: the one it was declared with, or the one it was handed to when
// it went in circles.
//
// The declaration is left as it was written. What a caller asked for does not change because the
// system had to escalate, so who is doing the work now is status rather than a rewrite of the
// declaration, and a reader can still see both.
func RoleNow(one *Job) string {
	if route, err := ReadRoute(one.EscalatedTo); err == nil && route.Word == RouteRole {
		return route.To
	}
	return one.Role
}

// ConversationFor is the conversation this job runs in now: the one named after the job, or one of
// its own where the job was handed to another role.
//
// A role is read only when a session is born, so a job handed on has to start a conversation of its
// own or the role it was handed to never applies. The name is derived from the row rather than
// minted, the way the first one is, so a controller that comes back to the job after another died
// finds the same conversation without being told which it was.
func ConversationFor(one *Job) string {
	if route, err := ReadRoute(one.EscalatedTo); err == nil && route.Word == RouteRole {
		return SessionFor(one.ID) + "-" + route.To
	}
	return SessionFor(one.ID)
}

// TheAttempt is what one attempt at a step produced, as the record keeps it, with how like the
// earlier attempts at that step it is.
//
// Keyed on the task rather than counted, because the same task is read again by whichever controller
// holds the job next, and an attempt counted twice would manufacture a loop out of one piece of work.
func TheAttempt(one *Job, task, said string) Attempt {
	kept := TidyAttempt(said)
	step := len(one.Steps) + 1
	return Attempt{
		Job: one.ID, Task: task, Seq: len(one.Attempted) + 1, Step: step, Session: one.Session,
		Said: kept, Similarity: LikeTheOnesBefore(kept, Before(one.Attempted, step, task)),
		OccurredAt: time.Now().UTC(),
	}
}

// Before is the attempts at this step that are not this task.
//
// Not this task, because a controller that reads a task another one already recorded would otherwise
// hold the attempt against a copy of itself, score it one, and make a loop out of one piece of work.
// The store keys on the task for the same reason; this is the other side of it.
func Before(attempts []Attempt, step int, task string) []Attempt {
	earlier := make([]Attempt, 0, len(attempts))
	for _, one := range AtStep(attempts, step) {
		if one.Task != task {
			earlier = append(earlier, one)
		}
	}
	return earlier
}

// Recorded says whether this attempt is already on the job's record, which is what a controller
// reading a task another controller already read finds.
func RecordedAttempt(attempts []Attempt, task string) bool {
	for _, one := range attempts {
		if one.Task == task {
			return true
		}
	}
	return false
}
