package job

import (
	"fmt"
	"strings"
)

// A job that names a repository does not settle on its own answer.
//
// Every failure of the acceptance run reached the operator through one door. A session finished its
// work, it wrote an answer, and the job settled on that answer. Where the work was wrong the answer
// said it was right, and it said so in good faith: the session had no way to know otherwise.
//
// So two sessions that did not do the work read it before it settles. A reviewer reads the change
// against what the job was asked for. A tester runs the repository's own gates and reads their
// output rather than their exit status, because a suite that ran nothing exits zero. Each says pass
// or fail on a line of its own, and the line is read off the answer, the way a pull request address
// and a base line already are: the model reporting on its own work is the thing this exists to stop.
//
// Neither of them holds a credential. What a session may call on the system comes from the job it
// runs, and these sessions run no job, so the answer is nothing at all. They change the row in no
// way and they cannot declare, answer or stop anything.
//
// A fail is not the end of the run. It goes back to the session that did the work as its next task,
// carrying the reason, and the job stays open, because the branch and the worktree are still there
// and the fix is usually one edit. It goes back once. Every ask is a task somebody pays for, so a
// second round that still fails stops the job with the reason on the row, which is the same bound the
// pull request ask already has.
//
// The gate is refusable rather than optional. A job may be declared with it off, and the row says so,
// so a settled job always states whether anything independent passed it.

// The two gates, by name. They are words rather than a boolean pair because they are printed: a
// reader of `krewe job show` is told which of them passed a job.
const (
	// GateReviewer reads the change against what the job was asked for.
	GateReviewer = "reviewer"
	// GateTester runs the repository's own gates and reads the output.
	GateTester = "tester"
)

// VerdictMarker opens the line a gate writes its verdict on. The system asks for one shape it can
// find, so what it finds is what the session meant to say rather than a sentence that happened to
// hold the word: an answer that discusses whether the change passes is not a verdict.
const VerdictMarker = "Verdict:"

// The two words a verdict may be. Matched exactly, folded for case, as the first word after the
// marker. Anything else is no verdict at all, which is the case this has to get right: a gate that
// cannot be read has passed nothing, and reading it as a pass is the false green the gate exists for.
const (
	verdictPass = "pass"
	verdictFail = "fail"
)

// theGateSentItBack is the sentence a send back is recognised by. It is a constant because the ask
// and the reading of it must not drift: an ask that stops matching is an ask that is made forever,
// and every ask is a task somebody pays for.
const theGateSentItBack = "did not pass the gate"

// Gated says whether this job has to be passed by something independent before it settles.
//
// A job that names no repository is not gated, because there is no change to read and no gate to
// run. A job declared with the gate off is not gated either, and the row says so.
func Gated(one *Job) bool {
	if one == nil {
		return false
	}
	return one.Repository != "" && !one.Ungated
}

// ReviewerFor and TesterFor are the conversations the two gates run in.
//
// Named after the job, the way the working session is, so a controller that comes back to a row
// finds the same two conversations without being told which they were. They are not the working
// session, and that is the whole point of them: a second opinion from the session that formed the
// first is not a second opinion.
func ReviewerFor(id string) string { return SessionFor(id) + "-reviewer" }
func TesterFor(id string) string   { return SessionFor(id) + "-tester" }

// Judgement is what a gate said about the work.
type Judgement struct {
	// Passed is whether the gate passed the work.
	Passed bool
	// Reason is what it said about it. It is what goes back to the session that did the work, so a
	// fail with nothing after it is handled rather than passed on empty.
	Reason string
	// Said is whether the answer carried a verdict at all. False means nothing judged this work,
	// which is not the same as a fail and must never be read as a pass.
	Said bool
}

// Verdict is what a gate's answer says about the work.
//
// A bullet, a heading or bold text in front of the marker is still the line, because a session
// writing a report reaches for one of those and refusing the line over a dash would throw away a
// judgement that was made. The marker with nothing after it, or with a word that is neither pass nor
// fail, is not a verdict.
func Verdict(answer string) Judgement {
	for _, line := range strings.Split(answer, "\n") {
		said := strings.TrimLeft(strings.TrimSpace(line), "-*#> \t")
		if len(said) < len(VerdictMarker) || !strings.EqualFold(said[:len(VerdictMarker)], VerdictMarker) {
			continue
		}
		rest := strings.TrimLeft(strings.Trim(said[len(VerdictMarker):], "*_ \t"), " \t")
		word, reason, _ := strings.Cut(rest, " ")
		switch strings.ToLower(strings.Trim(word, ".,:;*_")) {
		case verdictPass:
			return Judgement{Passed: true, Reason: tidyReason(reason), Said: true}
		case verdictFail:
			return Judgement{Reason: tidyReason(reason), Said: true}
		}
	}
	return Judgement{}
}

// tidyReason is what a gate said, as one line, with the word a session puts between the verdict and
// its reason taken off. "fail because the migration is missing" and "fail: the migration is missing"
// say the same thing and must read the same on the row.
func tidyReason(reason string) string {
	said := TidySentence(reason)
	for _, joining := range []string{"because ", "since ", ": ", "- ", "— "} {
		if len(said) >= len(joining) && strings.EqualFold(said[:len(joining)], joining) {
			said = strings.TrimSpace(said[len(joining):])
			break
		}
	}
	return strings.TrimSpace(strings.TrimLeft(said, ":-— "))
}

// because is what a gate said, and a sentence where it said nothing. A fail with no reason after it
// is still a fail, and the session it goes back to has to be told where to read.
func because(reason string) string {
	if reason == "" {
		return "it gave no reason, so read the conversation it wrote"
	}
	return reason
}

// SayTheVerdict is how a gate is asked for its answer, in the shape the system reads back. Both
// gates are asked for the same shape, so a reader of a job learns one line rather than two.
func SayTheVerdict() string {
	return fmt.Sprintf("Answer with one line of its own that starts with %s, followed by %s or %s, and "+
		"then why in a sentence. For example: %s %s the change adds a column and no migration, so a "+
		"fresh store cannot read it. An answer with no such line passes nothing.",
		VerdictMarker, verdictPass, verdictFail, VerdictMarker, verdictFail)
}

// AskedToReview is what the system sends the session that reads the change.
//
// It is given what the job was asked for rather than the answer the working session wrote, and that
// is deliberate: the answer is the testimony this gate exists to check, so a reviewer handed it first
// is a reviewer reading somebody else's conclusion.
func AskedToReview(one *Job, address string) string {
	said := []string{
		fmt.Sprintf("You are the %s of this work and you did not do it. Another session changed %s and "+
			"opened %s. Read that change and say whether it does what was asked.",
			GateReviewer, one.Repository, address),
		"What was asked:\n\n" + whatWasAsked(one),
		"Clone that repository yourself and read the change there. Read it against the repository as it " +
			"is, not only against the diff: most of what matters is in the files the pull request did not " +
			"touch, which is how the new code is wired in, which call sites were missed, and what a reader " +
			"of this repository is promised for a change like this one.",
		"Fail it where the change does not do what was asked, where it breaks something a person or an " +
			"operator gets, or where it says something about itself that the code does not do. Say nothing " +
			"about style, naming, comment density or anything a linter covers: a finding earns its place by " +
			"changing what somebody gets.",
		"Change no file, push nothing and open nothing. You are reading.",
		SayTheVerdict(),
	}
	return strings.Join(said, "\n\n")
}

// AskedToTest is what the system sends the session that runs the gates.
//
// The instruction it carries that matters most is to read the output rather than the exit status. A
// suite that ran nothing exits zero, a filter in a pipeline reports the status of the filter, and a
// green check that never executed is indistinguishable from one that passed.
func AskedToTest(one *Job, address string) string {
	said := []string{
		fmt.Sprintf("You are the %s of this work and you did not do it. Another session changed %s and "+
			"opened %s. Run that repository's own gates against the change and say whether they pass.",
			GateTester, one.Repository, address),
		"What was asked:\n\n" + whatWasAsked(one),
		"Clone that repository yourself and check out the branch the pull request is from, so what you " +
			"run is the change rather than the base.",
		"Run the gates the repository runs rather than an approximation of them. Find them in its " +
			"Makefile, its package scripts or its continuous integration workflow: its formatter, its " +
			"linter, its whole test suite, and its coverage threshold where it has one. Run the whole " +
			"suite and not the files that changed.",
		"Read the output, never the exit status. A suite that ran nothing exits zero, so a run is only " +
			"evidence once you have seen a count of what executed, a duration, or a line of real output. " +
			"Never pipe a gate through tail, head or grep and then read the status: a pipeline reports the " +
			"status of its last command and the filter eats the summary line. Say in your answer what " +
			"actually ran, with the numbers.",
		"Fail it where a gate is red, where a gate could not be run, or where a gate reported success " +
			"having executed nothing. Change no file, push nothing and merge nothing.",
		SayTheVerdict(),
	}
	return strings.Join(said, "\n\n")
}

// whatWasAsked is the job in front of a gate: the sentence it serves, its title and its brief.
//
// The sentence goes first for the reason it goes first in front of the session doing the work. A
// change is judged against what a person gets, and a gate given the brief alone judges faithfulness
// to a document, which is exactly how a run delivered something nobody could use.
func whatWasAsked(one *Job) string {
	said := []string{}
	if one.Product != "" {
		said = append(said, "For a person: "+one.Product)
	}
	said = append(said, one.Title, one.Brief)
	return strings.Join(said, "\n\n")
}

// SentBack is what the system sends the session that did the work, when a gate failed it.
//
// It asks for the address of the pull request again, for the reason the continued task does: this
// answer is the one that ends the job, and a job in a repository is held to naming its pull request
// in the answer that ends it, so an answer carrying the fix and no address would trade one silence
// for another.
func SentBack(gate, reason string, one *Job) string {
	return fmt.Sprintf("Your work %s: the %s read it and failed it. %s Your branch, your worktree and "+
		"your pull request are where you left them, so fix what it named rather than starting again. "+
		"This answer ends the job, so put the address of your pull request in it as well, and do not "+
		"merge it. The %s reads your work again after this, and a second fail ends the job.",
		theGateSentItBack, gate, because(reason), gate)
}

// SentBackByTheGate says whether a task the system sent was the gate sending the work back.
//
// The round is read off the record rather than counted in a field, which is what makes this safe to
// run twice: a controller that took the row over after another died reads the same tasks and reaches
// the same answer. It is also what bounds the asking, because the count of send backs is what says
// whether this work has been round once already.
func SentBackByTheGate(prompt string) bool { return strings.Contains(prompt, theGateSentItBack) }

// FailedTheGate is why a job that a gate failed twice stops.
//
// It names the gate, what it said, and where the work is, because the end of the job is not the end
// of what it produced: the branch is pushed and the pull request is open, and somebody reading this
// reason has to know that before they declare the work again.
func FailedTheGate(gate, reason string, one *Job) string {
	return fmt.Sprintf("the %s failed this work twice, so nothing independent passed it: %s. The work is "+
		"in %s and in the pull request it opened; read the %s conversation before this is trusted or the "+
		"job is declared again", gate, because(reason), one.Repository, gate)
}

// TheGateSaidNothing is why a job stops when a gate answered without a verdict.
//
// Stopped rather than passed, and stopped rather than asked again. A gate whose answer carries no
// verdict has judged nothing, and reading that as a pass is the false green the gate exists to
// prevent: the job settles saying it was independently checked, having been checked by nobody.
func TheGateSaidNothing(gate string) string {
	return fmt.Sprintf("the %s answered without a verdict, so nothing independent passed this work: an "+
		"answer carries a line opening with %s and this one carried none. Read the %s conversation, and "+
		"declare the job again when you know what it found", gate, VerdictMarker, gate)
}

// TheGateCouldNotRun is why a job stops when a gate's own task failed or was halted.
//
// A gate that could not be run has passed nothing, so the job does not settle on it. This is the
// same rule the prover already applies: a check that quietly passes when it could not be run is the
// same false green as no check at all.
func TheGateCouldNotRun(gate, failure string) string {
	return fmt.Sprintf("the %s could not run, so nothing independent passed this work: %s. The work is in "+
		"the session and in any pull request it opened", gate, oneLine(failure))
}

// Gate is what read a job before it settled: as much of a job as saying so needs.
//
// It is a value rather than the job itself so that the tool, which holds a job as it came off the
// wire, says the same sentence as anything else that reads one. Two places writing this sentence is
// two places for it to drift.
type Gate struct {
	Repository string
	Ungated    bool
	Reviewed   bool
	Tested     bool
	Phase      string
}

// Gate is what read this job before it settled.
func (j *Job) Gate() Gate {
	if j == nil {
		return Gate{}
	}
	return Gate{Repository: j.Repository, Ungated: j.Ungated,
		Reviewed: j.Reviewed, Tested: j.Tested, Phase: j.Phase}
}

// PassedBy is what `krewe job show` says about what read a job before it settled.
//
// Empty for a job that names no repository, because there was no change for anything to read. A job
// declared with the gate off reads differently rather than reading as one nothing happened to: the
// difference between "nothing independent passed this" and "this was passed" is the whole point of
// writing it down.
func (g Gate) PassedBy() string {
	if g.Repository == "" {
		return ""
	}
	if g.Ungated {
		return "declared with the gate off, so nothing independent read this work"
	}
	switch {
	case g.Reviewed && g.Tested:
		return fmt.Sprintf("passed by the %s and the %s, in sessions that did not do the work",
			GateReviewer, GateTester)
	case g.Reviewed:
		return fmt.Sprintf("passed by the %s; the %s has not passed it", GateReviewer, GateTester)
	case g.Tested:
		return fmt.Sprintf("passed by the %s; the %s has not passed it", GateTester, GateReviewer)
	case Terminal(g.Phase):
		return fmt.Sprintf("neither the %s nor the %s passed this work", GateReviewer, GateTester)
	default:
		return fmt.Sprintf("it settles when the %s and the %s pass it", GateReviewer, GateTester)
	}
}
